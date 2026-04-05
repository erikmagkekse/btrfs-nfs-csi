package swagger

import (
	"net/http"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
	"github.com/swaggo/swag"
)

// ServeSwaggerJSON returns a handler that serves the OpenAPI spec as JSON.
// The spec is parsed once at startup. Enable with AGENT_API_SWAGGER_ENABLED=true.
func ServeSwaggerJSON() echo.HandlerFunc {
	spec, err := swag.ReadDoc()
	if err != nil {
		return func(c *echo.Context) error {
			return c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "swagger spec not available", Code: storage.ErrInternal})
		}
	}
	blob := []byte(spec)

	return func(c *echo.Context) error {
		return c.JSONBlob(http.StatusOK, blob)
	}
}
