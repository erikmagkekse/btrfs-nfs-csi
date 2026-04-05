package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_InvalidToken_NoTokenInWarnLog(t *testing.T) {
	// capture log output at warn level (trace is intentionally allowed to contain tokens)
	var buf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(&buf).With().Timestamp().Logger().Level(zerolog.WarnLevel)
	defer func() { log.Logger = orig }()

	tenants := map[string]string{"valid-token": "default"}
	mw := AuthMiddleware(tenants)

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

	// verify the Authorization header / token value is NOT in warn-level log output
	logOutput := buf.String()
	assert.NotContains(t, logOutput, "secret-token-value", "auth token must not appear in warn logs")
	assert.NotContains(t, logOutput, "Bearer secret-token-value", "Authorization header must not appear in warn logs")
	// verify we still log useful context
	assert.Contains(t, logOutput, "auth failed", "should still log the auth failure event")
}

func TestAuthMiddleware_InvalidToken_TraceContainsToken(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Logger
	log.Logger = zerolog.New(&buf).With().Timestamp().Logger().Level(zerolog.TraceLevel)
	defer func() { log.Logger = orig }()

	tenants := map[string]string{"valid-token": "default"}
	mw := AuthMiddleware(tenants)

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

	logOutput := buf.String()
	assert.Contains(t, logOutput, "secret-token-value", "trace log should contain token for debugging")
	assert.Contains(t, logOutput, "auth failed detail", "trace log should have detail message")
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	tenants := map[string]string{"good-token": "mytenant"}
	mw := AuthMiddleware(tenants)

	e := echo.New()
	var gotTenant string
	handler := mw(func(c *echo.Context) error {
		gotTenant = c.Get("tenant").(string)
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
}
