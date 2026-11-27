package ipc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"yaare/internal/app/vault"

	"github.com/egoist/mygo"
	"yaare/internal/adapters/keyfile"
	"yaare/internal/adapters/secrets"
	"yaare/internal/domain/workspace"
	"yaare/internal/ports"
)

type KeyReference struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Format    string `json:"format"`
	Available bool   `json:"available"`
	Source    string `json:"source,omitempty"`
}

type keyFile struct {
	reference KeyReference
	path      string
	cipher    []byte
	cipherLen int
}

type KeyService struct {
	mu         sync.Mutex
	files      map[string]keyFile
	picking    atomic.Bool
	repository ports.KeyRepository
	uses       map[string]int
	forgetting map[string]int
	selections map[*callerScope]keySelection
	protector  secrets.Protector
}

func NewKeyService() *KeyService {
	return &KeyService{files: make(map[string]keyFile), protector: secrets.CurrentUser{}}
}

func NewPersistentKeyService(repository ports.KeyRepository) *KeyService {
	s := NewKeyService()
	s.repository = repository
	return s
}

func (s *KeyService) reserveUse(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.forgetting[id] > 0 {
		return false
	}
	if s.uses == nil {
		s.uses = map[string]int{}
	}
	s.uses[id]++
	return true
}

func (s *KeyService) releaseUse(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uses[id]--
	if s.uses[id] <= 0 {
		delete(s.uses, id)
	}
}

func (s *KeyService) metadata(ctx context.Context) (map[string]keyFile, error) {
	if s.repository == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		files := make(map[string]keyFile, len(s.files))
		for id, f := range s.files {
			f.cipherLen = len(f.cipher)
			files[id] = f
		}
		return files, nil
	}
	keys, err := s.repository.KeyMetadata(ctx)
	if err != nil {
		return nil, err
	}
	files := make(map[string]keyFile, len(keys))
	for _, key := range keys {
		source := "file"
		if key.Path == "" {
			source = "stored"
		}
		files[key.ID] = keyFile{reference: KeyReference{ID: key.ID, Name: key.Name, Format: key.Format, Source: source}, path: key.Path, cipherLen: key.CipherLen}
	}
	return files, nil
}

func (s *KeyService) loadKey(ctx context.Context, id string) (keyFile, bool, error) {
	if s.repository == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		f, ok := s.files[id]
		if ok {
			f.cipherLen = len(f.cipher)
		}
		return f, ok, nil
	}
	key, found, err := s.repository.Key(ctx, id)
	if err != nil || !found {
		return keyFile{}, false, err
	}
	source := "file"
	if key.Path == "" {
		source = "stored"
	}
	return keyFile{reference: KeyReference{ID: key.ID, Name: key.Name, Format: key.Format, Source: source}, path: key.Path, cipher: key.Cipher, cipherLen: len(key.Cipher)}, true, nil
}

func (s *KeyService) available(ctx context.Context, id string) (bool, error) {
	file, ok, err := s.loadKey(ctx, id)
	if err != nil || !ok {
		return false, err
	}
	return vault.FileAvailable(file.path) || (file.path == "" && file.cipherLen > 0), nil
}

func (s *KeyService) List(ctx context.Context) ([]KeyReference, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	files, err := s.metadata(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]KeyReference, 0, len(files))
	for _, file := range files {
		ref := file.reference
		ref.Available = vault.FileAvailable(file.path) || (file.path == "" && file.cipherLen > 0)
		result = append(result, ref)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *KeyService) Choose(ctx context.Context) (*KeyReference, error) {
	window := mygo.CallerWindow(ctx)
	if window == nil {
		return nil, errors.New("请在桌面窗口中选择私钥")
	}
	if !s.picking.CompareAndSwap(false, true) {
		return nil, errors.New("文件选择框已经打开")
	}
	defer s.picking.Store(false)
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: window, Title: uiText(callerPage(ctx), "选择 SSH 私钥"), ButtonLabel: uiText(callerPage(ctx), "选择私钥"),
		Filters: []mygo.FileFilter{{Name: uiText(callerPage(ctx), "私钥文件（含无扩展名）"), Extensions: []string{"*"}}},
	})
	if err != nil {
		return nil, errors.New("无法打开文件选择框，请重试")
	}
	if ctx.Err() != nil || len(paths) == 0 {
		return nil, nil
	}
	key, err := s.saveKey(ctx, SaveKeyRequest{Source: "file", Name: filepath.Base(paths[0])}, paths[0])
	if errors.Is(err, vault.ErrPassphraseMissing) {
		return nil, errors.New("密钥不可用或无法解锁，请在密钥页重新添加。")
	}
	return key, err
}

func (s *KeyService) Forget(ctx context.Context, id string) error {
	if s.repository != nil && callerPage(ctx) == nil {
		return errors.New("请在桌面窗口中管理密钥引用。")
	}
	return s.forget(ctx, id)
}
func (s *KeyService) forget(ctx context.Context, id string) error {
	s.mu.Lock()
	if s.uses[id] > 0 {
		s.mu.Unlock()
		return errors.New("此密钥仍被打开的 SSH 会话使用，请先关闭会话。")
	}
	if s.repository == nil {
		delete(s.files, id)
		s.mu.Unlock()
		return nil
	}
	if s.forgetting == nil {
		s.forgetting = map[string]int{}
	}
	s.forgetting[id]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.forgetting[id]--
		if s.forgetting[id] <= 0 {
			delete(s.forgetting, id)
		}
		s.mu.Unlock()
	}()
	return s.repository.ForgetKey(ctx, id)
}

func (s *KeyService) acquireForSSH(id string) (string, func(), bool) {
	if !s.reserveUse(id) {
		return "", nil, false
	}
	file, ok, err := s.loadKey(context.Background(), id)
	if err != nil || !ok || !vault.FileAvailable(file.path) {
		s.releaseUse(id)
		return "", nil, false
	}
	var once sync.Once
	return file.path, func() {
		once.Do(func() { s.releaseUse(id) })
	}, true
}

func (s *KeyService) register(path string) (*KeyReference, error) {
	canonical, format, err := vault.InspectFile(path)
	if err != nil {
		return nil, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, errors.New("无法创建密钥引用，请重试")
	}
	ref := KeyReference{ID: hex.EncodeToString(id[:]), Name: filepath.Base(canonical), Format: format, Available: true, Source: "file"}
	if s.repository != nil {
		files, err := s.metadata(context.Background())
		if err != nil {
			return nil, err
		}
		for _, existing := range files {
			if keyfile.SamePath(existing.path, canonical) {
				result := existing.reference
				result.Available = true
				return &result, nil
			}
		}
		if len(files) >= workspace.MaxSavedKeys {
			return nil, errors.New("最多保留 256 个密钥引用，请先移除不用的引用")
		}
		saved, err := s.repository.RegisterKey(context.Background(), workspace.SavedKey{ID: ref.ID, Name: ref.Name, Format: ref.Format, Path: canonical})
		if err != nil {
			return nil, err
		}
		ref.ID = saved.ID
		return &ref, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.files {
		if keyfile.SamePath(existing.path, canonical) {
			result := existing.reference
			result.Available = true
			return &result, nil
		}
	}
	if len(s.files) >= workspace.MaxSavedKeys {
		return nil, errors.New("最多保留 256 个密钥引用，请先移除不用的引用")
	}
	s.files[ref.ID] = keyFile{reference: ref, path: canonical}
	return &ref, nil
}
