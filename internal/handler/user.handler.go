package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type UserHandler struct {
	userUsecase usecase.UserUsecase
}

func NewUserHandler(userUsecase usecase.UserUsecase) *UserHandler {
	return &UserHandler{userUsecase: userUsecase}
}

// Login godoc
// @Summary Login
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dto.LoginRequest true "Login Request"
// @Success 200 {object} response.Response{data=dto.AuthResponse}
// @Router /api/auth/login [post]
func (h *UserHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.userUsecase.Login(c.Request.Context(), &req, c.ClientIP())
	if err != nil {
		respondError(c, err, "Login gagal")
		return
	}
	response.SetAuthCookies(c, result.AccessToken, result.RefreshToken, result.ExpiresIn)
	response.Success(c, http.StatusOK, "Login successful", result)
}

// RefreshToken godoc
// @Summary Rotate the access + refresh token pair
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dto.RefreshTokenRequest true "Refresh Token Request"
// @Success 200 {object} response.Response{data=dto.AuthResponse}
// @Router /api/auth/refresh [post]
func (h *UserHandler) RefreshToken(c *gin.Context) {
	var req dto.RefreshTokenRequest
	// The refresh token may also come from the httpOnly cookie.
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		if t, err := c.Cookie("refresh_token"); err == nil {
			req.RefreshToken = t
		}
	}
	if req.RefreshToken == "" {
		response.Error(c, http.StatusBadRequest, "refresh_token is required", nil)
		return
	}
	result, err := h.userUsecase.RefreshToken(c.Request.Context(), &req)
	if err != nil {
		respondError(c, err, "Refresh token gagal")
		return
	}
	response.SetAuthCookies(c, result.AccessToken, result.RefreshToken, result.ExpiresIn)
	response.Success(c, http.StatusOK, "Token refreshed successfully", result)
}

// Logout godoc
// @Summary Logout and revoke the refresh token
// @Tags auth
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/auth/logout [post]
//
// Logout always succeeds and runs on optional auth, so an expired access token
// still gets the session torn down.
func (h *UserHandler) Logout(c *gin.Context) {
	if userID := c.GetString("user_id"); userID != "" {
		if err := h.userUsecase.Logout(c.Request.Context(), userID); err != nil {
			logger.Warn("failed to clear refresh token on logout", zap.String("user_id", userID), zap.Error(err))
		}
	}
	response.ClearAuthCookies(c)
	response.Success(c, http.StatusOK, "Logged out successfully", nil)
}

// GetProfile godoc
// @Summary Current user
// @Tags users
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.UserResponse}
// @Router /api/users/profile [get]
func (h *UserHandler) GetProfile(c *gin.Context) {
	result, err := h.userUsecase.GetProfile(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to get profile")
		return
	}
	response.Success(c, http.StatusOK, "Profile retrieved successfully", result)
}

// GetUsers godoc
// @Summary List users (admin)
// @Tags users
// @Security BearerAuth
// @Router /api/users [get]
func (h *UserHandler) GetUsers(c *gin.Context) {
	q, ok := listQuery(c)
	if !ok {
		return
	}
	users, total, err := h.userUsecase.GetUsers(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get users")
		return
	}
	response.SuccessWithMeta(c, http.StatusOK, "Users retrieved successfully", users, listMeta(q, total))
}

// CreateUser godoc
// @Summary Create a user (admin)
// @Tags users
// @Security BearerAuth
// @Router /api/users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest
	if !bindJSON(c, &req) {
		return
	}
	id, err := h.userUsecase.CreateUser(c.Request.Context(), &req, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to create user")
		return
	}
	response.Success(c, http.StatusCreated, "User created successfully", gin.H{"id": id})
}

// UpdateUser godoc
// @Summary Update a user (admin)
// @Tags users
// @Security BearerAuth
// @Router /api/users/{id} [put]
func (h *UserHandler) UpdateUser(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var req dto.UpdateUserRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := h.userUsecase.UpdateUser(c.Request.Context(), id, &req, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to update user")
		return
	}
	response.Success(c, http.StatusOK, "User updated successfully", gin.H{"id": id})
}

// UpdateUserRole godoc
// @Summary Move a user to another role (needs user.update_role)
// @Tags users
// @Security BearerAuth
// @Router /api/users/{id}/role [put]
func (h *UserHandler) UpdateUserRole(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var req dto.UpdateUserRoleRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := h.userUsecase.UpdateUserRole(c.Request.Context(), id, &req, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to update user role")
		return
	}
	response.Success(c, http.StatusOK, "User role updated successfully", gin.H{"id": id, "role_id": req.RoleID})
}

// DeleteUser godoc
// @Summary Delete a user (admin, soft delete)
// @Tags users
// @Security BearerAuth
// @Router /api/users/{id} [delete]
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	if err := h.userUsecase.DeleteUser(c.Request.Context(), id, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to delete user")
		return
	}
	response.Success(c, http.StatusOK, "User deleted successfully", gin.H{"id": id})
}
