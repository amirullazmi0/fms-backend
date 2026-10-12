package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/amirullazmi0/fms-backend/pkg/routing"
	"github.com/gin-gonic/gin"
)

// RoadHandler: stored road data (fms-administrator → Data jalan).
type RoadHandler struct {
	repo repository.RoadRepository
}

func NewRoadHandler(repo repository.RoadRepository) *RoadHandler {
	return &RoadHandler{repo: repo}
}

// GetPaths godoc
// @Summary Stored road paths, newest first (no coords)
// @Tags road
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.RoadPathRow}
// @Router /api/road-data/paths [get]
func (h *RoadHandler) GetPaths(c *gin.Context) {
	q, ok := listQuery(c)
	if !ok {
		return
	}
	list, total, err := h.repo.ListPaths(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get road paths")
		return
	}
	response.SuccessWithMeta(c, http.StatusOK, "Road paths retrieved successfully", list, listMeta(q, total))
}

// GetPath godoc
// @Summary One stored road path with its coords
// @Tags road
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.RoadPathRow}
// @Router /api/road-data/paths/{id} [get]
func (h *RoadHandler) GetPath(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	p, err := h.repo.GetPathByID(c.Request.Context(), id)
	if err != nil {
		respondError(c, err, "Failed to get road path")
		return
	}
	if p == nil {
		response.Error(c, http.StatusNotFound, "Jalur nggak ada", nil)
		return
	}
	response.Success(c, http.StatusOK, "Road path retrieved successfully", p)
}

// DeletePath godoc
// @Summary Delete a stored road path (computed again when needed)
// @Tags road
// @Security BearerAuth
// @Router /api/road-data/paths/{id} [delete]
func (h *RoadHandler) DeletePath(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	found, err := h.repo.DeletePath(c.Request.Context(), id)
	if err != nil {
		respondError(c, err, "Failed to delete road path")
		return
	}
	if !found {
		response.Error(c, http.StatusNotFound, "Jalur nggak ada", nil)
		return
	}
	routing.Invalidate()
	response.Success(c, http.StatusOK, "Road path deleted", dto.DeletedCount{Deleted: 1})
}

// DeleteAllPaths godoc
// @Summary Delete every stored road path
// @Tags road
// @Security BearerAuth
// @Router /api/road-data/paths [delete]
func (h *RoadHandler) DeleteAllPaths(c *gin.Context) {
	n, err := h.repo.DeleteAllPaths(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to delete road paths")
		return
	}
	routing.Invalidate()
	response.Success(c, http.StatusOK, "Road paths deleted", dto.DeletedCount{Deleted: n})
}

// GetAreas godoc
// @Summary Stored OSM road data per area (no elements)
// @Tags road
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.RoadAreaRow}
// @Router /api/road-data/areas [get]
func (h *RoadHandler) GetAreas(c *gin.Context) {
	list, err := h.repo.ListAreas(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get road areas")
		return
	}
	response.Success(c, http.StatusOK, "Road areas retrieved successfully", list)
}

// DeleteArea godoc
// @Summary Delete the road data of one area (downloaded again when needed)
// @Tags road
// @Security BearerAuth
// @Router /api/road-data/areas/{id} [delete]
func (h *RoadHandler) DeleteArea(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	found, err := h.repo.DeleteArea(c.Request.Context(), id)
	if err != nil {
		respondError(c, err, "Failed to delete road area")
		return
	}
	if !found {
		response.Error(c, http.StatusNotFound, "Area nggak ada", nil)
		return
	}
	routing.Invalidate()
	response.Success(c, http.StatusOK, "Road area deleted", dto.DeletedCount{Deleted: 1})
}

// DeleteAllAreas godoc
// @Summary Delete the road data of every area
// @Tags road
// @Security BearerAuth
// @Router /api/road-data/areas [delete]
func (h *RoadHandler) DeleteAllAreas(c *gin.Context) {
	n, err := h.repo.DeleteAllAreas(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to delete road areas")
		return
	}
	routing.Invalidate()
	response.Success(c, http.StatusOK, "Road areas deleted", dto.DeletedCount{Deleted: n})
}
