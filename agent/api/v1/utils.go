package v1

import (
	"errors"
	"net/http"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/task"

	"github.com/labstack/echo/v5"
)

var codeStatus = map[string]int{
	storage.ErrInvalid:       http.StatusBadRequest,
	storage.ErrNotFound:      http.StatusNotFound,
	storage.ErrAlreadyExists: http.StatusConflict,
	storage.ErrBusy:          http.StatusLocked,
	storage.ErrMetadata:      http.StatusInternalServerError,
}

func StorageError(c *echo.Context, err error) error {
	if se, ok := err.(*storage.StorageError); ok {
		status, found := codeStatus[se.Code]
		if !found {
			status = http.StatusInternalServerError
		}
		return c.JSON(status, ErrorResponse{Error: se.Message, Code: se.Code})
	}
	if errors.Is(err, task.ErrNotFound) {
		return c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error(), Code: storage.ErrNotFound})
	}
	return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error(), Code: "INTERNAL_ERROR"})
}
