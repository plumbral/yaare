package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type HostCredential struct {
	HostID string `json:"-"`
	Scope  string `json:"-"`
	Cipher []byte `json:"-"`
}

type HostCredentialChange struct {
	HostID     string
	Action     string
	Credential HostCredential
	Proxy      *HostCredential
	Key        *SavedKey
}

func ProxyCredentialScope(id string, proxy ProxyConfig) string {
	raw, _ := json.Marshal([]any{id, proxy})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ProxyCredentialMatches(h Host, c HostCredential) bool {
	return h.ID == c.HostID && h.Proxy.UseAuthentication && c.Scope == ProxyCredentialScope(h.ID, h.Proxy) &&
		len(c.Cipher) > 0 && len(c.Cipher) <= 65536
}

func ValidProxyPassword[T ~string | ~[]byte](p ProxyConfig, password T) bool {
	return p.UseAuthentication && len(password) > 0 &&
		(p.Kind == "http" && len(password) <= 4096 || p.Kind == "socks5" && len(password) <= 255)
}

func CredentialScope(h Host) string {
	raw, _ := json.Marshal([]any{h.ID, h.Address, h.Port, h.Username, h.Authentication, h.IdentityID, h.KeyID, h.UseAgent})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func CredentialMatches(h Host, c HostCredential) bool {
	return h.ID == c.HostID && SupportsHostCredential(h) && c.Scope == CredentialScope(h) && len(c.Cipher) > 0 && len(c.Cipher) <= 65536
}

func SupportsHostCredential(h Host) bool {
	return !h.UseAgent && (h.Authentication == "password" && h.IdentityID == "" && h.KeyID == "" ||
		h.Authentication == "key" && (h.KeyID != "" || h.IdentityID != ""))
}
