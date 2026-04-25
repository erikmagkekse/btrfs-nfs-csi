package v1

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// testFingerprint is a deterministic fingerprint function for tests; it
// does not need a real installation secret.
func testFingerprint(s string) string {
	h := hmac.New(sha256.New, []byte("test-fingerprint-key"))
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func mustBcrypt(t *testing.T, secret string) string {
	t.Helper()
	// MinCost keeps tests fast; production uses htpasswd defaults.
	h, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

// tokensFromMap is a test helper that builds a TokenSet from the legacy
// map[token]TenantInfo shape so the existing middleware tests keep their
// terse setup without rewriting every literal.
func tokensFromMap(t *testing.T, m map[string]models.TenantInfo) *TokenSet {
	t.Helper()
	if m == nil {
		return nil
	}
	creds := make([]TokenCredential, 0, len(m))
	for tok, info := range m {
		creds = append(creds, TokenCredential{Stored: tok, Info: info})
	}
	ts, err := NewTokenSet(creds, testFingerprint)
	require.NoError(t, err)
	return ts
}

func TestNewTokenSet_RejectsDuplicateStored(t *testing.T) {
	creds := []TokenCredential{
		{Stored: "tok", Info: models.TenantInfo{Name: "a", Role: models.RoleAdmin}},
		{Stored: "tok", Info: models.TenantInfo{Name: "b", Role: models.RoleUser}},
	}
	_, err := NewTokenSet(creds, testFingerprint)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestNewTokenSet_RejectsEmptyStored(t *testing.T) {
	_, err := NewTokenSet([]TokenCredential{{Stored: "", Info: models.TenantInfo{Name: "a"}}}, testFingerprint)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestNewTokenSet_RejectsArgon2(t *testing.T) {
	stored := "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA"
	_, err := NewTokenSet([]TokenCredential{{Stored: stored, Info: models.TenantInfo{Name: "a"}}}, testFingerprint)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "argon2")
}

func TestNewTokenSet_RejectsUnknownScheme(t *testing.T) {
	_, err := NewTokenSet([]TokenCredential{{Stored: "$apr1$xyz$abc", Info: models.TenantInfo{Name: "a"}}}, testFingerprint)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestNewTokenSet_RejectsMalformedBcrypt(t *testing.T) {
	_, err := NewTokenSet([]TokenCredential{{Stored: "$2y$not-a-real-hash", Info: models.TenantInfo{Name: "a"}}}, testFingerprint)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bcrypt")
}

func TestVerify_PlaintextHit(t *testing.T) {
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: "plain-tok", Info: models.TenantInfo{Name: "ci", Role: models.RoleUser, Identity: "ci-bot"}},
	}, testFingerprint)
	require.NoError(t, err)

	info, fp, ok := ts.Verify("plain-tok")
	require.True(t, ok)
	assert.Equal(t, "ci", info.Name)
	assert.Equal(t, models.RoleUser, info.Role)
	assert.Equal(t, "ci-bot", info.Identity)
	assert.NotEmpty(t, fp)
}

func TestVerify_PlaintextMiss(t *testing.T) {
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: "plain-tok", Info: models.TenantInfo{Name: "ci", Role: models.RoleUser}},
	}, testFingerprint)
	require.NoError(t, err)

	_, _, ok := ts.Verify("wrong-tok")
	assert.False(t, ok)
}

func TestVerify_Empty(t *testing.T) {
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: "tok", Info: models.TenantInfo{Name: "a", Role: models.RoleAdmin}},
	}, testFingerprint)
	require.NoError(t, err)

	_, _, ok := ts.Verify("")
	assert.False(t, ok)
}

func TestVerify_Bcrypt(t *testing.T) {
	hashed := mustBcrypt(t, "my-secret")
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: hashed, Info: models.TenantInfo{Name: "ops", Role: models.RoleAdmin}},
	}, testFingerprint)
	require.NoError(t, err)

	info, fp, ok := ts.Verify("my-secret")
	require.True(t, ok, "correct password should verify")
	assert.Equal(t, "ops", info.Name)
	assert.NotEmpty(t, fp)

	_, _, ok = ts.Verify("wrong-secret")
	assert.False(t, ok)
}

