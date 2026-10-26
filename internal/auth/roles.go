package auth

// Role is a user's role. Vendor roles use the admin panel; customer and
// reseller roles use their portals.
type Role string

const (
	RoleOwner     Role = "owner"
	RoleAdmin     Role = "admin"
	RoleSupport   Role = "support"
	RolePublisher Role = "publisher"
	RoleViewer    Role = "viewer"
	RoleCustomer  Role = "customer"
	RoleReseller  Role = "reseller"
)

// Perm is a single permission checked by handlers.
type Perm string

const (
	PermCustomersRead      Perm = "customers.read"
	PermCustomersWrite     Perm = "customers.write"
	PermLicensesRead       Perm = "licenses.read"
	PermLicensesWrite      Perm = "licenses.write"
	PermLicensesRevoke     Perm = "licenses.revoke"
	PermActivationOffline  Perm = "activation.offline"
	PermInstallationsRead  Perm = "installations.read"
	PermInstallationsWrite Perm = "installations.write"
	PermResellersWrite     Perm = "resellers.write"
	PermProductsWrite      Perm = "products.write"
	PermRepoPublish        Perm = "repo.publish"
	PermKeysManage         Perm = "keys.manage"
	PermUsersManage        Perm = "users.manage"
	PermAuditRead          Perm = "audit.read"
)

var readOnly = []Perm{PermCustomersRead, PermLicensesRead, PermInstallationsRead}

var rolePerms = map[Role][]Perm{
	RoleOwner: {
		PermCustomersRead, PermCustomersWrite, PermLicensesRead, PermLicensesWrite, PermLicensesRevoke,
		PermActivationOffline, PermInstallationsRead, PermInstallationsWrite, PermResellersWrite,
		PermProductsWrite, PermRepoPublish, PermKeysManage, PermUsersManage, PermAuditRead,
	},
	RoleAdmin: {
		PermCustomersRead, PermCustomersWrite, PermLicensesRead, PermLicensesWrite, PermLicensesRevoke,
		PermActivationOffline, PermInstallationsRead, PermInstallationsWrite, PermResellersWrite,
		PermProductsWrite, PermAuditRead,
	},
	RoleSupport:   append([]Perm{PermActivationOffline, PermInstallationsWrite}, readOnly...),
	RolePublisher: append([]Perm{PermRepoPublish}, readOnly...),
	RoleViewer:    readOnly,
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleSupport, RolePublisher, RoleViewer, RoleCustomer, RoleReseller:
		return true
	}
	return false
}

// Vendor reports whether the role belongs to the vendor's own staff.
func (r Role) Vendor() bool { _, ok := rolePerms[r]; return ok }

// Can reports whether the role grants p.
func (r Role) Can(p Perm) bool {
	for _, have := range rolePerms[r] {
		if have == p {
			return true
		}
	}
	return false
}
