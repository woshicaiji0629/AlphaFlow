package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 2
	saltLength          = 16
	keyLength    uint32 = 32
)

type Hasher struct{}

func (Hasher) Hash(value string) (string, error)          { return HashPassword(value) }
func (Hasher) Verify(encoded, value string) (bool, error) { return VerifyPassword(encoded, value) }

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	memory, timeCost, threads, salt, expected, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func ValidatePassword(password string) error {
	if len(password) < 12 {
		return fmt.Errorf("password must contain at least 12 characters")
	}
	if len(password) > 256 {
		return fmt.Errorf("password must not exceed 256 characters")
	}
	return nil
}

func parseHash(encoded string) (uint32, uint32, uint8, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid password hash format")
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("unsupported argon2 version")
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid argon2 parameters: %w", err)
	}
	if memory == 0 || memory > 1024*1024 || timeCost == 0 || timeCost > 20 || threads == 0 || threads > 32 {
		return 0, 0, 0, nil, nil, fmt.Errorf("argon2 parameters out of range")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid argon2 salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid argon2 key")
	}
	return memory, timeCost, threads, salt, key, nil
}
