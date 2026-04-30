package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func init() {
	zerolog.DefaultContextLogger = &log.Logger
}

func TestAuthMiddleware_InvalidToken_NoTokenInLog(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(&buf).With().Timestamp().Logger().Level(zerolog.TraceLevel)
	defer func() { log.Logger = orig }()

	tenants := map[string]models.TenantInfo{"valid-token": {Name: "default", Role: models.RoleAdmin}}
	mw := AuthMiddleware(tokensFromMap(t, tenants))

	e := echo.New()
	handler := mw(func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/volumes", nil)
	req.Header.Set("Authorization", "Bearer secret-token-value")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var resp models.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, storage.ErrUnauthorized, resp.Code)

	logOutput := buf.String()
	assert.NotContains(t, logOutput, "secret-token-value", "auth token must not appear in any log level")
	assert.NotContains(t, logOutput, "Bearer secret-token-value", "Authorization header must not appear in any log level")
	assert.Contains(t, logOutput, "auth failed", "should still log the auth failure event")
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	tenants := map[string]models.TenantInfo{"good-token": {Name: "mytenant", Role: models.RoleAdmin}}
	mw := AuthMiddleware(tokensFromMap(t, tenants))

	e := echo.New()
	var gotTenant string
	var gotRole models.TenantRole
	handler := mw(func(c *echo.Context) error {
		gotTenant = c.Get("tenant").(string)
		gotRole = c.Get("role").(models.TenantRole)
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/volumes", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler(c)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "mytenant", gotTenant)
	assert.Equal(t, models.RoleAdmin, gotRole)
}

func TestAuthMiddleware_ReadonlyRejectsWrites(t *testing.T) {
	tenants := map[string]models.TenantInfo{"ro-token": {Name: "dashboard", Role: models.RoleReadonly}}
	mw := AuthMiddleware(tokensFromMap(t, tenants))

	cases := []struct {
		method     string
		wantStatus int
	}{
		{http.MethodGet, http.StatusOK},
		{http.MethodHead, http.StatusOK},
		{http.MethodPost, http.StatusForbidden},
		{http.MethodPatch, http.StatusForbidden},
		{http.MethodDelete, http.StatusForbidden},
		{http.MethodPut, http.StatusForbidden},
	}

	for _, tc := range cases {
		e := echo.New()
		handler := mw(func(c *echo.Context) error {
			return c.NoContent(http.StatusOK)
		})
		req := httptest.NewRequest(tc.method, "/v1/volumes", nil)
		req.Header.Set("Authorization", "Bearer ro-token")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		require.NoError(t, handler(c))
		assert.Equal(t, tc.wantStatus, rec.Code, "method=%s", tc.method)
		if tc.wantStatus == http.StatusForbidden {
			var resp models.ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, storage.ErrForbidden, resp.Code)
		}
	}
}

func TestAuthMiddleware_MounterRestricts(t *testing.T) {
	tenants := map[string]models.TenantInfo{"m-token": {Name: "nodes", Role: models.RoleMounter}}

	cases := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/v1/volumes", http.StatusOK},
		{http.MethodGet, "/v1/volumes/a", http.StatusOK},
		{http.MethodHead, "/v1/volumes/a", http.StatusOK},
		{http.MethodPost, "/v1/volumes/a/export", http.StatusOK},
		{http.MethodDelete, "/v1/volumes/a/export", http.StatusOK},
		{http.MethodPost, "/v1/volumes", http.StatusForbidden},
		{http.MethodPatch, "/v1/volumes/a", http.StatusForbidden},
		{http.MethodDelete, "/v1/volumes/a", http.StatusForbidden},
		{http.MethodPost, "/v1/snapshots", http.StatusForbidden},
		{http.MethodPost, "/v1/tasks/test", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			e := echo.New()
			api := e.Group("/v1", AuthMiddleware(tokensFromMap(t, tenants)))
			noop := func(c *echo.Context) error { return c.NoContent(http.StatusOK) }
			api.GET("/volumes", noop)
			api.GET("/volumes/:name", noop)
			api.HEAD("/volumes/:name", noop)
			api.POST("/volumes/:name/export", noop)
			api.DELETE("/volumes/:name/export", noop)
			api.POST("/volumes", noop)
			api.PATCH("/volumes/:name", noop)
			api.DELETE("/volumes/:name", noop)
			api.POST("/snapshots", noop)
			api.POST("/tasks/:type", noop)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer m-token")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, tc.want, rec.Code)
		})
	}
}

