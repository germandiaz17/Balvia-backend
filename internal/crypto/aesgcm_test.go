package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestAESGCM_RoundTrip(t *testing.T) {
	box, err := NewAESGCM(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	const secret = "sk-ant-super-secret-key-123"
	ct, err := box.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte(secret)) {
		t.Fatal("ciphertext leaks plaintext")
	}
	got, err := box.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != secret {
		t.Fatalf("round-trip mismatch: got %q", got)
	}
}

func TestAESGCM_TwoEncryptionsDiffer(t *testing.T) {
	box, _ := NewAESGCM(newKey(t))
	a, _ := box.Encrypt("same")
	b, _ := box.Encrypt("same")
	if bytes.Equal(a, b) {
		t.Fatal("nonce reuse: identical ciphertexts for same plaintext")
	}
}

func TestAESGCM_WrongKeyFails(t *testing.T) {
	box1, _ := NewAESGCM(newKey(t))
	box2, _ := NewAESGCM(newKey(t))
	ct, _ := box1.Encrypt("secret")
	if _, err := box2.Decrypt(ct); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestNewAESGCM_BadKeyLength(t *testing.T) {
	if _, err := NewAESGCM(make([]byte, 16)); err == nil {
		t.Fatal("expected error for non-32-byte key")
	}
}
