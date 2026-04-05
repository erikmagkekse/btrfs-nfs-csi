package v1

import (
	"errors"
	"net/http"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog/log"
)

var codeStatus = map[string]int{
	storage.ErrInvalid:       http.StatusBadRequest,
	storage.ErrNotFound:      http.StatusNotFound,
	storage.ErrAlreadyExists: http.StatusConflict,
	storage.ErrBusy:          http.StatusLocked,
	storage.ErrMetadata:      http.StatusInternalServerError,
	storage.ErrInternal:      http.StatusInternalServerError,
}

func StorageError(c *echo.Context, err error) error {
	if ve, ok := err.(*config.ValidationError); ok {
		return c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: ve.Message, Code: storage.ErrInvalid})
	}
	if se, ok := err.(*storage.StorageError); ok {
		status, found := codeStatus[se.Code]
		if !found {
			status = http.StatusInternalServerError
		}
		return c.JSON(status, models.ErrorResponse{Error: se.Message, Code: se.Code})
	}
	if errors.Is(err, task.ErrNotFound) {
		return c.JSON(http.StatusNotFound, models.ErrorResponse{Error: err.Error(), Code: storage.ErrNotFound})
	}
	log.Error().Err(err).Msg("unhandled storage error")
	return c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "internal error", Code: storage.ErrInternal})
}
