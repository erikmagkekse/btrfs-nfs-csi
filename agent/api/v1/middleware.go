package v1

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

// AuthMiddleware validates Bearer or Basic auth and resolves the token to a tenant name.
func AuthMiddleware(tenants map[string]string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			auth := c.Request().Header.Get("Authorization")
			if auth == "" {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="agent"`)
				return c.JSON(http.StatusUnauthorized, ErrorResponse{
					Error: "missing authorization header",
					Code:  "UNAUTHORIZED",
				})
			}

			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 {
				return unauthorized(c)
			}

			var providedToken string
			switch parts[0] {
			case "Bearer":
				providedToken = parts[1]
			case "Basic":
				decoded, err := base64.StdEncoding.DecodeString(parts[1])
				if err != nil {
					return unauthorized(c)
				}
				_, pass, ok := strings.Cut(string(decoded), ":")
				if !ok {
					return unauthorized(c)
				}
				providedToken = pass
			default:
				return unauthorized(c)
			}

			tenant, ok := tenants[providedToken]
			if !ok {
				return unauthorized(c)
			}
			c.Set("tenant", tenant)

			return next(c)
		}
	}
}

func unauthorized(c *echo.Context) error {
	c.Response().Header().Set("WWW-Authenticate", `Basic realm="agent"`)
	return c.JSON(http.StatusUnauthorized, ErrorResponse{
		Error: "invalid auth token",
		Code:  "UNAUTHORIZED",
	})
}
