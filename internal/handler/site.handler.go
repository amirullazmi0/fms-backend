package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type SiteHandler struct {
	siteUsecase usecase.SiteUsecase
}

func NewSiteHandler(siteUsecase usecase.SiteUsecase) *SiteHandler {
	return &SiteHandler{siteUsecase: siteUsecase}
}

// GetSites godoc
// @Summary List sites (geofence points) — ?search=&sort=&order=&page=&limit=
// @Tags sites
// @Security BearerAuth
// @Router /api/sites [get]
func (h *SiteHandler) GetSites(c *gin.Context) {
	q, ok := listQuery(c)
	if !ok {
		return
	}
	sites, total, err := h.siteUsecase.GetSites(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get sites")
		return
	}
	response.SuccessWithMeta(c, http.StatusOK, "Sites retrieved successfully", sites, listMeta(q, total))
}

// CreateSite godoc
// @Summary Create a site (admin)
// @Tags sites
// @Security BearerAuth
// @Router /api/sites [post]
func (h *SiteHandler) CreateSite(c *gin.Context) {
	var body dto.UpsertSiteRequest
	if !bindJSON(c, &body) {
		return
	}
	site, err := h.siteUsecase.CreateSite(c.Request.Context(), body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to create site")
		return
	}
	response.Success(c, http.StatusCreated, "Site created successfully", site)
}

// UpdateSite godoc
// @Summary Update a site (admin)
// @Tags sites
// @Security BearerAuth
// @Router /api/sites/{id} [put]
func (h *SiteHandler) UpdateSite(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.UpsertSiteRequest
	if !bindJSON(c, &body) {
		return
	}
	site, err := h.siteUsecase.UpdateSite(c.Request.Context(), id, body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to update site")
		return
	}
	response.Success(c, http.StatusOK, "Site updated successfully", site)
}

// DeleteSite godoc
// @Summary Delete a site (admin, soft delete; 409 while a route uses it)
// @Tags sites
// @Security BearerAuth
// @Router /api/sites/{id} [delete]
func (h *SiteHandler) DeleteSite(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	if err := h.siteUsecase.DeleteSite(c.Request.Context(), id, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to delete site")
		return
	}
	response.Success(c, http.StatusOK, "Site deleted successfully", gin.H{"id": id})
}

// GetRoutes godoc
// @Summary List routes with who is on duty
// @Tags routes
// @Security BearerAuth
// @Router /api/routes [get]
func (h *SiteHandler) GetRoutes(c *gin.Context) {
	q, ok := listQuery(c)
	if !ok {
		return
	}
	routes, total, err := h.siteUsecase.GetRoutes(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get routes")
		return
	}
	response.SuccessWithMeta(c, http.StatusOK, "Routes retrieved successfully", routes, listMeta(q, total))
}

// CreateRoute godoc
// @Summary Create a route (admin)
// @Tags routes
// @Security BearerAuth
// @Router /api/routes [post]
func (h *SiteHandler) CreateRoute(c *gin.Context) {
	var body dto.UpsertRouteRequest
	if !bindJSON(c, &body) {
		return
	}
	route, err := h.siteUsecase.CreateRoute(c.Request.Context(), body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to create route")
		return
	}
	response.Success(c, http.StatusCreated, "Route created successfully", route)
}

// UpdateRoute godoc
// @Summary Update a route (admin)
// @Tags routes
// @Security BearerAuth
// @Router /api/routes/{id} [put]
func (h *SiteHandler) UpdateRoute(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.UpsertRouteRequest
	if !bindJSON(c, &body) {
		return
	}
	route, err := h.siteUsecase.UpdateRoute(c.Request.Context(), id, body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to update route")
		return
	}
	response.Success(c, http.StatusOK, "Route updated successfully", route)
}

// DeleteRoute godoc
// @Summary Delete a route (admin; 409 once used by a shift)
// @Tags routes
// @Security BearerAuth
// @Router /api/routes/{id} [delete]
func (h *SiteHandler) DeleteRoute(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	if err := h.siteUsecase.DeleteRoute(c.Request.Context(), id, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to delete route")
		return
	}
	response.Success(c, http.StatusOK, "Route deleted successfully", gin.H{"id": id})
}

// RouteHistory godoc
// @Summary Real path + estimates from finished ritase (?days=, default 14, max 60)
// @Tags routes
// @Security BearerAuth
// @Router /api/routes/{id}/history [get]
func (h *SiteHandler) RouteHistory(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var q dto.RouteHistoryQuery
	if !bindQuery(c, &q) {
		return
	}
	hist, err := h.siteUsecase.RouteHistory(c.Request.Context(), id, q.Days)
	if err != nil {
		respondError(c, err, "Failed to build route history")
		return
	}
	response.Success(c, http.StatusOK, "Route history retrieved successfully", hist)
}

// Road godoc
// @Summary Road path + estimate between two sites (?from=<site id>&to=<site id>)
// @Tags routes
// @Security BearerAuth
// @Router /api/road [get]
func (h *SiteHandler) Road(c *gin.Context) {
	var q dto.RoadQuery
	if !bindQuery(c, &q) {
		return
	}
	road, err := h.siteUsecase.Road(c.Request.Context(), q.From, q.To)
	if err != nil {
		respondError(c, err, "Failed to get road path")
		return
	}
	response.Success(c, http.StatusOK, "Road path retrieved successfully", road)
}
