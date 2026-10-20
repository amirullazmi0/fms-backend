package model

import "slices"

// Permission names. Each is a row in the permissions table (seeded by migration) and
// guards routes through middleware.RequirePermission. Roles are sets of these.
const (
	PermissionUserReadAll    = "user.read_all"
	PermissionUserCreate     = "user.create"
	PermissionUserUpdate     = "user.update"
	PermissionUserUpdateRole = "user.update_role"
	PermissionUserDelete     = "user.delete"

	PermissionRoleRead   = "role.read"
	PermissionRoleCreate = "role.create"
	PermissionRoleUpdate = "role.update"
	PermissionRoleDelete = "role.delete"

	PermissionDeviceRead    = "device.read"
	PermissionDeviceUpdate  = "device.update"
	PermissionDeviceDelete  = "device.delete"
	PermissionDeviceCommand = "device.command"
	PermissionLogRead       = "log.read"

	PermissionSiteRead   = "site.read"
	PermissionSiteCreate = "site.create"
	PermissionSiteUpdate = "site.update"
	PermissionSiteDelete = "site.delete"

	PermissionRouteRead   = "route.read"
	PermissionRouteCreate = "route.create"
	PermissionRouteUpdate = "route.update"
	PermissionRouteDelete = "route.delete"

	PermissionDriverRead   = "driver.read"
	PermissionDriverCreate = "driver.create"
	PermissionDriverUpdate = "driver.update"
	PermissionDriverDelete = "driver.delete"

	PermissionShiftTemplateRead   = "shift_template.read"
	PermissionShiftTemplateCreate = "shift_template.create"
	PermissionShiftTemplateUpdate = "shift_template.update"
	PermissionShiftTemplateDelete = "shift_template.delete"

	PermissionShiftRead    = "shift.read"
	PermissionShiftCreate  = "shift.create"
	PermissionShiftUpdate  = "shift.update"
	PermissionShiftDelete  = "shift.delete"
	PermissionShiftOperate = "shift.operate"

	PermissionAssignmentManage = "assignment.manage"

	PermissionReportRead = "report.read"

	PermissionFeatureUpdate = "feature.update"

	PermissionRoadRead      = "road.read"
	PermissionRoadDelete    = "road.delete"
	PermissionSimulationRun = "simulation.run"

	PermissionTargetManage    = "target.manage"
	PermissionDelayCodeManage = "delay_code.manage"
	PermissionMdtManage       = "mdt.manage"
	PermissionUnitStatusWrite = "unit_status.write"
	PermissionDispatchManage  = "dispatch.manage"
)

// Feature keys (rows of the features table).
const (
	FeatureSimulation = "simulation"
)

// Built-in role names (seeded by migration). Roles are editable; these are only the
// names the seeder and create-user CLI look up.
const (
	RoleAdmin      = "ADMIN"
	RoleSupervisor = "SUPERVISOR"
	RoleViewer     = "VIEWER"
	RoleOwner      = "OWNER" // fms-administrator, read-only reports
)

type Permission struct {
	ID          string
	Name        string
	Description *string
}

// Actor is the authenticated user acting on a request.
type Actor struct {
	ID   string
	Role string
	// IsAdministrator: superuser, passes every permission check.
	IsAdministrator bool
	// AdminApp: may open fms-administrator (superuser or a role flagged for it).
	AdminApp    bool
	Permissions []string
}

func (a *Actor) HasPermission(name string) bool {
	return a.IsAdministrator || slices.Contains(a.Permissions, name)
}
