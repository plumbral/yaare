package sshclient

import (
	"bytes"
	"encoding/pem"
)

const EncryptedPKCS8Header = "-----BEGIN ENCRYPTED PRIVATE KEY-----"
const EncryptedPKCS8Message = "不支持加密的 PKCS#8 私钥，请转换为 OpenSSH 私钥后重试。"

func IsEncryptedPKCS8(data []byte) bool {
	firstLine, _, _ := bytes.Cut(bytes.TrimSpace(data), []byte("\n"))
	if bytes.Equal(bytes.TrimSpace(firstLine), []byte(EncryptedPKCS8Header)) {
		return true
	}
	if !bytes.Contains(data, []byte(EncryptedPKCS8Header)) {
		return false
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false
	}
	clear(block.Bytes)
	return block.Type == "ENCRYPTED PRIVATE KEY"
}
