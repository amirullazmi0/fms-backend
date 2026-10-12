package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/middleware"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type FeatureHandler struct {
	featureUsecase usecase.FeatureUsecase
}

func NewFeatureHandler(featureUsecase usecase.FeatureUsecase) *FeatureHandler {
	return &FeatureHandler{featureUsecase: featureUsecase}
}

// GetFeatures godoc
// @Summary Feature switches and their config
// @Tags feature
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.FeatureResponse}
// @Router /api/features [get]
func (h *FeatureHandler) GetFeatures(c *gin.Context) {
	list, err := h.featureUsecase.List(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get features")
		return
	}
	response.Success(c, http.StatusOK, "Features retrieved successfully", list)
}

// UpdateFeature godoc
// @Summary Switch a feature on/off and/or replace its config
// @Tags feature
// @Security BearerAuth
// @Param body body dto.UpdateFeatureRequest true "Feature"
// @Success 200 {object} response.Response{data=dto.FeatureResponse}
// @Router /api/features/{key} [put]
func (h *FeatureHandler) UpdateFeature(c *gin.Context) {
	actor, _ := middleware.ActorFrom(c)
	var req dto.UpdateFeatureRequest
	if !bindJSON(c, &req) {
		return
	}
	f, err := h.featureUsecase.Update(c.Request.Context(), actor, c.Param("key"), &req)
	if err != nil {
		respondError(c, err, "Failed to update feature")
		return
	}
	response.Success(c, http.StatusOK, "Feature updated successfully", f)
}

// GetSimulation godoc
// @Summary State of the backend's truck simulator
// @Tags feature
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.SimulationStatus}
// @Router /api/simulation [get]
func (h *FeatureHandler) GetSimulation(c *gin.Context) {
	s, err := h.featureUsecase.SimulationStatus(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get simulation")
		return
	}
	response.Success(c, http.StatusOK, "Simulation retrieved successfully", s)
}

// StartSimulation godoc
// @Summary Start fake FMC150 trucks (feature "simulation" must be on)
// @Tags feature
// @Security BearerAuth
// @Param body body dto.StartSimulationRequest false "Override devices / interval"
// @Success 200 {object} response.Response{data=dto.SimulationStatus}
// @Router /api/simulation/start [post]
func (h *FeatureHandler) StartSimulation(c *gin.Context) {
	actor, _ := middleware.ActorFrom(c)
	var req dto.StartSimulationRequest
	if c.Request.ContentLength > 0 && !bindJSON(c, &req) {
		return
	}
	s, err := h.featureUsecase.StartSimulation(c.Request.Context(), actor, &req)
	if err != nil {
		respondError(c, err, "Failed to start simulation")
		return
	}
	response.Success(c, http.StatusOK, "Simulation started", s)
}

// StopSimulation godoc
// @Summary Stop the fake trucks
// @Tags feature
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.SimulationStatus}
// @Router /api/simulation/stop [post]
func (h *FeatureHandler) StopSimulation(c *gin.Context) {
	actor, _ := middleware.ActorFrom(c)
	s, err := h.featureUsecase.StopSimulation(c.Request.Context(), actor)
	if err != nil {
		respondError(c, err, "Failed to stop simulation")
		return
	}
	response.Success(c, http.StatusOK, "Simulation stopped", s)
}
