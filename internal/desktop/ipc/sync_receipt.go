package ipc

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
	syncapp "yaare/internal/app/sync"
	"yaare/internal/domain/syncmodel"

	"yaare/internal/adapters/cloudsync"
	"yaare/internal/adapters/configsync"
	"yaare/internal/adapters/storage/durable"
	"yaare/internal/adapters/storage/sqlite"
)

const syncReceiptPurpose = "config-sync-receipt-v2"
const syncReceiptBodyAAD = "yaare-sync-receipt-body-v2"
const maxReceiptHeader = 4096

type syncReceipt struct {
	Scope, Baseline, Published, Local [32]byte
	LocalKey                          []byte
}

func receiptScope(c syncSaved) [32]byte {
	raw, _ := json.Marshal(struct {
		Target  configsync.Config
		Include bool
		Epoch   string
	}{c.Target, c.IncludeCredentials, c.ReceiptEpoch})
	return sha256.Sum256(raw)
}

func sealLocalReceiptBody(d syncapp.Document) (body, key []byte, err error) {
	d.Workspace = syncmodel.PackIcons(d.Workspace)
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, nil, err
	}
	defer clear(raw)
	if len(raw) > cloudsync.MaxPlaintext {
		return nil, nil, errors.New("同步配置超过大小限制。")
	}
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return nil, nil, err
	}
	if _, err = gz.Write(raw); err != nil {
		_ = gz.Close()
		clear(buf.Bytes())
		return nil, nil, err
	}
	if err = gz.Close(); err != nil {
		clear(buf.Bytes())
		return nil, nil, err
	}
	compressed := buf.Bytes()
	defer clear(compressed)
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		clear(key)
		return nil, nil, err
	}
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		clear(key)
		return nil, nil, err
	}
	body = aead.Seal(nonce, nonce, compressed, []byte(syncReceiptBodyAAD))
	if len(body) > cloudsync.MaxBlob {
		clear(key)
		clear(body)
		return nil, nil, errors.New("同步配置超过大小限制。")
	}
	return body, key, nil
}

func openLocalReceiptBody(body, key []byte) (syncapp.Document, error) {
	var d syncapp.Document
	if len(key) != 32 {
		return d, errors.New("待确认同步记录无效。")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return d, err
	}
	aead, _ := cipher.NewGCM(block)
	nonceSize := aead.NonceSize()
	if len(body) < nonceSize+aead.Overhead() {
		return d, errors.New("待确认同步记录无效。")
	}
	compressed, err := aead.Open(nil, body[:nonceSize], body[nonceSize:], []byte(syncReceiptBodyAAD))
	if err != nil {
		return d, errors.New("待确认同步记录无效。")
	}
	defer clear(compressed)
	reader := bytes.NewReader(compressed)
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return d, errors.New("待确认同步记录无效。")
	}
	gz.Multistream(false)
	raw, readErr := io.ReadAll(io.LimitReader(gz, cloudsync.MaxPlaintext+1))
	closeErr := gz.Close()
	defer clear(raw)
	if readErr != nil || closeErr != nil || len(raw) > cloudsync.MaxPlaintext || reader.Len() != 0 {
		return d, errors.New("待确认同步记录无效。")
	}
	return syncapp.DecodeDocument(raw)
}

func (s *SyncService) saveSyncReceipt(ctx context.Context, p syncPlan, candidate []byte) error {
	path, err := s.cloud.store.SyncReceiptPath()
	if err != nil {
		return err
	}
	local, localKey, err := sealLocalReceiptBody(syncapp.NewDocument(0, p.saved.IncludeCredentials, p.at, p.local))
	if err != nil {
		return err
	}
	defer clear(local)
	defer clear(localKey)
	info := syncReceipt{
		Scope:     receiptScope(p.saved),
		Baseline:  sha256.Sum256(p.record.State),
		Published: sha256.Sum256(candidate),
		Local:     sha256.Sum256(local),
		LocalKey:  localKey,
	}
	raw, _ := json.Marshal(info)
	header, err := s.cloud.protector.Protect(raw, syncReceiptPurpose)
	clear(raw)
	if err != nil {
		return err
	}
	defer clear(header)
	if len(header) == 0 || len(header) > maxReceiptHeader {
		return errors.New("无法安全记录待确认的同步写入。")
	}
	blob := make([]byte, 4, 4+len(header)+len(local))
	defer func() { clear(blob) }()
	binary.BigEndian.PutUint32(blob, uint32(len(header)))
	blob = append(blob, header...)
	blob = append(blob, local...)
	if err = cloudsync.WriteArchive(ctx, path, blob); err != nil {
		return errors.New("无法安全记录待确认的同步写入。")
	}
	return nil
}

