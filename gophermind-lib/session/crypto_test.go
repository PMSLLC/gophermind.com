package session

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := deriveKey("correct horse battery staple")
	plain := []byte(`{"role":"user","content":"secret history"}` + "\n")

	ct, err := encryptBytes(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("secret history")) {
		t.Error("ciphertext leaks plaintext")
	}
	got, err := decryptBytes(key, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("round trip mismatch: %q", got)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	ct, _ := encryptBytes(deriveKey("key-a"), []byte("data"))
	if _, err := decryptBytes(deriveKey("key-b"), ct); err == nil {
		t.Error("decrypt with wrong key should fail")
	}
}

func TestDecryptTamperFails(t *testing.T) {
	key := deriveKey("k")
	ct, _ := encryptBytes(key, []byte("data"))
	ct[len(ct)-1] ^= 0xff // flip a byte in the tag/ciphertext
	if _, err := decryptBytes(key, ct); err == nil {
		t.Error("tampered ciphertext should fail authentication")
	}
}

func TestIsEncryptedMagic(t *testing.T) {
	if !isEncrypted(append([]byte(encMagic), 1, 2, 3)) {
		t.Error("magic-prefixed data should be detected as encrypted")
	}
	if isEncrypted([]byte(`{"role":"user"}`)) {
		t.Error("plain JSONL must not be detected as encrypted")
	}
	if isEncrypted([]byte("hi")) {
		t.Error("short data must not panic or match")
	}
}
