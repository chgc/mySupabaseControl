package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SecretGenerator produces cryptographic secrets used during project creation.
type SecretGenerator interface {
	// RandomHex returns a random hex string of the given character length.
	RandomHex(length int) (string, error)

	// RandomAlphanumeric returns a random alphanumeric string of the given character length.
	RandomAlphanumeric(length int) (string, error)

	// GenerateJWT signs a JWT token with the given secret and role claim.
	// expiry is the token lifetime in seconds.
	GenerateJWT(secret string, role string, expiry int) (string, error)
}

// cryptoSecretGenerator is the production implementation backed by crypto/rand.
type cryptoSecretGenerator struct{}

// NewSecretGenerator returns the production SecretGenerator.
func NewSecretGenerator() SecretGenerator {
	return &cryptoSecretGenerator{}
}

func (g *cryptoSecretGenerator) RandomHex(length int) (string, error) {
	// Each hex char encodes 4 bits, so we need length/2 bytes (rounded up).
	bytesNeeded := (length + 1) / 2
	b := make([]byte, bytesNeeded)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random hex generation failed: %w", err)
	}
	return hex.EncodeToString(b)[:length], nil
}

func (g *cryptoSecretGenerator) RandomAlphanumeric(length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	// Use rejection sampling with crypto/rand to avoid modulo bias.
	raw := make([]byte, length*2)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("random alphanumeric generation failed: %w", err)
	}
	written := 0
	for i := 0; written < length; i++ {
		if i >= len(raw) {
			extra := make([]byte, length)
			if _, err := rand.Read(extra); err != nil {
				return "", fmt.Errorf("random alphanumeric generation failed: %w", err)
			}
			raw = append(raw, extra...)
		}
		// Only accept bytes below 256 - (256 % len(alphabet)) to eliminate bias.
		cutoff := byte(256 - (256 % len(alphabet)))
		if raw[i] >= cutoff {
			continue
		}
		b[written] = alphabet[int(raw[i])%len(alphabet)]
		written++
	}
	return string(b), nil
}

func (g *cryptoSecretGenerator) GenerateJWT(secret string, role string, expiry int) (string, error) {
	claims := jwt.MapClaims{
		"role": role,
		"iss":  "supabase-control-plane",
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(time.Duration(expiry) * time.Second).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("JWT signing failed: %w", err)
	}
	return signed, nil
}

// GenerateProjectSecrets generates all per-project secrets for a new project.
// Returns a map of env-var key → value.
func GenerateProjectSecrets(gen SecretGenerator) (map[string]string, error) {
	jwtSecret, err := gen.RandomHex(64)
	if err != nil {
		return nil, fmt.Errorf("generating JWT_SECRET: %w", err)
	}

	anonKey, err := gen.GenerateJWT(jwtSecret, "anon", 3600)
	if err != nil {
		return nil, fmt.Errorf("generating ANON_KEY: %w", err)
	}

	serviceRoleKey, err := gen.GenerateJWT(jwtSecret, "service_role", 3600)
	if err != nil {
		return nil, fmt.Errorf("generating SERVICE_ROLE_KEY: %w", err)
	}

	pairs := []struct{ key, generator string }{
		{"POSTGRES_PASSWORD", "alphanum32"},
		{"DASHBOARD_PASSWORD", "alphanum32"},
		{"SECRET_KEY_BASE", "hex64"},
		{"VAULT_ENC_KEY", "alphanum32"},
		{"PG_META_CRYPTO_KEY", "alphanum32"},
		{"LOGFLARE_PUBLIC_ACCESS_TOKEN", "alphanum32"},
		{"LOGFLARE_PRIVATE_ACCESS_TOKEN", "alphanum32"},
		{"S3_PROTOCOL_ACCESS_KEY_ID", "alphanum32"},
		{"S3_PROTOCOL_ACCESS_KEY_SECRET", "alphanum64"},
	}

	secrets := map[string]string{
		"JWT_SECRET":      jwtSecret,
		"ANON_KEY":        anonKey,
		"SERVICE_ROLE_KEY": serviceRoleKey,
	}

	for _, p := range pairs {
		var val string
		switch {
		case p.generator == "alphanum32":
			val, err = gen.RandomAlphanumeric(32)
		case p.generator == "alphanum64":
			val, err = gen.RandomAlphanumeric(64)
		case strings.HasPrefix(p.generator, "hex"):
			val, err = gen.RandomHex(64)
		}
		if err != nil {
			return nil, fmt.Errorf("generating %s: %w", p.key, err)
		}
		secrets[p.key] = val
	}

	return secrets, nil
}
