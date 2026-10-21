package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type SpeedZoneHandler struct {
	speedZoneUsecase usecase.SpeedZoneUsecase
}

func NewSpeedZoneHandler(speedZoneUsecase usecase.SpeedZoneUsecase) *SpeedZoneHandler {
	return &SpeedZoneHandler{speedZoneUsecase: speedZoneUsecase}
}

// List godoc
// @Summary Zona batas kecepatan
// @Tags speed-zones
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.SpeedZone}
// @Router /api/speed-zones [get]
func (h *SpeedZoneHandler) List(c *gin.Context) {
	list, err := h.speedZoneUsecase.List(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get speed zones")
		return
	}
	response.Success(c, http.StatusOK, "Speed zones retrieved successfully", list)
}

// Create godoc
// @Summary Buat zona batas kecepatan
// @Tags speed-zones
// @Security BearerAuth
// @Param body body dto.UpsertSpeedZoneRequest true "Zona"
// @Router /api/speed-zones [post]
func (h *SpeedZoneHandler) Create(c *gin.Context) {
	var body dto.UpsertSpeedZoneRequest
	if !bindJSON(c, &body) {
		return
	}
	if err := h.speedZoneUsecase.Create(c.Request.Context(), body, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to create speed zone")
		return
	}
	response.Success(c, http.StatusCreated, "Speed zone created successfully", nil)
}

// Update godoc
// @Summary Ubah zona batas kecepatan
// @Tags speed-zones
// @Security BearerAuth
// @Param body body dto.UpsertSpeedZoneRequest true "Zona"
// @Router /api/speed-zones/{id} [put]
func (h *SpeedZoneHandler) Update(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.UpsertSpeedZoneRequest
	if !bindJSON(c, &body) {
		return
	}
	if err := h.speedZoneUsecase.Update(c.Request.Context(), id, body, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to update speed zone")
		return
	}
	response.Success(c, http.StatusOK, "Speed zone updated successfully", nil)
}

// Delete godoc
// @Summary Hapus zona batas kecepatan
// @Tags speed-zones
// @Security BearerAuth
// @Router /api/speed-zones/{id} [delete]
func (h *SpeedZoneHandler) Delete(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	if err := h.speedZoneUsecase.Delete(c.Request.Context(), id, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to delete speed zone")
		return
	}
	response.Success(c, http.StatusOK, "Speed zone deleted successfully", nil)
}

// Report godoc
// @Summary Overspeed di zona batas kecepatan: episode & ringkasan per operator
// @Tags speed-zones
// @Security BearerAuth
// @Param from query int false "Start (ms)"
// @Param to query int false "End (ms)"
// @Success 200 {object} response.Response{data=dto.OverspeedReport}
// @Router /api/reports/overspeed [get]
func (h *SpeedZoneHandler) Report(c *gin.Context) {
	var q dto.ReportQuery
	if !bindQuery(c, &q) {
		return
	}
	r, err := h.speedZoneUsecase.Report(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to build overspeed report")
		return
	}
	response.Success(c, http.StatusOK, "Overspeed report retrieved successfully", r)
}
