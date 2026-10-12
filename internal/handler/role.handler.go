package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/middleware"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type RoleHandler struct {
	roleUsecase usecase.RoleUsecase
}

func NewRoleHandler(roleUsecase usecase.RoleUsecase) *RoleHandler {
	return &RoleHandler{roleUsecase: roleUsecase}
}

// GetAllPermissions godoc
// @Summary Permission catalogue (fixed by the route guards; rendered as checkboxes)
// @Tags role
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.PermissionResponse}
// @Router /api/permissions [get]
func (h *RoleHandler) GetAllPermissions(c *gin.Context) {
	list, err := h.roleUsecase.GetAllPermissions(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get permissions")
		return
	}
	response.Success(c, http.StatusOK, "Permissions retrieved successfully", list)
}

// GetAllRoles godoc
// @Summary List roles with permissions, user_count and in_use
// @Tags role
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.RoleListResponse}
// @Router /api/roles [get]
func (h *RoleHandler) GetAllRoles(c *gin.Context) {
	q, ok := listQuery(c)
	if !ok {
		return
	}
	list, total, err := h.roleUsecase.GetAll(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get roles")
		return
	}
	response.SuccessWithMeta(c, http.StatusOK, "Roles retrieved successfully", list, listMeta(q, total))
}

// GetRoleByID godoc
// @Summary One role
// @Tags role
// @Security BearerAuth
// @Router /api/roles/{id} [get]
func (h *RoleHandler) GetRoleByID(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	role, err := h.roleUsecase.GetByID(c.Request.Context(), id)
	if err != nil {
		respondError(c, err, "Failed to get role")
		return
	}
	response.Success(c, http.StatusOK, "Role retrieved successfully", role)
}

// CreateRole godoc
// @Summary Create a role with its permissions
// @Tags role
// @Security BearerAuth
// @Param request body dto.CreateRoleRequest true "Role"
// @Router /api/roles [post]
func (h *RoleHandler) CreateRole(c *gin.Context) {
	actor, _ := middleware.ActorFrom(c)
	var req dto.CreateRoleRequest
	if !bindJSON(c, &req) {
		return
	}
	role, err := h.roleUsecase.Create(c.Request.Context(), actor, &req)
	if err != nil {
		respondError(c, err, "Failed to create role")
		return
	}
	response.Success(c, http.StatusCreated, "Role created successfully", role)
}

// UpdateRole godoc
// @Summary Rename a role, replace its permissions, set default / administrator
// @Tags role
// @Security BearerAuth
// @Param request body dto.UpdateRoleRequest true "Role"
// @Router /api/roles/{id} [put]
func (h *RoleHandler) UpdateRole(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	actor, _ := middleware.ActorFrom(c)
	var req dto.UpdateRoleRequest
	if !bindJSON(c, &req) {
		return
	}
	role, err := h.roleUsecase.Update(c.Request.Context(), actor, id, &req)
	if err != nil {
		respondError(c, err, "Failed to update role")
		return
	}
	response.Success(c, http.StatusOK, "Role updated successfully", role)
}

// DeleteRole godoc
// @Summary Soft-delete a role (not the default one, not while users have it)
// @Tags role
// @Security BearerAuth
// @Router /api/roles/{id} [delete]
func (h *RoleHandler) DeleteRole(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	actor, _ := middleware.ActorFrom(c)
	if err := h.roleUsecase.Delete(c.Request.Context(), actor, id); err != nil {
		respondError(c, err, "Failed to delete role")
		return
	}
	response.Success(c, http.StatusOK, "Role deleted successfully", gin.H{"id": id})
}
