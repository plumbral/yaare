package ipc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"yaare/internal/app/vault"

	"github.com/egoist/mygo"
	cryptossh "golang.org/x/crypto/ssh"
	"yaare/internal/adapters/keyfile"
	sshclient "yaare/internal/adapters/ssh"
	"yaare/internal/domain/workspace"
)

type KeySelection struct {
	Token     string `json:"token"`
	Name      string `json:"name"`
	Format    string `json:"format"`
	PublicKey string `json:"publicKey,omitempty"`
}
type keySelection struct {
	KeySelection
	path    string
	cipher  []byte
	expires time.Time
}

type SaveKeyRequest struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	Token      string `json:"token"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}

func (s *KeyService) Pick(ctx context.Context) (*KeySelection, error) {
	page, window := callerPage(ctx), mygo.CallerWindow(ctx)
	if page == nil || window == nil {
		return nil, errors.New("请在桌面窗口中选择私钥。")
	}
	if !s.picking.CompareAndSwap(false, true) {
		return nil, errors.New("文件选择框已经打开。")
	}
	defer s.picking.Store(false)
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: uiText(callerPage(ctx), "选择 SSH 私钥"), ButtonLabel: uiText(callerPage(ctx), "选择私钥"), Filters: []mygo.FileFilter{{Name: uiText(callerPage(ctx), "私钥文件（含无扩展名）"), Extensions: []string{"*"}}}})
	if err != nil {
		return nil, errors.New("无法打开文件选择框，请重试。")
	}
	if ctx.Err() != nil || len(paths) == 0 {
		return nil, nil
	}
	return s.pickFile(page, paths[0])
}

func (s *KeyService) pickFile(page *callerScope, path string) (*KeySelection, error) {
	path, format, err := vault.InspectFile(path)
	if err != nil {
		return nil, err
	}
	selection := KeySelection{Token: workspace.NewID(), Name: filepath.Base(path), Format: format}
	if selection.Token == "" {
		return nil, errors.New("无法选择密钥，请重试。")
	}
	return s.rememberSelection(page, keySelection{KeySelection: selection, path: path})
}

func (s *KeyService) Generate(ctx context.Context) (*KeySelection, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, errors.New("密钥生成失败，请重试。")
	}
	defer clear(private)
	pk, err := cryptossh.NewPublicKey(public)
	if err != nil {
		return nil, errors.New("密钥生成失败，请重试。")
	}
	token := workspace.NewID()
	if token == "" {
		return nil, errors.New("密钥生成失败，请重试。")
	}
	cipher, err := s.protector.Protect(private, "key-draft:"+token)
	if err != nil {
		return nil, err
	}
	return s.rememberSelection(callerPage(ctx), keySelection{
		KeySelection: KeySelection{Token: token, Name: "Ed25519", Format: "OpenSSH", PublicKey: strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(pk)))},
		cipher:       cipher,
	})
}

func (s *KeyService) rememberSelection(page *callerScope, selection keySelection) (*KeySelection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selections == nil {
		s.selections = make(map[*callerScope]keySelection)
	}
	for owner, old := range s.selections {
		if time.Now().After(old.expires) {
			clear(old.cipher)
			delete(s.selections, owner)
		}
	}
	if _, exists := s.selections[page]; !exists && len(s.selections) >= 16 {
		return nil, errors.New("打开的密钥编辑过多，请关闭后重试。")
	}
	selection.expires = time.Now().Add(30 * time.Minute)
	s.selections[page] = selection
	return &selection.KeySelection, nil
}
func (s *KeyService) Discard(ctx context.Context, token string) {
	page := callerPage(ctx)
	if page == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if selection, ok := s.selections[page]; ok && selection.Token == token {
		clear(selection.cipher)
		delete(s.selections, page)
	}
}

func (s *KeyService) selectedFile(ctx context.Context, token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	selection, ok := s.selections[callerPage(ctx)]
	if !ok || selection.Token != token || time.Now().After(selection.expires) {
		return ""
	}
	return selection.path
}

func (s *KeyService) takeTemporaryFile(ctx context.Context, token string) string {
	page := callerPage(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	selection, ok := s.selections[page]
	if !ok || selection.Token != token || selection.path == "" || time.Now().After(selection.expires) {
		return ""
	}
	delete(s.selections, page)
	return selection.path
}
func (s *KeyService) Save(ctx context.Context, r SaveKeyRequest) (*KeyReference, error) {
	if err := workspaceCaller(ctx); err != nil {
		return nil, err
	}
	path := ""
	if r.Source == "file" {
		s.mu.Lock()
		selection, ok := s.selections[callerPage(ctx)]
		s.mu.Unlock()
		if !ok || selection.Token != r.Token || selection.path == "" || time.Now().After(selection.expires) {
			return nil, errors.New("请选择私钥文件，原选择可能已过期。")
		}
		path = selection.path
	}
	if r.Source == "generated" {
		return s.saveGenerated(ctx, r)
	}
	result, err := s.saveKey(ctx, r, path)
	if err == nil {
		s.Discard(ctx, r.Token)
	}
	return result, err
}

func (s *KeyService) saveGenerated(ctx context.Context, r SaveKeyRequest) (*KeyReference, error) {
	if r.PrivateKey != "" || r.Passphrase != "" {
		return nil, errors.New("私钥或口令超过支持长度。")
	}
	material, err := s.generatedPrivateKey(ctx, r.Token)
	if err != nil {
		return nil, err
	}
	defer clear(material)
	ref, err := s.saveKeyInput(ctx, vault.KeyInput{Name: r.Name, Source: "text", PrivateKey: material}, "")
	if err == nil {
		s.Discard(ctx, r.Token)
	}
	return ref, err
}

func (s *KeyService) generatedPrivateKey(ctx context.Context, token string) ([]byte, error) {
	page := callerPage(ctx)
	s.mu.Lock()
	draft, ok := s.selections[page]
	ok = page != nil && ok && draft.Token == token && len(draft.cipher) != 0 && time.Now().Before(draft.expires)
	var cipherCopy []byte
	if ok {
		cipherCopy = append([]byte(nil), draft.cipher...)
	}
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("请重新生成密钥。")
	}
	defer clear(cipherCopy)
	raw, err := s.protector.Unprotect(cipherCopy, "key-draft:"+token)
	defer clear(raw)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("请重新生成密钥。")
	}
	block, err := cryptossh.MarshalPrivateKey(ed25519.PrivateKey(raw), "")
	if err != nil {
		return nil, errors.New("密钥保存失败。")
	}
	defer clear(block.Bytes)
	return pem.EncodeToMemory(block), nil
}

func (s *KeyService) PublicKey(ctx context.Context, id string) (string, error) {
	if err := workspaceCaller(ctx); err != nil {
		return "", err
	}
	_, material, passphrase, release, err := s.acquireMaterial(id)
	if err != nil {
		return "", err
	}
	defer release()
	if sshclient.IsEncryptedPKCS8(material) {
		return "", vault.ErrEncryptedPKCS8
	}
	var signer cryptossh.Signer
	if len(passphrase) == 0 {
		signer, err = cryptossh.ParsePrivateKey(material)
	} else {
		signer, err = cryptossh.ParsePrivateKeyWithPassphrase(material, passphrase)
	}
	if err != nil {
		return "", errors.New("无法读取公钥。")
	}
	return strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

func (s *KeyService) CopyPrivateKey(ctx context.Context, id string) error {
	if callerPage(ctx) == nil || mygo.CallerWindow(ctx) == nil {
		return errors.New("请在桌面窗口中复制。")
	}
	path, material, _, release, err := s.acquireMaterial(id)
	if err != nil {
		return err
	}
	defer release()
	if path != "" {
		return errors.New("保存的密钥无效。")
	}
	return writeClipboardText(string(material))
}

func (s *KeyService) saveKey(ctx context.Context, r SaveKeyRequest, path string) (*KeyReference, error) {
	if len(r.PrivateKey) > 1<<20 || len(r.Passphrase) > 16384 {
		return nil, errors.New("私钥或口令超过支持长度。")
	}
	input := vault.KeyInput{Name: r.Name, Source: r.Source, Token: r.Token, PrivateKey: []byte(r.PrivateKey), Passphrase: []byte(r.Passphrase)}
	defer clear(input.PrivateKey)
	defer clear(input.Passphrase)
	return s.saveKeyInput(ctx, input, path)
}

func (s *KeyService) saveKeyInput(ctx context.Context, input vault.KeyInput, path string) (*KeyReference, error) {
	key, err := vault.PrepareKey(s.protector, input, path)
	if err != nil {
		return nil, err
	}
	source := "file"
	if key.Path == "" {
		source = "stored"
	}
	ref := KeyReference{ID: key.ID, Name: key.Name, Format: key.Format, Available: true, Source: source}
	if s.repository != nil {
		files, err := s.metadata(ctx)
		if err != nil {
			return nil, err
		}
		if len(files) >= workspace.MaxSavedKeys {
			return nil, errors.New("最多保存 256 个密钥。")
		}
		totalCipher := len(key.Cipher)
		for _, old := range files {
			totalCipher += old.cipherLen
		}
		if totalCipher > workspace.MaxTotalKeyCipherBytes {
			return nil, errors.New("已保存密钥达到容量上限，请先删除不用的密钥。")
		}
		for _, old := range files {
			if keyfile.SamePath(old.path, key.Path) {
				return nil, errors.New("该私钥文件已经添加，请使用已有密钥。")
			}
		}
		if _, err = s.repository.RegisterKey(ctx, key); err != nil {
			return nil, err
		}
		return &ref, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.files) >= workspace.MaxSavedKeys {
		return nil, errors.New("最多保存 256 个密钥。")
	}
	totalCipher := len(key.Cipher)
	for _, old := range s.files {
		totalCipher += len(old.cipher)
	}
	if totalCipher > workspace.MaxTotalKeyCipherBytes {
		return nil, errors.New("已保存密钥达到容量上限，请先删除不用的密钥。")
	}
	for _, old := range s.files {
		if keyfile.SamePath(old.path, key.Path) {
			return nil, errors.New("该私钥文件已经添加，请使用已有密钥。")
		}
	}
	s.files[key.ID] = keyFile{reference: ref, path: key.Path, cipher: key.Cipher, cipherLen: len(key.Cipher)}
	return &ref, nil
}

func (s *KeyService) acquireMaterial(id string) (path string, material, passphrase []byte, release func(), err error) {
	if !s.reserveUse(id) {
		return "", nil, nil, nil, errors.New("密钥不可用，请重新添加。")
	}
	committed := false
	defer func() {
		if !committed {
			clear(material)
			clear(passphrase)
			s.releaseUse(id)
		}
	}()
	key, ok, err := s.loadKey(context.Background(), id)
	if err != nil {
		return "", nil, nil, nil, errors.New("密钥不可用。")
	}
	if !ok {
		return "", nil, nil, nil, errors.New("密钥不可用，请重新添加。")
	}
	material, passphrase, err = vault.OpenMaterial(s.protector, id, key.path, key.cipher)
	if err != nil {
		return "", nil, nil, nil, err
	}
	committed = true
	var once sync.Once
	release = func() {
		once.Do(func() {
			clear(material)
			clear(passphrase)
			s.releaseUse(id)
		})
	}
	return key.path, material, passphrase, release, nil
}
