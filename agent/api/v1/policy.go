package v1

import (
	"slices"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/labstack/echo/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	rolesUserAdmin        = []models.TenantRole{models.RoleUser, models.RoleAdmin}
	rolesMounterUserAdmin = []models.TenantRole{models.RoleMounter, models.RoleUser, models.RoleAdmin}
)

// adminOnlyTaskTypes require RoleAdmin because they operate filesystem-wide
// rather than tenant-scoped.
var adminOnlyTaskTypes = map[string]struct{}{
	models.TaskTypeScrub:       {},
	models.TaskTypeBalance:     {},
	models.TaskTypeQuotaRescan: {},
}

func isAdminOnlyTaskType(taskType string) bool {
	_, ok := adminOnlyTaskTypes[taskType]
	return ok
}

const (
	denialInvalidToken     = "invalid_token"
	denialRoleDenied       = "role_denied"
	denialOwnership        = "ownership"
	denialIdentityMismatch = "identity_mismatch"
)

// Policy is the per-handler authorization declaration. Field semantics and
// the full per-endpoint matrix are documented in docs/rbac.md.
type Policy struct {
	AllowedRoles      []models.TenantRole
	EnforceCreatedBy  bool
	PreserveCreatedBy bool
	EnforceOwnership  bool
	MounterBypass     bool
}

// Apply enforces the policy on a request. ownerLabels is the resource being
// checked for ownership (nil if there's nothing to own yet, e.g. a fresh
// create). labelsPtr is the labels being mutated by this request (nil if
// nothing is being created or updated, e.g. a delete or a list).
func (p Policy) Apply(c *echo.Context, ownerLabels map[string]string, labelsPtr *map[string]string) error {
	role := roleOf(c)
	if err := p.checkAllowedRoles(c, role); err != nil {
		return err
	}
	if p.EnforceCreatedBy {
		if err := enforceCreatedBy(c, labelsPtr); err != nil {
			return err
		}
	}
	if p.PreserveCreatedBy && labelsPtr != nil {
		if err := preserveCreatedBy(c, ownerLabels, labelsPtr); err != nil {
			return err
		}
	}
	if !p.EnforceOwnership {
		return nil
	}
	// Anonymous mounter bypasses ownership; an identified mounter is
	// scoped to its own resources via the normal check below.
	if p.MounterBypass && role == models.RoleMounter && identityOf(c) == "" {
		return nil
	}
	return checkOwnership(c, role, ownerLabels)
}

func (p Policy) checkAllowedRoles(c *echo.Context, role models.TenantRole) error {
	if len(p.AllowedRoles) == 0 || slices.Contains(p.AllowedRoles, role) {
		return nil
	}
	// No dedicated log line: the access log emitted by MetricsMiddleware
	// already carries reason=role_denied plus all caller fields and is
	// strictly a superset of what we'd write here.
	c.Set(ctxKeyDenial, denialRoleDenied)
	return &storage.StorageError{
		Code:    storage.ErrForbidden,
		Message: "role \"" + string(role) + "\" not allowed for this operation",
	}
}

func denialLog(c *echo.Context, reason string) *zerolog.Event {
	c.Set(ctxKeyDenial, reason)
	return log.Ctx(c.Request().Context()).Warn().
		Str("client", c.RealIP()).
		Str("path", c.Request().URL.Path)
}

func rejectHardReservedLabels(labels map[string]string) error {
	for _, k := range config.HardReservedLabelKeys {
		if _, set := labels[k]; set {
			return &storage.StorageError{
				Code:    storage.ErrInvalid,
				Message: "label \"" + k + "\" cannot be set directly; it is server-managed",
			}
		}
	}
	return nil
}

func enforceCreatedBy(c *echo.Context, labelsPtr *map[string]string) error {
	if *labelsPtr == nil {
		*labelsPtr = map[string]string{}
	}
	labels := *labelsPtr
	if err := rejectHardReservedLabels(labels); err != nil {
		return err
	}
	identity := identityOf(c)
	if identity == "" {
		return nil
	}
	if existing, ok := labels[config.LabelCreatedBy]; ok && existing != identity {
		denialLog(c, denialIdentityMismatch).
			Str("supplied", existing).
			Str("identity", identity).
			Msg("identity mismatch on create")
		return &storage.StorageError{
			Code:    storage.ErrForbidden,
			Message: "label \"" + config.LabelCreatedBy + "\" must match configured identity \"" + identity + "\"",
		}
	}
	labels[config.LabelCreatedBy] = identity
	return nil
}

// preserveCreatedBy guards against a labels-replace clearing or rewriting
// created-by: admins bypass ownership but still cannot edit audit trail.
func preserveCreatedBy(c *echo.Context, ownerLabels map[string]string, labelsPtr *map[string]string) error {
	if *labelsPtr == nil {
		*labelsPtr = map[string]string{}
	}
	labels := *labelsPtr
	if err := rejectHardReservedLabels(labels); err != nil {
		return err
	}
	existing := ownerLabels[config.LabelCreatedBy]
	supplied, clientSet := labels[config.LabelCreatedBy]
	if clientSet && supplied != existing {
		denialLog(c, denialIdentityMismatch).
			Str("supplied", supplied).
			Str("existing", existing).
			Msg("created-by change rejected on update")
		return &storage.StorageError{
			Code:    storage.ErrForbidden,
			Message: "label \"" + config.LabelCreatedBy + "\" cannot be changed via update",
		}
	}
	if existing != "" {
		labels[config.LabelCreatedBy] = existing
	}
	return nil
}

// checkOwnership lets tokens without an identity through (Level 2), and
// lets resources with no created-by through as a pre-enforcement migration
// path. Everyone else (including admins) must match.
func checkOwnership(c *echo.Context, role models.TenantRole, labels map[string]string) error {
	identity := identityOf(c)
	if identity == "" {
		return nil
	}
	owner := labels[config.LabelCreatedBy]
	if owner == "" {
		return nil
	}
	if owner != identity {
		denialLog(c, denialOwnership).
			Str("method", c.Request().Method).
			Str("owner", owner).
			Str("identity", identity).
			Msg("ownership denied")
		return &storage.StorageError{
			Code:    storage.ErrForbidden,
			Message: "resource owned by \"" + owner + "\" is not accessible to identity \"" + identity + "\"",
		}
	}
	return nil
}
