package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

const prefix = "enc:v1:"

func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte("torchwood-secretbox:" + secret))
	return sum[:]
}

// Encrypt seals plaintext with AES-256-GCM using secret as key material.
func Encrypt(plaintext, secret string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if secret == "" {
		return "", errors.New("encryption secret is required")
	}
	block, err := aes.NewCipher(deriveKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt opens a value produced by Encrypt. Legacy plaintext values pass through.
func Decrypt(stored, secret string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil
	}
	if secret == "" {
		return "", errors.New("encryption secret is required")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(deriveKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
