package sshclient

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const maxHostTrustBytes = 8 * 1024 * 1024

type hostKeyRecord struct {
	key       ssh.PublicKey
	line      int
	exclusive bool
}

type hostTrust struct {
	raw         []hostKeyRecord
	authorities map[string]struct{}
	revoked     map[string]struct{}
}

func readHostTrust(path, address string) (hostTrust, []byte, error) {
	var trust hostTrust
	unavailable := fault("KNOWN_HOSTS_UNAVAILABLE", "无法读取已信任的主机记录。")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return trust, nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxHostTrustBytes {
		return trust, nil, unavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return trust, nil, unavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, maxHostTrustBytes+1))
	file.Close()
	if err != nil || len(data) > maxHostTrustBytes {
		return trust, nil, unavailable
	}
	trust, ok := parseHostTrust(data, address)
	if !ok {
		return hostTrust{}, nil, fault("KNOWN_HOSTS_INVALID", "主机信任记录格式无效，请检查应用的 known_hosts 文件。")
	}
	return trust, data, nil
}

func parseHostTrust(data []byte, address string) (hostTrust, bool) {
	trust := hostTrust{authorities: make(map[string]struct{}), revoked: make(map[string]struct{})}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return hostTrust{}, false
	}
	endpoint := knownhosts.Normalize(address)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		entry := bytes.TrimSpace(scanner.Bytes())
		if len(entry) == 0 || entry[0] == '#' {
			continue
		}
		fields := bytes.Fields(entry)
		if len(fields) < 3 || entry[0] == '@' && len(fields) < 4 {
			return hostTrust{}, false
		}
		marker, hosts, key, _, _, err := ssh.ParseKnownHosts(entry)
		if err != nil {
			return hostTrust{}, false
		}
		cert, isCert := key.(*ssh.Certificate)
		if marker == "revoked" {
			if isCert {
				key = cert.Key
			}
			trust.revoked[string(key.Marshal())] = struct{}{}
			continue
		}
		if marker != "" && (marker != "cert-authority" || isCert) {
			return hostTrust{}, false
		}
		matched, exclusive, valid := matchHostRecord(hosts, host, port, endpoint)
		if !valid {
			return hostTrust{}, false
		}
		if !matched {
			continue
		}
		if marker == "cert-authority" {
			trust.authorities[string(key.Marshal())] = struct{}{}
		} else if !isCert {
			trust.raw = append(trust.raw, hostKeyRecord{key, line, exclusive})
		}
	}
	return trust, scanner.Err() == nil
}

func matchHostRecord(patterns []string, host, port, endpoint string) (matched, exclusive, valid bool) {
	if len(patterns) == 1 && strings.HasPrefix(patterns[0], "|") {
		parts := strings.Split(patterns[0], "|")
		if len(parts) != 4 || parts[1] != "1" {
			return false, false, false
		}
		salt, saltErr := base64.StdEncoding.DecodeString(parts[2])
		hash, hashErr := base64.StdEncoding.DecodeString(parts[3])
		if saltErr != nil || hashErr != nil || len(salt) != sha1.Size || len(hash) != sha1.Size {
			return false, false, false
		}
		mac := hmac.New(sha1.New, salt)
		mac.Write([]byte(endpoint))
		return hmac.Equal(mac.Sum(nil), hash), true, true
	}
	exclusive = len(patterns) == 1
	negatedMatch := false
	for _, pattern := range patterns {
		pattern, negated := strings.CutPrefix(pattern, "!")
		if pattern == "" || strings.HasPrefix(pattern, "|") {
			return false, false, false
		}
		patternHost, patternPort, err := net.SplitHostPort(pattern)
		if err != nil {
			if strings.HasPrefix(pattern, "[") {
				return false, false, false
			}
			patternHost, patternPort = pattern, "22"
		}
		exclusive = exclusive && !negated && !strings.ContainsAny(patternHost, "*?")
		if patternPort == port && matchHostName(patternHost, host) {
			matched = matched || !negated
			negatedMatch = negatedMatch || negated
		}
	}
	return matched && !negatedMatch, exclusive, true
}

