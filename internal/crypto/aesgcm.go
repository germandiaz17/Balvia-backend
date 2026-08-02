// Package crypto provides authenticated encryption for small secrets at rest
// (e.g. user-supplied AI API keys), using AES-256-GCM.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

// AESGCM encrypts and decrypts short secrets with AES-256-GCM. The ciphertext is
// self-describing: the random nonce is prepended to the sealed bytes.
type AESGCM struct {
	gcm cipher.AEAD
}

// NewAESGCM builds an AESGCM from a 32-byte key (AES-256).
func NewAESGCM(key []byte) (*AESGCM, error) {
	if len(key) != 32 {
		return nil, errors.New("crypto: key must be 32 bytes (AES-256)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCM{gcm: gcm}, nil
}

// Encrypt seals plaintext and returns nonce||ciphertext.
func (a *AESGCM) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, a.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// Seal appends the ciphertext to nonce (used as the dst), so the result is
	// nonce||ciphertext in one slice.
	return a.gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt opens a nonce||ciphertext blob produced by Encrypt.
func (a *AESGCM) Decrypt(blob []byte) (string, error) {
	ns := a.gcm.NonceSize()
	if len(blob) < ns {
		return "", errors.New("crypto: ciphertext too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	pt, err := a.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
