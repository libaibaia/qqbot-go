package connector

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
)

// DecryptSecret decrypts the AES-256-GCM encrypted appSecret.
//
// The ciphertext format (base64-encoded):
//
//	[12 bytes IV][ciphertext][16 bytes GCM Auth Tag]
//
// The key is the 32-byte random key generated during create_bind_task (base64-encoded).
func DecryptSecret(keyBase64, encryptedBase64 string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return "", fmt.Errorf("decode key: %w", err)
	}
	if len(key) != 32 {
		return "", fmt.Errorf("invalid key length: %d (expected 32)", len(key))
	}

	encrypted, err := base64.StdEncoding.DecodeString(encryptedBase64)
	if err != nil {
		return "", fmt.Errorf("decode encrypted: %w", err)
	}

	// Minimum: 12 (IV) + 1 (min ciphertext) + 16 (tag) = 29
	if len(encrypted) < 29 {
		return "", fmt.Errorf("encrypted data too short: %d bytes", len(encrypted))
	}

	iv := encrypted[:12]
	tag := encrypted[len(encrypted)-16:]
	ciphertext := encrypted[12 : len(encrypted)-16]

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	// GCM expects nonce + ciphertext together; we append the tag
	plaintext, err := aesGCM.Open(nil, iv, append(ciphertext, tag...), nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}
