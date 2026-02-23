package v1

import (
	"net/http"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"

	"github.com/labstack/echo/v5"
)

var codeStatus = map[string]int{
	storage.ErrInvalid:       http.StatusBadRequest,
	storage.ErrNotFound:      http.StatusNotFound,
	storage.ErrAlreadyExists: http.StatusConflict,
}

func StorageError(c *echo.Context, err error) error {
	if se, ok := err.(*storage.StorageError); ok {
		status, found := codeStatus[se.Code]
		if !found {
			status = http.StatusInternalServerError
		}
		return c.JSON(status, ErrorResponse{Error: se.Message, Code: se.Code})
	}
	return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error(), Code: "INTERNAL_ERROR"})
}
