package v1

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// LoggerMiddleware places a per-request copy of the global logger into the
// request context so subsequent middleware can mutate it via UpdateContext
// (adding tenant, identity, fingerprint after auth) and downstream code can
// pick it up with log.Ctx(ctx). Without this, UpdateContext would mutate the
// global logger.
func LoggerMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			l := log.Logger.With().Logger()
			c.SetRequest(req.WithContext(l.WithContext(req.Context())))
			return next(c)
		}
	}
}

// Context keys set by AuthMiddleware. Read via the *Of accessors below.
const (
	ctxKeyTenant      = "tenant"
	ctxKeyRole        = "role"
	ctxKeyIdentity    = "identity"
	ctxKeyFingerprint = "token_fingerprint"
	ctxKeyDenial      = "denial_reason"
)

func tenantOf(c *echo.Context) string { v, _ := c.Get(ctxKeyTenant).(string); return v }
func roleOf(c *echo.Context) models.TenantRole {
	v, _ := c.Get(ctxKeyRole).(models.TenantRole)
	return v
}
func identityOf(c *echo.Context) string    { v, _ := c.Get(ctxKeyIdentity).(string); return v }
func fingerprintOf(c *echo.Context) string { v, _ := c.Get(ctxKeyFingerprint).(string); return v }

func AuthMiddleware(tokens *TokenSet) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			authHdr := c.Request().Header.Get("Authorization")
			if authHdr == "" {
				return authFailed(c, "missing authorization header")
			}

			scheme, token, ok := strings.Cut(authHdr, " ")
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

			info, fp, ok := tokens.Verify(providedToken)
			if !ok {
				c.Set(ctxKeyDenial, denialInvalidToken)
				return authFailed(c, "invalid token")
			}
			c.Set(ctxKeyTenant, info.Name)
			c.Set(ctxKeyRole, info.Role)
			if info.Identity != "" {
				c.Set(ctxKeyIdentity, info.Identity)
			}
			if fp != "" {
				c.Set(ctxKeyFingerprint, fp)
			}

			log.Ctx(c.Request().Context()).UpdateContext(func(zc zerolog.Context) zerolog.Context {
				zc = zc.Str("tenant", info.Name).Str("role", string(info.Role))
				if info.Identity != "" {
					zc = zc.Str("identity", info.Identity)
				}
				if fp != "" {
					zc = zc.Str("token_fingerprint", fp)
				}
				return zc
			})

			method := c.Request().Method
			if info.Role == models.RoleReadonly {
				if method != http.MethodGet && method != http.MethodHead {
					c.Set(ctxKeyDenial, denialRoleDenied)
					return StorageError(c, &storage.StorageError{Code: storage.ErrForbidden, Message: "readonly role cannot perform " + method})
				}
			}
			if info.Role == models.RoleMounter {
				switch {
				case method == http.MethodGet, method == http.MethodHead:
					// allow, no route lookup needed
				case (method == http.MethodPost || method == http.MethodDelete) && c.RouteInfo().Path == "/v1/volumes/:name/export":
					// allow
				default:
					c.Set(ctxKeyDenial, denialRoleDenied)
					return StorageError(c, &storage.StorageError{Code: storage.ErrForbidden, Message: "mounter role: only GET/HEAD and POST/DELETE /volumes/:name/export"})
				}
			}

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