func TestVerify_BcryptCacheShortCircuits(t *testing.T) {
	hashed := mustBcrypt(t, "cached")
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: hashed, Info: models.TenantInfo{Name: "ops", Role: models.RoleAdmin}},
	}, testFingerprint)
	require.NoError(t, err)

	// First verify populates the cache via the slow bcrypt path.
	_, fp1, ok := ts.Verify("cached")
	require.True(t, ok)

	// Force the bcrypt entry to a sentinel so any second slow-path verify
	// would fail. If the cache works, Verify returns from the cache and
	// ignores the entry contents.
	ts.entries[0].bcryptHash = []byte("$2y$04$xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")

	_, fp2, ok := ts.Verify("cached")
	require.True(t, ok, "cache miss path would re-hash and fail")
	assert.Equal(t, fp1, fp2)
}

func TestVerify_MixedPlaintextAndBcrypt(t *testing.T) {
	hashed := mustBcrypt(t, "hashed-secret")
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: "plain-tok", Info: models.TenantInfo{Name: "alpha", Role: models.RoleUser}},
		{Stored: hashed, Info: models.TenantInfo{Name: "bravo", Role: models.RoleAdmin}},
	}, testFingerprint)
	require.NoError(t, err)

	info, _, ok := ts.Verify("plain-tok")
	require.True(t, ok)
	assert.Equal(t, "alpha", info.Name)

	info, _, ok = ts.Verify("hashed-secret")
	require.True(t, ok)
	assert.Equal(t, "bravo", info.Name)

	_, _, ok = ts.Verify("nope")
	assert.False(t, ok)
}

func TestVerify_NilTokenSet(t *testing.T) {
	var ts *TokenSet
	_, _, ok := ts.Verify("anything")
	assert.False(t, ok)
}

func TestVerify_NilFingerprintFunc_StillWorks(t *testing.T) {
	// Tests must be able to construct a TokenSet without a real secret.
	// Cache is disabled and fingerprints are empty, but matching still works.
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: "tok", Info: models.TenantInfo{Name: "a", Role: models.RoleAdmin}},
	}, nil)
	require.NoError(t, err)

	info, fp, ok := ts.Verify("tok")
	require.True(t, ok)
	assert.Equal(t, "a", info.Name)
	assert.Empty(t, fp)
}

func TestEntries_HasFingerprintsAndInfo(t *testing.T) {
	hashed := mustBcrypt(t, "x")
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: "p1", Info: models.TenantInfo{Name: "a", Role: models.RoleUser, Identity: "id-a"}},
		{Stored: hashed, Info: models.TenantInfo{Name: "b", Role: models.RoleAdmin}},
	}, testFingerprint)
	require.NoError(t, err)

	entries := ts.Entries()
	require.Len(t, entries, 2)
	for _, e := range entries {
		assert.NotEmpty(t, e.Fingerprint, "fingerprint should be set when fp is configured")
	}
	assert.Equal(t, "a", entries[0].Info.Name)
	assert.Equal(t, "b", entries[1].Info.Name)
}

func TestFilterTenant(t *testing.T) {
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: "t1", Info: models.TenantInfo{Name: "a", Role: models.RoleAdmin}},
		{Stored: "t2", Info: models.TenantInfo{Name: "a", Role: models.RoleUser, Identity: "id"}},
		{Stored: "t3", Info: models.TenantInfo{Name: "b", Role: models.RoleAdmin}},
	}, testFingerprint)
	require.NoError(t, err)

	got := ts.FilterTenant("a")
	require.Len(t, got, 2)
	for _, v := range got {
		assert.Equal(t, "a", v.Info.Name)
	}

	got = ts.FilterTenant("nonexistent")
	assert.Empty(t, got)
}