func (s *SyncService) reconcileSyncReceipt(p *syncPlan, document syncapp.Document, published []byte) error {
	if p.restoring || p.record.Paused {
		return nil
	}
	path, err := s.cloud.store.SyncReceiptPath()
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	var length [4]byte
	if _, err = io.ReadFull(f, length[:]); err != nil {
		return nil
	}
	size := int(binary.BigEndian.Uint32(length[:]))
	if size < 1 || size > maxReceiptHeader {
		return nil
	}
	header := make([]byte, size)
	if _, err = io.ReadFull(f, header); err != nil {
		return nil
	}
	raw, err := s.cloud.protector.Unprotect(header, syncReceiptPurpose)
	if err != nil {
		return nil
	}
	defer clear(raw)
	var info syncReceipt
	defer func() { clear(info.LocalKey) }()
	if json.Unmarshal(raw, &info) != nil {
		return nil
	}
	if len(info.LocalKey) != 32 || info.Scope != receiptScope(p.saved) || info.Baseline != sha256.Sum256(p.record.State) || info.Published != sha256.Sum256(published) {
		return nil
	}
	local, err := io.ReadAll(io.LimitReader(f, cloudsync.MaxBlob+1))
	defer clear(local)
	if err != nil || len(local) > cloudsync.MaxBlob || info.Local != sha256.Sum256(local) {
		return nil
	}
	captured, err := openLocalReceiptBody(local, info.LocalKey)
	if err != nil {
		return err
	}
	defer syncmodel.ClearPayload(captured.Workspace)

	rebased, conflicts, err := syncmodel.Merge(captured.Workspace, p.local, document.Workspace, nil)
	if err != nil {
		syncmodel.ClearPayload(rebased)
		return err
	}
	if len(conflicts) != 0 {
		syncmodel.ClearPayload(rebased)
		choices := make(map[string]string, len(conflicts))
		for _, conflict := range conflicts {
			choices[conflict.ID] = "local"
		}
		rebased, _, err = syncmodel.Merge(captured.Workspace, p.local, document.Workspace, choices)
		if err != nil {
			syncmodel.ClearPayload(rebased)
			return err
		}
	}
	syncmodel.ClearPayload(p.base)
	syncmodel.ClearPayload(p.local)
	p.base, p.local, p.receipt = document.Workspace, rebased, &document
	return nil
}

func (s *SyncService) commitSyncReceipt(ctx context.Context, p *syncPlan) (bool, error) {
	if p.receipt == nil {
		return false, nil
	}
	data, err := s.materialize(ctx, p.local, p.saved.IncludeCredentials)
	if err != nil {
		return false, err
	}
	p.saved.LastSync = time.Now().UnixMilli()
	p.saved.Issue = ""
	var r sqlite.SyncRecord
	if len(p.remoteBlob) > 0 {
		r, err = s.recordWithState(p.record, p.saved, p.remoteBlob)
	} else {
		r, err = s.record(p.record, p.saved, p.receipt)
	}
	if err != nil {
		return false, err
	}
	if err = s.cloud.store.ApplySync(ctx, p.revision, data, r); err != nil {
		return false, err
	}
	r.Version++
	p.record = r
	p.baseGeneration = p.receipt.Generation
	p.revision++
	nextHash := syncmodel.PayloadHash(p.local)
	pulled := p.hash != nextHash
	p.hash = nextHash
	return pulled, nil
}

func (s *SyncService) clearSyncReceipt() {
	if path, err := s.cloud.store.SyncReceiptPath(); err == nil {
		if err := os.Remove(path); err == nil {
			_ = durable.SyncDir(filepath.Dir(path))
		}
	}
}