func TestAuthMiddleware_IdentitySetInContext(t *testing.T) {
	tenants := map[string]models.TenantInfo{
		"user-token":  {Name: "ci", Role: models.RoleUser, Identity: "ci-bot"},
		"admin-token": {Name: "ops", Role: models.RoleAdmin},
	}
	mw := AuthMiddleware(tokensFromMap(t, tenants))

	cases := []struct {
		token      string
		wantLabel  string
		wantHasVal bool
	}{
		{"user-token", "ci-bot", true},
		{"admin-token", "", false},
	}

	for _, tc := range cases {
		e := echo.New()
		var gotLabel any
		handler := mw(func(c *echo.Context) error {
			gotLabel = c.Get("identity")
			return c.NoContent(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodGet, "/volumes", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		require.NoError(t, handler(c))
		assert.Equal(t, http.StatusOK, rec.Code)
		if tc.wantHasVal {
			assert.Equal(t, tc.wantLabel, gotLabel)
		} else {
			assert.Nil(t, gotLabel)
		}
	}
}

func TestAuthMiddleware_ValidToken_MultipleTenants(t *testing.T) {
	tenants := map[string]models.TenantInfo{
		"token-a": {Name: "alpha", Role: models.RoleAdmin},
		"token-b": {Name: "bravo", Role: models.RoleUser},
		"token-c": {Name: "charlie", Role: models.RoleAdmin},
	}
	mw := AuthMiddleware(tokensFromMap(t, tenants))

	cases := []struct {
		token      string
		wantTenant string
		wantRole   models.TenantRole
	}{
		{"token-a", "alpha", models.RoleAdmin},
		{"token-b", "bravo", models.RoleUser},
		{"token-c", "charlie", models.RoleAdmin},
	}

	for _, tc := range cases {
		e := echo.New()
		var gotTenant string
		var gotRole models.TenantRole
		handler := mw(func(c *echo.Context) error {
			gotTenant = c.Get("tenant").(string)
			gotRole = c.Get("role").(models.TenantRole)
			return c.NoContent(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/volumes", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		require.NoError(t, handler(c))
		assert.Equal(t, http.StatusOK, rec.Code, "token %q", tc.token)
		assert.Equal(t, tc.wantTenant, gotTenant, "token %q", tc.token)
		assert.Equal(t, tc.wantRole, gotRole, "token %q", tc.token)
	}
}

func TestAuthMiddleware_BcryptToken_AcceptedAndCachedAcrossRequests(t *testing.T) {
	hashed, err := bcrypt.GenerateFromPassword([]byte("plain-secret"), bcrypt.MinCost)
	require.NoError(t, err)
	ts, err := NewTokenSet([]TokenCredential{
		{Stored: string(hashed), Info: models.TenantInfo{Name: "ops", Role: models.RoleAdmin, Identity: "ansible"}},
	}, testFingerprint)
	require.NoError(t, err)

	mw := AuthMiddleware(ts)

	send := func(token string) (int, *echo.Context) {
		e := echo.New()
		var ctxAfter *echo.Context
		handler := mw(func(c *echo.Context) error {
			ctxAfter = c
			return c.NoContent(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodGet, "/v1/volumes", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		require.NoError(t, handler(c))
		return rec.Code, ctxAfter
	}

	code, c := send("plain-secret")
	assert.Equal(t, http.StatusOK, code)
	require.NotNil(t, c)
	assert.Equal(t, "ops", c.Get("tenant"))
	assert.Equal(t, models.RoleAdmin, c.Get("role"))
	assert.Equal(t, "ansible", c.Get("identity"))
	assert.NotEmpty(t, c.Get("token_fingerprint"))

	// Second request with the same plaintext goes through the cache; sentinel
	// the bcrypt hash so a slow-path verify would fail.
	ts.entries[0].bcryptHash = []byte("$2y$04$xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	code, _ = send("plain-secret")
	assert.Equal(t, http.StatusOK, code, "cache should short-circuit slow bcrypt verify")

	// Wrong password still rejected.
	code, _ = send("wrong-secret")
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestAuthMiddleware_EmptyTenants(t *testing.T) {
	mw := AuthMiddleware(nil)

	e := echo.New()
	handler := mw(func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/volumes", nil)
	req.Header.Set("Authorization", "Bearer any-token")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, handler(c))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestVerifyTokenSet_NoMatchWithSimilarLengthToken(t *testing.T) {
	ts := tokensFromMap(t, map[string]models.TenantInfo{
		"token-a": {Name: "alpha", Role: models.RoleAdmin},
		"token-b": {Name: "bravo", Role: models.RoleUser},
	})

	matched, _, ok := ts.Verify("token-x")
	assert.False(t, ok)
	assert.Empty(t, matched.Name)

	matched, _, ok = ts.Verify("")
	assert.False(t, ok)
	assert.Empty(t, matched.Name)

	matched, _, ok = ts.Verify("token-a")
	assert.True(t, ok)
	assert.Equal(t, "alpha", matched.Name)
	assert.Equal(t, models.RoleAdmin, matched.Role)
}

func TestCheckOwnership(t *testing.T) {
	newCtx := func(identity string) *echo.Context {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/v1/volumes/x", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		if identity != "" {
			c.Set("identity", identity)
		}
		return c
	}

	cases := []struct {
		name     string
		identity string
		labels   map[string]string
		wantErr  bool
	}{
		{"no identity allows anything", "", map[string]string{"created-by": "other"}, false},
		{"no created-by allows (migration)", "ci-bot", map[string]string{}, false},
		{"matching created-by allowed", "ci-bot", map[string]string{"created-by": "ci-bot"}, false},
		{"mismatched created-by rejected", "ci-bot", map[string]string{"created-by": "other-bot"}, true},
		{"nil labels allowed (migration)", "ci-bot", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkOwnership(newCtx(tc.identity), models.RoleUser, tc.labels)
			if tc.wantErr {
				require.Error(t, err)
				se, ok := err.(*storage.StorageError)
				require.True(t, ok, "expected StorageError, got %T", err)
				assert.Equal(t, storage.ErrForbidden, se.Code)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestPolicyApply is a compact matrix-driven test that runs every
// per-handler Policy literal through the three enforcement axes
// (role, identity match/mismatch, ownership) and asserts the expected
// outcome. Uses `config` and `log` via sibling code, keeping coverage
// of the Policy.Apply code path in one place.
func TestPolicyApply(t *testing.T) {
	newCtx := func(role models.TenantRole, identity string) *echo.Context {
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/v1/x", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.Set("role", role)
		if identity != "" {
			c.Set("identity", identity)
		}
		c.Set("tenant", "ci")
		return c
	}

	cases := []struct {
		name        string
		policy      Policy
		role        models.TenantRole
		identity    string
		owner       map[string]string // read for EnforceOwnership; if nil, Apply reuses stamp
		stamp       map[string]string // seed for stamp map
		wantErr     bool
		wantStamped string
	}{
		{
			name:        "CreateVolume as user with identity: stamps",
			policy:      policyCreateVolume,
			role:        models.RoleUser,
			identity:    "ci-bot",
			stamp:       nil,
			wantStamped: "ci-bot",
		},
		{
			name:     "CreateVolume as user with mismatched created-by: 403",
			policy:   policyCreateVolume,
			role:     models.RoleUser,
			identity: "ci-bot",
			stamp:    map[string]string{config.LabelCreatedBy: "other"},
			wantErr:  true,
		},
		{
			name:    "CreateVolume as readonly: 403",
			policy:  policyCreateVolume,
			role:    models.RoleReadonly,
			wantErr: true,
		},
		{
			name:     "DeleteVolume as user with matching owner: pass",
			policy:   policyDeleteVolume,
			role:     models.RoleUser,
			identity: "ci-bot",
			stamp:    map[string]string{config.LabelCreatedBy: "ci-bot"},
		},
		{
			name:     "DeleteVolume as user with wrong owner: 403",
			policy:   policyDeleteVolume,
			role:     models.RoleUser,
			identity: "ci-bot",
			stamp:    map[string]string{config.LabelCreatedBy: "other-bot"},
			wantErr:  true,
		},
		{
			name:   "DeleteVolume as admin without identity: pass",
			policy: policyDeleteVolume,
			role:   models.RoleAdmin,
			stamp:  map[string]string{config.LabelCreatedBy: "someone-else"},
		},
		{
			name:     "DeleteVolume as identified admin with foreign owner: 403",
			policy:   policyDeleteVolume,
			role:     models.RoleAdmin,
			identity: "ansible",
			stamp:    map[string]string{config.LabelCreatedBy: "someone-else"},
			wantErr:  true,
		},
		{
			name:   "CreateExport as anonymous mounter bypasses ownership",
			policy: policyCreateExport,
			role:   models.RoleMounter,
			owner:  map[string]string{config.LabelCreatedBy: "ci-bot"}, // volume owned by ci-bot
			stamp:  nil,                                                // export's own labels, no identity to stamp
		},
		{
			name:        "CreateExport as identified mounter with matching owner: pass",
			policy:      policyCreateExport,
			role:        models.RoleMounter,
			identity:    "ci-bot",
			owner:       map[string]string{config.LabelCreatedBy: "ci-bot"},
			stamp:       nil,
			wantStamped: "ci-bot",
		},
		{
			name:     "CreateExport as identified mounter with foreign owner: 403",
			policy:   policyCreateExport,
			role:     models.RoleMounter,
			identity: "node-1",
			owner:    map[string]string{config.LabelCreatedBy: "ci-bot"},
			stamp:    nil,
			wantErr:  true,
		},
		{
			name:     "CreateExport as user still ownership-gated",
			policy:   policyCreateExport,
			role:     models.RoleUser,
			identity: "other-bot",
			owner:    map[string]string{config.LabelCreatedBy: "ci-bot"},
			stamp:    nil,
			wantErr:  true,
		},
		{
			name:     "CreateSnapshot of foreign volume rejected",
			policy:   policyCreateSnapshot,
			role:     models.RoleUser,
			identity: "ci-bot",
			owner:    map[string]string{config.LabelCreatedBy: "other-bot"},
			stamp:    nil,
			wantErr:  true,
		},
		{
			name:        "CreateSnapshot of own volume stamps",
			policy:      policyCreateSnapshot,
			role:        models.RoleUser,
			identity:    "ci-bot",
			owner:       map[string]string{config.LabelCreatedBy: "ci-bot"},
			stamp:       nil,
			wantStamped: "ci-bot",
		},
		{
			name:   "CreateSnapshot as admin without identity: pass",
			policy: policyCreateSnapshot,
			role:   models.RoleAdmin,
			owner:  map[string]string{config.LabelCreatedBy: "someone-else"},
			stamp:  nil,
		},
		{
			name:     "CreateSnapshot as identified admin with foreign source: 403",
			policy:   policyCreateSnapshot,
			role:     models.RoleAdmin,
			identity: "ansible",
			owner:    map[string]string{config.LabelCreatedBy: "someone-else"},
			stamp:    nil,
			wantErr:  true,
		},
		{
			name:     "CloneVolume of foreign source rejected",
			policy:   policyCloneVolume,
			role:     models.RoleUser,
			identity: "ci-bot",
			owner:    map[string]string{config.LabelCreatedBy: "other-bot"},
			stamp:    nil,
			wantErr:  true,
		},
		{
			name:     "CreateClone of foreign snapshot rejected",
			policy:   policyCreateClone,
			role:     models.RoleUser,
			identity: "ci-bot",
			owner:    map[string]string{config.LabelCreatedBy: "other-bot"},
			stamp:    nil,
			wantErr:  true,
		},
		{
			name:        "UpdateVolume omits created-by: preserved",
			policy:      policyUpdateVolume,
			role:        models.RoleUser,
			identity:    "ci-bot",
			owner:       map[string]string{config.LabelCreatedBy: "ci-bot"},
			stamp:       map[string]string{"foo": "bar"},
			wantStamped: "ci-bot",
		},
		{
			name:     "UpdateVolume rewrites created-by: 403",
			policy:   policyUpdateVolume,
			role:     models.RoleUser,
			identity: "ci-bot",
			owner:    map[string]string{config.LabelCreatedBy: "ci-bot"},
			stamp:    map[string]string{config.LabelCreatedBy: "attacker"},
			wantErr:  true,
		},
		{
			name:     "UpdateVolume with client-supplied tenant label: 400",
			policy:   policyUpdateVolume,
			role:     models.RoleUser,
			identity: "ci-bot",
			owner:    map[string]string{config.LabelCreatedBy: "ci-bot"},
			stamp:    map[string]string{config.LabelTenant: "other"},
			wantErr:  true,
		},
		{
			name:     "UpdateVolume with client-supplied clone.source.name: 400 even with same value",
			policy:   policyUpdateVolume,
			role:     models.RoleUser,
			identity: "ci-bot",
			owner:    map[string]string{config.LabelCreatedBy: "ci-bot", config.LabelCloneSourceName: "snap-1"},
			stamp:    map[string]string{config.LabelCloneSourceName: "snap-1"},
			wantErr:  true,
		},
		{
			name:    "UpdateVolume as admin rewrites created-by: still 403",
			policy:  policyUpdateVolume,
			role:    models.RoleAdmin,
			owner:   map[string]string{config.LabelCreatedBy: "ci-bot"},
			stamp:   map[string]string{config.LabelCreatedBy: "other"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newCtx(tc.role, tc.identity)
			stamp := tc.stamp
			owner := tc.owner
			if owner == nil {
				owner = stamp
			}
			err := tc.policy.Apply(c, owner, &stamp)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.wantStamped != "" {
				assert.Equal(t, tc.wantStamped, stamp[config.LabelCreatedBy])
			}
		})
	}
}

// TestMetricsMiddleware_AccessLogIncludesAuthFields verifies the per-request
// access log emitted by MetricsMiddleware carries tenant/role/identity/
// token_fingerprint after AuthMiddleware ran UpdateContext on the per-request
// logger placed by LoggerMiddleware. Pinning this protects the audit story.
func TestMetricsMiddleware_AccessLogIncludesAuthFields(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(&buf).With().Timestamp().Logger()
	defer func() { log.Logger = orig }()

	tenants := map[string]models.TenantInfo{
		"user-token": {Name: "ci", Role: models.RoleUser, Identity: "ci-bot"},
	}

	e := echo.New()
	e.Use(LoggerMiddleware())
	e.Use(MetricsMiddleware())
	api := e.Group("/v1", AuthMiddleware(tokensFromMap(t, tenants)))
	api.GET("/whoami", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	out := buf.String()
	assert.Contains(t, out, `"message":"request"`, "expected access log line")
	assert.Contains(t, out, `"tenant":"ci"`)
	assert.Contains(t, out, `"role":"user"`)
	assert.Contains(t, out, `"identity":"ci-bot"`)
	assert.Contains(t, out, `"token_fingerprint":`)
	assert.Contains(t, out, `"code":200`)
	assert.Contains(t, out, `"method":"GET"`)
	assert.Contains(t, out, `"path":"/v1/whoami"`)
	assert.NotContains(t, out, `"reason":`, "no denial reason on success")
}

// TestMetricsMiddleware_HealthzNotLogged verifies that GET /healthz produces
// no access log line. The agent's CSI node-driver pollt that endpoint every
// few seconds and would flood the log otherwise.
func TestMetricsMiddleware_HealthzNotLogged(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(&buf).With().Timestamp().Logger()
	defer func() { log.Logger = orig }()

	e := echo.New()
	e.Use(LoggerMiddleware())
	e.Use(MetricsMiddleware())
	e.GET("/healthz", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/v1/whatever", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	e.ServeHTTP(healthRec, healthReq)
	require.Equal(t, http.StatusOK, healthRec.Code)
	assert.NotContains(t, buf.String(), `"message":"request"`, "/healthz must not emit access log")

	buf.Reset()

	otherReq := httptest.NewRequest(http.MethodGet, "/v1/whatever", nil)
	otherRec := httptest.NewRecorder()
	e.ServeHTTP(otherRec, otherReq)
	require.Equal(t, http.StatusOK, otherRec.Code)
	otherOut := buf.String()
	assert.Contains(t, otherOut, `"message":"request"`, "non-/healthz must emit access log")
	assert.Contains(t, otherOut, `"path":"/v1/whatever"`)
}

// TestStorageEventLogInheritsAuthFields verifies that downstream code (the
// storage layer) calling log.Ctx(ctx) inside a handler picks up the
// tenant/role/identity/token_fingerprint that AuthMiddleware stamped onto
// the per-request logger via UpdateContext. This is what makes "volume
// created" event lines carry the same audit fields as the access log.
func TestStorageEventLogInheritsAuthFields(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(&buf).With().Timestamp().Logger()
	defer func() { log.Logger = orig }()

	tenants := map[string]models.TenantInfo{
		"user-token": {Name: "ops", Role: models.RoleUser, Identity: "ci-bot"},
	}

	e := echo.New()
	e.Use(LoggerMiddleware())
	api := e.Group("/v1", AuthMiddleware(tokensFromMap(t, tenants)))
	api.POST("/volumes", func(c *echo.Context) error {
		// Mirror what agent/storage/volume.go does after CreateVolume.
		log.Ctx(c.Request().Context()).Info().
			Str("name", "my-vol").
			Str("path", "/data/ops/my-vol").
			Msg("volume created")
		return c.NoContent(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/volumes", nil)
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	out := buf.String()
	assert.Contains(t, out, `"message":"volume created"`)
	assert.Contains(t, out, `"name":"my-vol"`)
	assert.Contains(t, out, `"tenant":"ops"`)
	assert.Contains(t, out, `"role":"user"`)
	assert.Contains(t, out, `"identity":"ci-bot"`)
	assert.Contains(t, out, `"token_fingerprint":`)
}

// TestMetricsMiddleware_NotFoundLogsRealStatusAndUserAgent verifies that a
// request to an unregistered path logs at warn level with code=404 (not 200)
// and that the User-Agent header lands in the access log. echo's default
// error handler writes the 404 after middleware unwinds, so reading
// Response.Status directly would still show 200, the middleware therefore
// uses echo.ResolveResponseStatus to resolve the err.
func TestMetricsMiddleware_NotFoundLogsRealStatusAndUserAgent(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(&buf).With().Timestamp().Logger()
	defer func() { log.Logger = orig }()

	e := echo.New()
	e.Use(LoggerMiddleware())
	e.Use(MetricsMiddleware())
	e.GET("/known", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/totally-fake", nil)
	req.Header.Set("User-Agent", "ZGrab/0.x (scanner)")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)

	out := buf.String()
	assert.Contains(t, out, `"level":"warn"`, "4xx must log at warn level")
	assert.Contains(t, out, `"code":404`)
	assert.Contains(t, out, `"path":"/totally-fake"`)
	assert.Contains(t, out, `"user_agent":"ZGrab/0.x (scanner)"`)
	assert.NotContains(t, out, `"code":200`, "must not report 200 for a 404")
}

// TestPolicyApply_RejectsHardReservedLabels exercises every policy that
// stamps or preserves labels against every server-managed key. Any
// combination must fail at the API boundary rather than silently overwrite
// or pass through.
func TestPolicyApply_RejectsHardReservedLabels(t *testing.T) {
	type policyCase struct {
		name      string
		policy    Policy
		needOwner bool // true for policies with EnforceOwnership (need a non-empty owner map)
	}
	policies := []policyCase{
		{"CreateVolume", policyCreateVolume, false},
		{"CloneVolume", policyCloneVolume, true},
		{"CreateClone", policyCreateClone, true},
		{"CreateSnapshot", policyCreateSnapshot, true},
		{"CreateExport", policyCreateExport, true},
		{"CreateTask", policyCreateTask, false},
		{"CreateTaskOnVolume", policyCreateTaskOnVolume, true},
		{"UpdateVolume", policyUpdateVolume, true},
	}
	forgeKeys := []string{
		config.LabelTenant,
		config.LabelCloneSourceType,
		config.LabelCloneSourceName,
	}

	for _, p := range policies {
		for _, key := range forgeKeys {
			t.Run(p.name+"_forges_"+key, func(t *testing.T) {
				e := echo.New()
				req := httptest.NewRequest(http.MethodPost, "/v1/x", nil)
				rec := httptest.NewRecorder()
				c := e.NewContext(req, rec)
				c.Set("role", models.RoleUser)
				c.Set("identity", "ci-bot")
				c.Set("tenant", "ci")

				stamp := map[string]string{key: "haxx"}
				var owner map[string]string
				if p.needOwner {
					owner = map[string]string{config.LabelCreatedBy: "ci-bot"}
				}
				err := p.policy.Apply(c, owner, &stamp)
				require.Error(t, err, "policy %s must reject client-supplied %s", p.name, key)
			})
		}
	}
}
