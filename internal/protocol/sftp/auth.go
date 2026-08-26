package sftp

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// LoadPrivateKey reads and parses an OpenSSH-compatible private key. An empty
// passphrase parses unencrypted keys and returns ssh.PassphraseMissingError for
// encrypted keys so the caller can request a secret without exposing it in
// command-line arguments.
func LoadPrivateKey(filename string, passphrase []byte) (ssh.Signer, error) {
	privateKey, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("개인키 파일 읽기 실패: %w", err)
	}
	var signer ssh.Signer
	if len(passphrase) == 0 {
		signer, err = ssh.ParsePrivateKey(privateKey)
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(privateKey, passphrase)
	}
	if err != nil {
		return nil, fmt.Errorf("개인키 해석 실패: %w", err)
	}
	return signer, nil
}
