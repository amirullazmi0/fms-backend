package dto

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type UserResponse struct {
	ID              string   `json:"id"`
	Email           string   `json:"email"`
	Name            *string  `json:"name"`
	RoleID          string   `json:"role_id"`
	Role            string   `json:"role"`
	IsAdministrator bool     `json:"is_administrator"` // superuser: passes every permission check
	AdminApp        bool     `json:"admin_app"`        // may open fms-administrator
	Permissions     []string `json:"permissions,omitempty"`
	IsActive        bool     `json:"is_active"`
	LastLoginAt     *int64   `json:"last_login_at"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

type AuthResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int64        `json:"expires_in"` // seconds
	User         UserResponse `json:"user"`
}

// CreateUserRequest: role_id empty = the default role.
type CreateUserRequest struct {
	Email    string  `json:"email" validate:"required,email,max=255"`
	Password string  `json:"password" validate:"required,min=8,max=128"`
	Name     *string `json:"name" validate:"omitempty,max=60"`
	RoleID   string  `json:"role_id" validate:"omitempty,uuid"`
	// Only an administrator may grant this.
	IsAdministrator *bool `json:"is_administrator"`
}

type UpdateUserRequest struct {
	Name     *string `json:"name" validate:"omitempty,max=60"`
	Password *string `json:"password" validate:"omitempty,min=8,max=128"`
	IsActive *bool   `json:"is_active"`
	// Only an administrator may change this.
	IsAdministrator *bool `json:"is_administrator"`
}

type UpdateUserRoleRequest struct {
	RoleID string `json:"role_id" validate:"required,uuid"`
}
