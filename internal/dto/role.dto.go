package dto

type CreateRoleRequest struct {
	Name            string   `json:"name" validate:"required,min=3,max=100"`
	Permissions     []string `json:"permissions" validate:"omitempty,dive,required,max=100"`
	IsDefault       *bool    `json:"is_default"`
	IsAdministrator *bool    `json:"is_administrator"`
}

// Permissions is a pointer so an absent field leaves the grants alone while an
// empty array revokes them all.
type UpdateRoleRequest struct {
	Name            string    `json:"name" validate:"omitempty,min=3,max=100"`
	Permissions     *[]string `json:"permissions" validate:"omitempty,dive,required,max=100"`
	IsDefault       *bool     `json:"is_default"`
	IsAdministrator *bool     `json:"is_administrator"`
}

type PermissionResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type RoleResponse struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	IsDefault       bool     `json:"is_default"`
	IsAdministrator bool     `json:"is_administrator"`
	Permissions     []string `json:"permissions"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

type RoleListResponse struct {
	RoleResponse
	UserCount int  `json:"user_count"`
	InUse     bool `json:"in_use"`
}
