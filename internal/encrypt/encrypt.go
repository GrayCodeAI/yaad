// Package encrypt provides at-rest encryption for the Yaad SQLite database.
// Uses AES-256-GCM to encrypt the entire DB file with a user-provided key.
// Based on Engram's E2E encryption approach.
//
// Usage:
//   yaad init --encrypt          # generates key, saves to ~/.yaad/key
//   yaad init --key <base64key>  # use provided key
//
// The DB is encrypted on disk and decrypted in memory when opened.
// This is file-level encryption, not SQLCipher (which requires CGO).
package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"
)

// GenerateKey creates a new 256-bit encryption key.
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// EncryptFile encrypts a file in-place using AES-256-GCM.
func EncryptFile(path, keyBase64 string) error {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return err
	}
	plaintext, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return os.WriteFile(path, ciphertext, 0600)
}

// DecryptFile decrypts a file in-place using AES-256-GCM.
func DecryptFile(path, keyBase64 string) error {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return err
	}
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return os.ErrInvalid
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return err
	}
	return os.WriteFile(path, plaintext, 0600)
}

// IsEncrypted checks if a file appears to be encrypted (not valid SQLite header).
func IsEncrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	header := make([]byte, 16)
	n, _ := f.Read(header)
	if n < 16 {
		return false
	}
	// SQLite files start with "SQLite format 3\000"
	return string(header[:6]) != "SQLite"
}
