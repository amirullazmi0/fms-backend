package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type DispatchHandler struct {
	dispatchUsecase usecase.DispatchUsecase
	shiftUsecase    usecase.ShiftUsecase
}

func NewDispatchHandler(dispatchUsecase usecase.DispatchUsecase, shiftUsecase usecase.ShiftUsecase) *DispatchHandler {
	return &DispatchHandler{dispatchUsecase: dispatchUsecase, shiftUsecase: shiftUsecase}
}

// Board godoc
// @Summary Panel dispatcher: per fleet MF live, DT antre, hanging time excavator, DT tidak aktif, saran pindah DT
// @Tags dispatch
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.DispatchBoard}
// @Router /api/dispatch/fleets [get]
func (h *DispatchHandler) Board(c *gin.Context) {
	b, err := h.dispatchUsecase.Board(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to build dispatch board")
		return
	}
	response.Success(c, http.StatusOK, "Dispatch board retrieved successfully", b)
}

// SetLoader godoc
// @Summary Pasang excavator di fleet (rute), atau lepas (imei kosong)
// @Tags dispatch
// @Security BearerAuth
// @Param body body dto.SetLoaderRequest true "Excavator"
// @Router /api/dispatch/fleets/{id}/loader [put]
func (h *DispatchHandler) SetLoader(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.SetLoaderRequest
	if !bindJSON(c, &body) {
		return
	}
	if err := h.dispatchUsecase.SetLoader(c.Request.Context(), id, body, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to set loader")
		return
	}
	response.Success(c, http.StatusOK, "Loader updated successfully", nil)
}

// Move godoc
// @Summary Pindahkan unit ke shift berjalan di fleet lain (operator sama, mulai sekarang)
// @Tags dispatch
// @Security BearerAuth
// @Param body body dto.MoveUnitRequest true "Unit & fleet tujuan"
// @Success 200 {object} response.Response{data=dto.Assignment}
// @Router /api/dispatch/move [post]
func (h *DispatchHandler) Move(c *gin.Context) {
	var body dto.MoveUnitRequest
	if !bindJSON(c, &body) {
		return
	}
	a, err := h.shiftUsecase.MoveUnit(c.Request.Context(), body.Imei, body.ToRouteID)
	if err != nil {
		respondError(c, err, "Failed to move unit")
		return
	}
	response.Success(c, http.StatusOK, "Unit moved successfully", a)
}
