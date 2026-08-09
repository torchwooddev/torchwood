package idgen

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
)

const (
	RandomCharsetNumeric      = "numeric"
	RandomCharsetAlphanumeric = "alphanumeric"

	defaultRandomLength     = 10
	defaultRandomMaxRetries = 10
	maxRandomLength         = 32
)

var (
	// ErrRandomReservationFailed indicates all reservation attempts collided or Redis rejected the ID.
	ErrRandomReservationFailed = errors.New("failed to reserve random id")
	// ErrRandomRedisRequired indicates random strategy requires a Redis client.
	ErrRandomRedisRequired = errors.New("random id strategy requires redis")
)

const numericAlphabet = "0123456789"
const alphanumericAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// RandomConfig controls fixed-length random identifiers.
type RandomConfig struct {
	Length     int
	Charset    string
	MaxRetries int
}

// WithDefaults returns cfg with platform defaults applied.
func (c RandomConfig) WithDefaults() RandomConfig {
	return c.normalized()
}

func (c RandomConfig) normalized() RandomConfig {
	out := c
	if out.Length <= 0 {
		out.Length = defaultRandomLength
	}
	if out.Length > maxRandomLength {
		out.Length = maxRandomLength
	}
	if out.Charset == "" {
		out.Charset = RandomCharsetNumeric
	}
	if out.MaxRetries <= 0 {
		out.MaxRetries = defaultRandomMaxRetries
	}
	return out
}

// RandomString generates a candidate random identifier (no deduplication).
func RandomString(cfg RandomConfig) (string, error) {
	cfg = cfg.normalized()
	alphabet := numericAlphabet
	if cfg.Charset == RandomCharsetAlphanumeric {
		alphabet = alphanumericAlphabet
	}
	max := big.NewInt(int64(len(alphabet)))
	out := make([]byte, cfg.Length)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("random id generation failed: %w", err)
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}
