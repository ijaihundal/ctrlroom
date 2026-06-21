package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/ijaihundal/ctrlroom/internal/config"
)

const (
	saltBytes = 16
	hashBytes = 32
)

var ErrInvalidHashFormat = errors.New("invalid argon2 hash format")

func Hash(password string, cfg *config.Config) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey(
		[]byte(password), salt,
		cfg.Argon2Time, cfg.Argon2Memory, cfg.Argon2Threads,
		hashBytes,
	)
	return encode(cfg.Argon2Time, cfg.Argon2Memory, cfg.Argon2Threads, salt, key), nil
}

func Verify(encoded, password string) (bool, error) {
	params, salt, expectedHash, err := decode(encoded)
	if err != nil {
		return false, err
	}
	hashLen := uint32(len(expectedHash)) //nolint:gosec // bounded by base64-decoded input, always fits in uint32
	derived := argon2.IDKey(
		[]byte(password), salt,
		params.time, params.memory, params.parallelism,
		hashLen,
	)
	if subtle.ConstantTimeCompare(derived, expectedHash) != 1 {
		return false, nil
	}
	return true, nil
}

type argonParams struct {
	time, memory uint32
	parallelism  uint8
}

func encode(time, memory uint32, parallelism uint8, salt, key []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory, time, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func decode(encoded string) (p argonParams, salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return argonParams{}, nil, nil, fmt.Errorf("%w: expected 6 segments, got %d", ErrInvalidHashFormat, len(parts))
	}
	if parts[1] != "argon2id" {
		return argonParams{}, nil, nil, fmt.Errorf("%w: not argon2id", ErrInvalidHashFormat)
	}
	if parts[2] != "v=19" {
		return argonParams{}, nil, nil, fmt.Errorf("%w: unsupported version %q", ErrInvalidHashFormat, parts[2])
	}
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.parallelism); err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("%w: parse params: %s", ErrInvalidHashFormat, err)
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("%w: decode salt: %s", ErrInvalidHashFormat, err)
	}
	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argonParams{}, nil, nil, fmt.Errorf("%w: decode hash: %s", ErrInvalidHashFormat, err)
	}
	return p, salt, hash, nil
}
