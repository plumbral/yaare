package sshclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type Fault struct{ Code, Message string }

func (f *Fault) Error() string         { return f.Message }
func fault(code, message string) error { return &Fault{code, message} }

type Challenge struct {
	ID                   string   `json:"id"`
	Address              string   `json:"address"`
	Algorithm            string   `json:"algorithm"`
	Fingerprint          string   `json:"fingerprint"`
	Name                 string   `json:"name"`
	PreviousFingerprints []string `json:"previousFingerprints,omitempty"`
}

func (c Challenge) accepts(decision string) bool {
	if decision == "reject" {
		return true
	}
	if len(c.PreviousFingerprints) > 0 {
		return decision == "replace"
	}
	return decision == "once" || decision == "remember"
}

type TrustStore struct {
	Path func() (string, error)
	mu   sync.Mutex
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func (t *TrustStore) path() (string, error) {
	path, err := t.Path()
	if err != nil {
		return "", fault("KNOWN_HOSTS_UNAVAILABLE", "无法定位应用的主机信任记录。")
	}
	return path, nil
}

func (t *TrustStore) load(path, address string) (hostTrust, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	trust, _, err := readHostTrust(path, address)
	return trust, err
}

func fingerprints(keys []hostKeyRecord) []string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, ssh.FingerprintSHA256(key.key))
	}
	slices.Sort(values)
	return slices.Compact(values)
}

func withoutHostRecords(data []byte, keys []hostKeyRecord) ([]byte, error) {
	lines := bytes.Split(data, []byte{'\n'})
	for _, key := range keys {
		if !key.exclusive {
			return nil, fault("HOST_KEY_CHANGED", "此指纹来自共享信任规则。请先在主机信任记录中核实。")
		}
		lines[key.line-1] = nil
	}
	return bytes.Join(slices.DeleteFunc(lines, func(line []byte) bool { return line == nil }), []byte{'\n'}), nil
}

func (t *TrustStore) verify(ctx context.Context, path, address string, key ssh.PublicKey, ask func(Challenge) (string, error)) error {
	trust, err := t.load(path, address)
	if err != nil {
		return err
	}
	if trusted, err := trust.check(address, key); trusted || err != nil {
		return err
	}
	previous := fingerprints(trust.raw)
	if len(previous) > 8 {
		return fault("HOST_KEY_CHANGED", "主机信任记录包含过多指纹，请先核实服务器身份。")
	}
	id := newID()
	if id == "" {
		return fault("INTERNAL", "无法创建主机验证请求。")
	}
	challenge := Challenge{ID: id, Address: address, Algorithm: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), PreviousFingerprints: previous}
	decision, err := ask(challenge)
	if err != nil {
		return err
	}
	if decision == "reject" || !challenge.accepts(decision) {
		return fault("HOST_KEY_REJECTED", "已取消服务器指纹确认。")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fault("KNOWN_HOSTS_WRITE", "无法创建主机信任目录。")
	}
	lock, err := lockHostTrust(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	t.mu.Lock()
	defer t.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	trust, data, err := readHostTrust(path, address)
	if err != nil {
		return err
	}
	if trusted, err := trust.check(address, key); trusted || err != nil {
		return err
	}
	if !slices.Equal(previous, fingerprints(trust.raw)) {
		return fault("HOST_KEY_CHANGED", "主机信任记录已变化，请重新核实服务器身份。")
	}
	if decision == "once" {
		return nil
	}
	if decision == "replace" {
		data, err = withoutHostRecords(data, trust.raw)
		if err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	endpoint := knownhosts.Normalize(address)
	if strings.ContainsAny(endpoint, "*,?!") || strings.HasPrefix(endpoint, "#") || strings.HasPrefix(endpoint, "@") || strings.HasPrefix(endpoint, "|") {
		endpoint = knownhosts.HashHostname(endpoint)
	}
	data = append(data, knownhosts.Line([]string{endpoint}, key)...)
	data = append(data, '\n')
	if len(data) > maxHostTrustBytes {
		return fault("KNOWN_HOSTS_WRITE", "无法保存服务器指纹。")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".known-hosts-*")
	if err != nil {
		return fault("KNOWN_HOSTS_WRITE", "无法保存服务器指纹。")
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return fault("KNOWN_HOSTS_WRITE", "服务器指纹保存失败，连接已停止。")
	}
	return nil
}
