package model

import "time"

type User struct {
	ID           string
	Email        string
	Name         *string
	PasswordHash string
	RoleID       string
	Role         string // role name (joined)
	// IsAdministrator (users.is_administrator) is a superuser: every permission check passes.
	IsAdministrator bool
	// RoleAdminApp (roles.is_administrator, joined): the role may open fms-administrator.
	RoleAdminApp bool
	IsActive     bool
	RefreshToken *string
	TokenExpiry  *time.Time
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

type Role struct {
	ID        string
	Name      string
	IsDefault bool
	// IsAdministrator lets the role into the fms-administrator app.
	IsAdministrator bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

func (r *Role) IsDeleted() bool {
	return r.DeletedAt != nil
}
