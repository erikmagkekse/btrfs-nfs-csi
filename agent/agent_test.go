package agent

import (
	"testing"

	v1 "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findCred returns the first credential whose Stored value matches tok,
// or nil if none. parseTenants returns a slice (not a map) because hashed
// tokens cannot be keyed by stored value at the call site.
func findCred(creds []v1.TokenCredential, stored string) *v1.TokenCredential {
	for i := range creds {
		if creds[i].Stored == stored {
			return &creds[i]
		}
	}
	return nil
}

func TestParseTenants_EmptyReturnsNil(t *testing.T) {
	creds, err := parseTenants("")
	require.NoError(t, err)
	assert.Nil(t, creds)
}

func TestParseTenants_DefaultRoleIsAdmin(t *testing.T) {
	creds, err := parseTenants("default:tok1")
	require.NoError(t, err)
	require.Len(t, creds, 1)
	assert.Equal(t, models.TenantInfo{Name: "default", Role: models.RoleAdmin}, creds[0].Info)
	assert.Equal(t, "tok1", creds[0].Stored)
}

func TestParseTenants_ExplicitRoles(t *testing.T) {
	creds, err := parseTenants("default:tok1:admin,ci:tok2:user,dash:tok3:readonly")
	require.NoError(t, err)
	require.Len(t, creds, 3)
	assert.Equal(t, models.RoleAdmin, findCred(creds, "tok1").Info.Role)
	assert.Equal(t, models.RoleUser, findCred(creds, "tok2").Info.Role)
	assert.Equal(t, models.RoleReadonly, findCred(creds, "tok3").Info.Role)
}

func TestParseTenants_IdentityPerRole(t *testing.T) {
	// user, mounter, admin accept identity
	creds, err := parseTenants("ci:tok1:user:ci-bot,nodes:tok2:mounter:node_1,ops:tok3:admin:ansible-ops")
	require.NoError(t, err)
	assert.Equal(t, "ci-bot", findCred(creds, "tok1").Info.Identity)
	assert.Equal(t, "node_1", findCred(creds, "tok2").Info.Identity)
	assert.Equal(t, "ansible-ops", findCred(creds, "tok3").Info.Identity)

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
	creds, err := parseTenants("default:old,default:new")
	require.NoError(t, err)
	require.Len(t, creds, 2)
	assert.Equal(t, "default", findCred(creds, "old").Info.Name)
	assert.Equal(t, "default", findCred(creds, "new").Info.Name)
	assert.Equal(t, models.RoleAdmin, findCred(creds, "old").Info.Role)
	assert.Equal(t, models.RoleAdmin, findCred(creds, "new").Info.Role)
}

func TestParseTenants_MixedRolesAllowed(t *testing.T) {
	creds, err := parseTenants("default:tok1:user:ci-bot,default:tok2:admin,default:tok3:mounter:node-1")
	require.NoError(t, err)
	require.Len(t, creds, 3)
	assert.Equal(t, models.RoleUser, findCred(creds, "tok1").Info.Role)
	assert.Equal(t, models.RoleAdmin, findCred(creds, "tok2").Info.Role)
	assert.Equal(t, models.RoleMounter, findCred(creds, "tok3").Info.Role)
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

func TestParseTenants_BcryptHashAccepted(t *testing.T) {
	// Pre-generated $2y$04$ hash of "secret"; cost 4 keeps the test fast.
	const bcryptOfSecret = "$2y$04$BqUeqpe8Sp1LkiMVsqdzs.Rd5Eg29TJj6e3Wvt9.iZ8eqwjN12iKy"
	creds, err := parseTenants("ops:" + bcryptOfSecret + ":admin")
	require.NoError(t, err)
	require.Len(t, creds, 1)
	assert.Equal(t, "ops", creds[0].Info.Name)
	assert.Equal(t, models.RoleAdmin, creds[0].Info.Role)
	assert.Equal(t, bcryptOfSecret, creds[0].Stored)
}
