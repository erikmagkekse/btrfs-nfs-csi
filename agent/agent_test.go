package agent

import (
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTenants_EmptyReturnsNil(t *testing.T) {
	m, err := parseTenants("")
	require.NoError(t, err)
	assert.Nil(t, m)
}

func TestParseTenants_DefaultRoleIsAdmin(t *testing.T) {
	m, err := parseTenants("default:tok1")
	require.NoError(t, err)
	require.Len(t, m, 1)
	assert.Equal(t, models.TenantInfo{Name: "default", Role: models.RoleAdmin}, m["tok1"])
}

func TestParseTenants_ExplicitRoles(t *testing.T) {
	m, err := parseTenants("default:tok1:admin,ci:tok2:user,dash:tok3:readonly")
	require.NoError(t, err)
	require.Len(t, m, 3)
	assert.Equal(t, models.RoleAdmin, m["tok1"].Role)
	assert.Equal(t, models.RoleUser, m["tok2"].Role)
	assert.Equal(t, models.RoleReadonly, m["tok3"].Role)
}

func TestParseTenants_IdentityPerRole(t *testing.T) {
	// user, mounter, admin accept identity
	m, err := parseTenants("ci:tok1:user:ci-bot,nodes:tok2:mounter:node_1,ops:tok3:admin:ansible-ops")
	require.NoError(t, err)
	assert.Equal(t, "ci-bot", m["tok1"].Identity)
	assert.Equal(t, "node_1", m["tok2"].Identity)
	assert.Equal(t, "ansible-ops", m["tok3"].Identity)

	// readonly cannot have identity
	_, err = parseTenants("dash:tok1:readonly:some-label")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identity does not apply to role")

	// empty identity is invalid regardless of role
	_, err = parseTenants("ci:tok1:user:")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid identity")

	// dots are not allowed in identity
	_, err = parseTenants("ci:tok1:user:ci.bot")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid identity")
}

func TestParseTenants_MultipleTokensSameTenant(t *testing.T) {
	m, err := parseTenants("default:old,default:new")
	require.NoError(t, err)
	require.Len(t, m, 2)
	assert.Equal(t, "default", m["old"].Name)
	assert.Equal(t, "default", m["new"].Name)
	assert.Equal(t, models.RoleAdmin, m["old"].Role)
	assert.Equal(t, models.RoleAdmin, m["new"].Role)
}

func TestParseTenants_MixedRolesAllowed(t *testing.T) {
	m, err := parseTenants("default:tok1:user:ci-bot,default:tok2:admin,default:tok3:mounter:node-1")
	require.NoError(t, err)
	require.Len(t, m, 3)
	assert.Equal(t, models.RoleUser, m["tok1"].Role)
	assert.Equal(t, models.RoleAdmin, m["tok2"].Role)
	assert.Equal(t, models.RoleMounter, m["tok3"].Role)
}

func TestParseTenants_DuplicateToken(t *testing.T) {
	_, err := parseTenants("default:sametok,other:sametok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate token")
	assert.Contains(t, err.Error(), "default")
	assert.Contains(t, err.Error(), "other")
}

func TestParseTenants_UnknownRole(t *testing.T) {
	_, err := parseTenants("default:tok1:superuser")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown role")
}

func TestParseTenants_ReservedName(t *testing.T) {
	_, err := parseTenants("tasks:tok1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reserved")
}

func TestParseTenants_InvalidStructure(t *testing.T) {
	_, err := parseTenants("just-a-name")
	require.Error(t, err)

	_, err = parseTenants("a:b:c:d:e")
	require.Error(t, err)
}

func TestParseTenants_EmptyToken(t *testing.T) {
	_, err := parseTenants("default:")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty token")
}
