package domain_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kevin/supabase-control-plane/internal/domain"
)

// ─── SecretGenerator ─────────────────────────────────────────────────────────

func TestSecretGenerator_RandomHex(t *testing.T) {
	gen := domain.NewSecretGenerator()

	lengths := []int{32, 64, 16}
	for _, l := range lengths {
		got, err := gen.RandomHex(l)
		require.NoError(t, err)
		assert.Len(t, got, l, "RandomHex(%d) should return %d chars", l, l)

		// Must be hex characters only.
		for _, c := range got {
			assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
				"RandomHex result contains non-hex char: %q", c)
		}
	}

	// Two consecutive calls should not produce identical output.
	a, _ := gen.RandomHex(64)
	b, _ := gen.RandomHex(64)
	assert.NotEqual(t, a, b, "RandomHex should return different values on successive calls")
}

func TestSecretGenerator_RandomAlphanumeric(t *testing.T) {
	gen := domain.NewSecretGenerator()

	lengths := []int{32, 64}
	for _, l := range lengths {
		got, err := gen.RandomAlphanumeric(l)
		require.NoError(t, err)
		assert.Len(t, got, l, "RandomAlphanumeric(%d) should return %d chars", l, l)

		// Must be alphanumeric only (no special chars).
		for _, c := range got {
			assert.True(t, unicode.IsLetter(c) || unicode.IsDigit(c),
				"RandomAlphanumeric result contains non-alphanumeric char: %q", c)
		}
	}

	// Two consecutive calls should not produce identical output.
	a, _ := gen.RandomAlphanumeric(64)
	b, _ := gen.RandomAlphanumeric(64)
	assert.NotEqual(t, a, b, "RandomAlphanumeric should return different values on successive calls")
}

func TestSecretGenerator_GenerateJWT_ValidFormat(t *testing.T) {
	gen := domain.NewSecretGenerator()
	secret, err := gen.RandomHex(64)
	require.NoError(t, err)

	cases := []struct {
		role   string
		expiry int
	}{
		{"anon", 3600},
		{"service_role", 3600},
	}
	for _, c := range cases {
		token, err := gen.GenerateJWT(secret, c.role, c.expiry)
		require.NoError(t, err)

		// JWT must be 3 base64url-encoded segments separated by dots.
		parts := strings.Split(token, ".")
		require.Len(t, parts, 3, "JWT must have 3 parts: header.payload.signature")

		// Each part must be valid base64url.
		for i, part := range parts {
			// Pad to multiple of 4 for standard base64 decode.
			padded := part + strings.Repeat("=", (4-len(part)%4)%4)
			_, err := base64.URLEncoding.DecodeString(padded)
			assert.NoError(t, err, "JWT part %d is not valid base64url", i)
		}
	}
}

// ─── GenerateProjectSecrets ──────────────────────────────────────────────────

func TestGenerateProjectSecrets_ReturnsAll12Keys(t *testing.T) {
	gen := domain.NewSecretGenerator()
	secrets, err := domain.GenerateProjectSecrets(gen)
	require.NoError(t, err)

	wantKeys := []string{
		"JWT_SECRET",
		"ANON_KEY",
		"SERVICE_ROLE_KEY",
		"POSTGRES_PASSWORD",
		"DASHBOARD_PASSWORD",
		"SECRET_KEY_BASE",
		"VAULT_ENC_KEY",
		"PG_META_CRYPTO_KEY",
		"LOGFLARE_PUBLIC_ACCESS_TOKEN",
		"LOGFLARE_PRIVATE_ACCESS_TOKEN",
		"S3_PROTOCOL_ACCESS_KEY_ID",
		"S3_PROTOCOL_ACCESS_KEY_SECRET",
	}
	assert.Len(t, secrets, len(wantKeys))
	for _, k := range wantKeys {
		val, ok := secrets[k]
		assert.True(t, ok, "missing key: %s", k)
		assert.NotEmpty(t, val, "empty value for key: %s", k)
	}
}

func TestGenerateProjectSecrets_AnonKeyIsValidJWT(t *testing.T) {
	gen := domain.NewSecretGenerator()
	secrets, err := domain.GenerateProjectSecrets(gen)
	require.NoError(t, err)

	for _, key := range []string{"ANON_KEY", "SERVICE_ROLE_KEY"} {
		token := secrets[key]
		parts := strings.Split(token, ".")
		assert.Len(t, parts, 3, "%s must be a 3-part JWT", key)
	}
}

func TestGenerateProjectSecrets_JWTSecretIs64HexChars(t *testing.T) {
	gen := domain.NewSecretGenerator()
	secrets, err := domain.GenerateProjectSecrets(gen)
	require.NoError(t, err)

	jwt := secrets["JWT_SECRET"]
	assert.Len(t, jwt, 64, "JWT_SECRET should be 64 hex chars")
	for _, c := range jwt {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"JWT_SECRET contains non-hex char: %q", c)
	}
}

func TestGenerateProjectSecrets_DifferentEachCall(t *testing.T) {
	gen := domain.NewSecretGenerator()
	a, err := domain.GenerateProjectSecrets(gen)
	require.NoError(t, err)
	b, err := domain.GenerateProjectSecrets(gen)
	require.NoError(t, err)

	assert.NotEqual(t, a["JWT_SECRET"], b["JWT_SECRET"], "JWT_SECRET should differ between calls")
	assert.NotEqual(t, a["POSTGRES_PASSWORD"], b["POSTGRES_PASSWORD"], "POSTGRES_PASSWORD should differ between calls")
}
