package session

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// encMagic prefixes an encrypted session file so plain and encrypted stores can
// coexist and be told apart on read.
const encMagic = "GMENC1\n"

// deriveKey turns a passphrase into a 32-byte AES-256 key via SHA-256.
func deriveKey(passphrase string) []byte {
	sum := sha256.Sum256([]byte(passphrase))
	return sum[:]
}

// sessionKey returns the derived encryption key when GOPHERMIND_SESSION_KEY is
// set, or nil when session encryption is disabled (the default).
func sessionKey() []byte {
	pass := os.Getenv("GOPHERMIND_SESSION_KEY")
	if pass == "" {
		return nil
	}
	return deriveKey(pass)
}

// encryptBytes seals plaintext with AES-256-GCM, prepending a random nonce.
func encryptBytes(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptBytes opens an AES-256-GCM blob whose nonce is prepended.
func decryptBytes(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	return gcm.Open(nil, nonce, ct, nil)
}

// isEncrypted reports whether data is an encrypted session blob (has the magic).
func isEncrypted(data []byte) bool {
	return bytes.HasPrefix(data, []byte(encMagic))
}
