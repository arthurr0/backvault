package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const Prefix = "enc:v1:"

type Cipher struct {
	aead cipher.AEAD
}

func New(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("master key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, Prefix)
}

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if IsEncrypted(plaintext) {
		return plaintext, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return Prefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(value string) (string, error) {
	if !IsEncrypted(value) {
		return value, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted value: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("encrypted value is truncated")
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", errors.New("decrypt failed: wrong master key or corrupted value")
	}
	return string(plain), nil
}
