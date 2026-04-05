package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Welp, unsure if this was really needed :)
func TestStorageError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "ErrInvalid_maps_to_400",
			err:        &storage.StorageError{Code: storage.ErrInvalid, Message: "bad input"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID",
		},
		{
			name:       "ErrNotFound_maps_to_404",
			err:        &storage.StorageError{Code: storage.ErrNotFound, Message: "not found"},
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
		},
		{
			name:       "ErrAlreadyExists_maps_to_409",
			err:        &storage.StorageError{Code: storage.ErrAlreadyExists, Message: "exists"},
			wantStatus: http.StatusConflict,
			wantCode:   "ALREADY_EXISTS",
		},
		{
			name:       "unknown_code_maps_to_500",
			err:        &storage.StorageError{Code: "CUSTOM", Message: "custom error"},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "CUSTOM",
		},
		{
			name:       "ErrInternal_maps_to_500",
			err:        &storage.StorageError{Code: storage.ErrInternal, Message: "nfs export failed"},
			wantStatus: http.StatusInternalServerError,
			wantCode:   storage.ErrInternal,
		},
		{
			name:       "non_StorageError_maps_to_500",
			err:        fmt.Errorf("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   storage.ErrInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := StorageError(c, tt.err)
			require.NoError(t, err)

			assert.Equal(t, tt.wantStatus, rec.Code)

			var resp models.ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, tt.wantCode, resp.Code)
		})
	}
}

func TestStorageError_FallbackSanitized(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := StorageError(c, fmt.Errorf("exportfs: /data/vol1: permission denied"))
	require.NoError(t, err)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var resp models.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "internal error", resp.Error, "fallback must not leak error details")
	assert.NotContains(t, resp.Error, "exportfs", "command name must not leak")
	assert.NotContains(t, resp.Error, "permission denied", "system error must not leak")
}