func matchHostName(pattern, host string) bool {
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == host
	}
	star, retry := -1, 0
	patternIndex, hostIndex := 0, 0
	for hostIndex < len(host) {
		switch {
		case patternIndex < len(pattern) && pattern[patternIndex] == '*':
			star, retry = patternIndex, hostIndex
			patternIndex++
		case patternIndex < len(pattern) && (pattern[patternIndex] == '?' || pattern[patternIndex] == host[hostIndex]):
			patternIndex++
			hostIndex++
		case star >= 0:
			retry++
			patternIndex, hostIndex = star+1, retry
		default:
			return false
		}
	}
	return strings.TrimLeft(pattern[patternIndex:], "*") == ""
}

func isHostCertAlgorithm(algorithm string) bool {
	return strings.Contains(algorithm, "-cert-v01@")
}

func (trust hostTrust) requiresCertificate() bool { return len(trust.authorities) > 0 }

func (trust hostTrust) isRevoked(key ssh.PublicKey) bool {
	if cert, ok := key.(*ssh.Certificate); ok {
		return trust.isRevoked(cert.Key) || trust.isRevoked(cert.SignatureKey)
	}
	_, revoked := trust.revoked[string(key.Marshal())]
	return revoked
}

func (trust hostTrust) algorithms(supported []string) []string {
	known := make(map[string]bool)
	for _, record := range trust.raw {
		if trust.isRevoked(record.key) {
			continue
		}
		if record.key.Type() == ssh.KeyAlgoRSA {
			known[ssh.KeyAlgoRSASHA256], known[ssh.KeyAlgoRSASHA512] = true, true
		} else {
			known[record.key.Type()] = true
		}
	}
	preferred := make([]string, 0, len(supported))
	var rest []string
	ed25519Default := false
	for _, algorithm := range supported {
		if isHostCertAlgorithm(algorithm) != trust.requiresCertificate() {
			continue
		}
		switch {
		case known[algorithm]:
			preferred = append(preferred, algorithm)
		case algorithm == ssh.KeyAlgoED25519:
			ed25519Default = true
		default:
			rest = append(rest, algorithm)
		}
	}
	if ed25519Default {
		preferred = append(preferred, ssh.KeyAlgoED25519)
	}
	return append(preferred, rest...)
}

func hostCertRequired() error {
	return fault("HOST_CERT_REQUIRED", "此地址和端口已配置主机 CA，但服务器未提供受支持的主机证书。")
}

func hostCertUnsupported() error {
	return fault("HOST_CERT_UNSUPPORTED", "此地址和端口未配置受信任的主机 CA，无法验证服务器的主机证书。")
}

func (trust hostTrust) negotiationError(err error) error {
	var negotiation *ssh.AlgorithmNegotiationError
	if !errors.As(err, &negotiation) || negotiation.What != "host key" {
		return err
	}
	if trust.requiresCertificate() {
		return hostCertRequired()
	}
	if slices.ContainsFunc(negotiation.RequestedAlgorithms, isHostCertAlgorithm) {
		return hostCertUnsupported()
	}
	return err
}

func (trust hostTrust) check(address string, key ssh.PublicKey) (bool, error) {
	if trust.isRevoked(key) {
		return false, fault("HOST_KEY_REVOKED", "服务器指纹已被撤销，连接已停止。")
	}
	if cert, ok := key.(*ssh.Certificate); ok {
		if !trust.requiresCertificate() {
			return false, hostCertUnsupported()
		}
		checker := ssh.CertChecker{IsHostAuthority: func(ca ssh.PublicKey, _ string) bool {
			_, known := trust.authorities[string(ca.Marshal())]
			return known
		}}
		if err := checker.CheckHostKey(address, nil, cert); err != nil {
			return false, fault("HOST_CERT_INVALID", "服务器主机证书无效，或不是由此地址和端口信任的 CA 签发；连接已停止。")
		}
		return true, nil
	}
	if trust.requiresCertificate() {
		return false, hostCertRequired()
	}
	presented := key.Marshal()
	return slices.ContainsFunc(trust.raw, func(record hostKeyRecord) bool {
		return bytes.Equal(record.key.Marshal(), presented)
	}), nil
}
