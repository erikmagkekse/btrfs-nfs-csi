package v1

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog/log"
)

// AuthMiddleware validates Bearer or Basic auth and resolves the token to a tenant name.
func AuthMiddleware(tenants map[string]string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			auth := c.Request().Header.Get("Authorization")
			if auth == "" {
				return authFailed(c, "missing authorization header")
			}

			scheme, token, ok := strings.Cut(auth, " ")
			if !ok {
				return authFailed(c, "malformed authorization header")
			}

			var providedToken string
			switch scheme {
			case "Bearer":
				providedToken = token
			case "Basic":
				decoded, err := base64.StdEncoding.DecodeString(token)
				if err != nil {
					return authFailed(c, "invalid basic auth encoding")
				}
				_, pass, ok := strings.Cut(string(decoded), ":")
				if !ok {
					return authFailed(c, "invalid basic auth format")
				}
				providedToken = pass
			default:
				return authFailed(c, "unsupported auth scheme: "+scheme)
			}

			tenant, ok := tenants[providedToken]
			if !ok {
				return authFailed(c, "invalid token")
			}
			c.Set("tenant", tenant)

			return next(c)
		}
	}
}

func authFailed(c *echo.Context, reason string) error {
	log.Warn().Str("client", c.RealIP()).Str("path", c.Request().URL.Path).Str("reason", reason).Msg("auth failed")
	log.Trace().Str("client", c.RealIP()).Str("authorization", c.Request().Header.Get("Authorization")).Msg("auth failed detail")
	c.Response().Header().Set("WWW-Authenticate", `Basic realm="agent"`)
	return c.JSON(http.StatusUnauthorized, models.ErrorResponse{
		Error: "invalid auth token",
		Code:  storage.ErrUnauthorized,
	})
}
