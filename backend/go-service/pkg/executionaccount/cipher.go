package executionaccount

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

type Cipher struct{ aead cipher.AEAD }

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("credential encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}
func (c *Cipher) Encrypt(value Credential) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, payload, nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}
func (c *Cipher) Decrypt(value string) (Credential, error) {
	payload, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return Credential{}, fmt.Errorf("decode credential: %w", err)
	}
	size := c.aead.NonceSize()
	if len(payload) < size {
		return Credential{}, fmt.Errorf("invalid credential ciphertext")
	}
	plain, err := c.aead.Open(nil, payload[:size], payload[size:], nil)
	if err != nil {
		return Credential{}, fmt.Errorf("decrypt credential: %w", err)
	}
	var credential Credential
	if err := json.Unmarshal(plain, &credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}
