package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCreateTaskReq(t *testing.T, taskType, body string) (*echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+taskType, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPathValues(echo.PathValues{{Name: "type", Value: taskType}})
	return c, rec
}

func TestCreateTask_ReadonlyRejected(t *testing.T) {
	h := &Handler{}
	c, rec := newCreateTaskReq(t, models.TaskTypeTest, `{"labels":{"created-by":"cli"}}`)
	c.Set("tenant", "dashboard")
	c.Set("role", models.RoleReadonly)

	require.NoError(t, h.CreateTask(c))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	var resp models.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, storage.ErrForbidden, resp.Code)
}

func TestCreateTask_AdminOnlyRejectsUser(t *testing.T) {
	h := &Handler{}

	for _, taskType := range []string{models.TaskTypeScrub, models.TaskTypeBalance, models.TaskTypeQuotaRescan} {
		c, rec := newCreateTaskReq(t, taskType, `{"labels":{"created-by":"cli"}}`)
		c.Set("tenant", "default")
		c.Set("role", models.RoleUser)

		require.NoError(t, h.CreateTask(c))
		assert.Equal(t, http.StatusForbidden, rec.Code, "task=%s", taskType)

		var resp models.ErrorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, storage.ErrForbidden, resp.Code)
	}
}

func TestCreateTask_RequiresCreatedByLabel(t *testing.T) {
	// unknown task type triggers validation AFTER label enforcement, letting us
	// observe the created-by requirement without needing a real Store.
	h := &Handler{}
	c, rec := newCreateTaskReq(t, "test", `{"labels":{}}`)
	c.Set("tenant", "default")
	c.Set("role", models.RoleAdmin)

	require.NoError(t, h.CreateTask(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp models.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, storage.ErrInvalid, resp.Code)
	assert.Contains(t, resp.Error, "created-by")
}

func TestCreateTask_IdentitySatisfiesCreatedByRequirement(t *testing.T) {
	// With a configured identity, the server injects created-by and the
	// label check passes. We route to an unknown task type so the handler
	// short-circuits with 400 "unknown task type" without needing a Store.
	h := &Handler{}
	c, rec := newCreateTaskReq(t, "nonexistent", `{"labels":{}}`)
	c.Set("tenant", "ci")
	c.Set("role", models.RoleUser)
	c.Set("identity", "ci-bot")

	require.NoError(t, h.CreateTask(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp models.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp.Error, "unknown task type")
}

func TestCreateTask_IdentityMismatchRejected(t *testing.T) {
	// Client sends a created-by that conflicts with the configured identity.
	// Server must reject instead of silently overwriting.
	h := &Handler{}
	c, rec := newCreateTaskReq(t, models.TaskTypeTest, `{"labels":{"created-by":"attacker"}}`)
	c.Set("tenant", "ci")
	c.Set("role", models.RoleUser)
	c.Set("identity", "ci-bot")

	require.NoError(t, h.CreateTask(c))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	var resp models.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, storage.ErrForbidden, resp.Code)
	assert.Contains(t, resp.Error, "ci-bot")
}

func TestCreateTask_IdentityMatchingAllowed(t *testing.T) {
	// Client echoing the configured identity is idempotent, not an error.
	// Route to an unknown task type so we short-circuit without a Store.
	h := &Handler{}
	c, rec := newCreateTaskReq(t, "nonexistent", `{"labels":{"created-by":"ci-bot"}}`)
	c.Set("tenant", "ci")
	c.Set("role", models.RoleUser)
	c.Set("identity", "ci-bot")

	require.NoError(t, h.CreateTask(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown task type")
}

func TestCreateTask_AdminBypassRoleCheck(t *testing.T) {
	// Admin is not blocked by the role gate; unknown task type lets us assert
	// the handler progressed past the admin-only check without needing a Store.
	h := &Handler{}
	c, rec := newCreateTaskReq(t, "unknown-type", `{"labels":{"created-by":"cli"}}`)
	c.Set("tenant", "ops")
	c.Set("role", models.RoleAdmin)

	require.NoError(t, h.CreateTask(c))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown task type")
}
