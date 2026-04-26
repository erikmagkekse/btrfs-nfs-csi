package v1

import (
	"cmp"
	"net/http"
	"slices"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/labstack/echo/v5"
)

// policyListTokens: admin only. Exposing the token roster (even as
// fingerprints) is privileged operator data.
var policyListTokens = Policy{AllowedRoles: []models.TenantRole{models.RoleAdmin}}

// Whoami godoc
// @Summary      Return the caller's tenant, role, and identity
// @Description  Echoes the authenticated token's tenant context. Useful for clients to verify what they authenticated as.
// @Tags         auth
// @Produce      json
// @Success      200 {object} models.WhoamiResponse
// @Router       /v1/whoami [get]
// @Security     BearerAuth
func (h *Handler) Whoami(c *echo.Context) error {
	return c.JSON(http.StatusOK, models.WhoamiResponse{
		Tenant:      tenantOf(c),
		Role:        roleOf(c),
		Identity:    identityOf(c),
		Fingerprint: fingerprintOf(c),
	})
}

// ListTokens godoc
// @Summary      List tokens configured for the caller's tenant
// @Description  Admin-only. Returns every token within the caller's tenant with its role, identity, and a fingerprint. Tokens themselves are never returned. Cross-tenant visibility is not exposed.
// @Tags         auth
// @Produce      json
// @Success      200 {object} models.TenantResponse
// @Failure      403 {object} models.ErrorResponse "Admin role required"
// @Router       /v1/tokens [get]
// @Security     BearerAuth
func (h *Handler) ListTokens(c *echo.Context) error {
	if err := policyListTokens.Apply(c, nil, nil); err != nil {
		return StorageError(c, err)
	}

	tenant := tenantOf(c)

	entries := h.Tokens.FilterTenant(tenant)
	tokens := make([]models.TenantTokenResponse, 0, len(entries))
	for _, e := range entries {
		tokens = append(tokens, models.TenantTokenResponse{
			Fingerprint: e.Fingerprint,
			Role:        e.Info.Role,
			Identity:    e.Info.Identity,
		})
	}
	slices.SortFunc(tokens, func(a, b models.TenantTokenResponse) int {
		return cmp.Compare(a.Fingerprint, b.Fingerprint)
	})

	return c.JSON(http.StatusOK, models.TenantResponse{Name: tenant, Tokens: tokens})
}
