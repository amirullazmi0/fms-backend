package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type ProductionHandler struct {
	productionUsecase usecase.ProductionUsecase
}

func NewProductionHandler(productionUsecase usecase.ProductionUsecase) *ProductionHandler {
	return &ProductionHandler{productionUsecase: productionUsecase}
}

// GetTargets godoc
// @Summary Production targets per route (fleet)
// @Tags production
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.ProductionTarget}
// @Router /api/production-targets [get]
func (h *ProductionHandler) GetTargets(c *gin.Context) {
	list, err := h.productionUsecase.GetTargets(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get targets")
		return
	}
	response.Success(c, http.StatusOK, "Targets retrieved successfully", list)
}

// CreateTarget godoc
// @Summary Create a production target
// @Tags production
// @Security BearerAuth
// @Param body body dto.UpsertTargetRequest true "Target"
// @Router /api/production-targets [post]
func (h *ProductionHandler) CreateTarget(c *gin.Context) {
	var body dto.UpsertTargetRequest
	if !bindJSON(c, &body) {
		return
	}
	t, err := h.productionUsecase.CreateTarget(c.Request.Context(), body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to create target")
		return
	}
	response.Success(c, http.StatusCreated, "Target created successfully", t)
}

// UpdateTarget godoc
// @Summary Update a production target
// @Tags production
// @Security BearerAuth
// @Param body body dto.UpsertTargetRequest true "Target"
// @Router /api/production-targets/{id} [put]
func (h *ProductionHandler) UpdateTarget(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.UpsertTargetRequest
	if !bindJSON(c, &body) {
		return
	}
	t, err := h.productionUsecase.UpdateTarget(c.Request.Context(), id, body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to update target")
		return
	}
	response.Success(c, http.StatusOK, "Target updated successfully", t)
}

// DeleteTarget godoc
// @Summary Delete a production target
// @Tags production
// @Security BearerAuth
// @Router /api/production-targets/{id} [delete]
func (h *ProductionHandler) DeleteTarget(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	if err := h.productionUsecase.DeleteTarget(c.Request.Context(), id, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to delete target")
		return
	}
	response.Success(c, http.StatusOK, "Target deleted successfully", gin.H{"id": id})
}

// Hourly godoc
// @Summary Ritase & tonase per hour vs target, with provisional / final status
// @Tags reports
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.HourlyReport}
// @Router /api/reports/hourly [get]
func (h *ProductionHandler) Hourly(c *gin.Context) {
	var q dto.ReportQuery
	if !bindQuery(c, &q) {
		return
	}
	r, err := h.productionUsecase.Hourly(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to build hourly report")
		return
	}
	response.Success(c, http.StatusOK, "Hourly report retrieved successfully", r)
}

// Freshness godoc
// @Summary Data freshness per unit: newest GPS time, last received, delay
// @Tags reports
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.Freshness}
// @Router /api/reports/freshness [get]
func (h *ProductionHandler) Freshness(c *gin.Context) {
	list, err := h.productionUsecase.Freshness(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get data freshness")
		return
	}
	response.Success(c, http.StatusOK, "Freshness retrieved successfully", list)
}

// Stages godoc
// @Summary Cycle time per stage per route (queue, spot, hauling, dumping, return)
// @Tags reports
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.StageReport}
// @Router /api/reports/stages [get]
func (h *ProductionHandler) Stages(c *gin.Context) {
	var q dto.ReportQuery
	if !bindQuery(c, &q) {
		return
	}
	r, err := h.productionUsecase.Stages(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to build stage report")
		return
	}
	response.Success(c, http.StatusOK, "Stage report retrieved successfully", r)
}
