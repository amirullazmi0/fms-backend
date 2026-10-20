package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/middleware"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

// MdtHandler: the MDT tablet in the cab (fms-mdt) and the unit status / delay code endpoints
// the dashboards use.
type MdtHandler struct {
	mdtUsecase usecase.MdtUsecase
}

func NewMdtHandler(mdtUsecase usecase.MdtUsecase) *MdtHandler {
	return &MdtHandler{mdtUsecase: mdtUsecase}
}

// ---------- Supervisor side ----------

// PairCode godoc
// @Summary New 6-digit code to pair a tablet with this unit (valid 10 minutes)
// @Tags mdt
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.MdtPairCode}
// @Router /api/devices/{imei}/mdt [post]
func (h *MdtHandler) PairCode(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	code, err := h.mdtUsecase.PairCode(c.Request.Context(), imei, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to create pairing code")
		return
	}
	response.Success(c, http.StatusOK, "Pairing code created", code)
}

// Tablets godoc
// @Summary Tablets paired with this unit
// @Tags mdt
// @Security BearerAuth
// @Router /api/devices/{imei}/mdt [get]
func (h *MdtHandler) Tablets(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	list, err := h.mdtUsecase.Tablets(c.Request.Context(), imei)
	if err != nil {
		respondError(c, err, "Failed to get tablets")
		return
	}
	response.Success(c, http.StatusOK, "Tablets retrieved successfully", list)
}

// RevokeTablet godoc
// @Summary Revoke a tablet (it must pair again)
// @Tags mdt
// @Security BearerAuth
// @Router /api/mdt-tablets/{id} [delete]
func (h *MdtHandler) RevokeTablet(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	if err := h.mdtUsecase.RevokeTablet(c.Request.Context(), id); err != nil {
		respondError(c, err, "Failed to revoke tablet")
		return
	}
	response.Success(c, http.StatusOK, "Tablet revoked", gin.H{"id": id})
}

// SetPin godoc
// @Summary Set an operator's MDT PIN
// @Tags mdt
// @Security BearerAuth
// @Param body body dto.SetPinRequest true "PIN"
// @Router /api/drivers/{id}/pin [put]
func (h *MdtHandler) SetPin(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.SetPinRequest
	if !bindJSON(c, &body) {
		return
	}
	if err := h.mdtUsecase.SetPin(c.Request.Context(), id, body, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to set PIN")
		return
	}
	response.Success(c, http.StatusOK, "PIN updated", gin.H{"id": id})
}

// DelayCodes godoc
// @Summary Delay codes (?all=true includes inactive ones)
// @Tags unit-status
// @Security BearerAuth
// @Router /api/delay-codes [get]
func (h *MdtHandler) DelayCodes(c *gin.Context) {
	list, err := h.mdtUsecase.DelayCodes(c.Request.Context(), c.Query("all") == "true")
	if err != nil {
		respondError(c, err, "Failed to get delay codes")
		return
	}
	response.Success(c, http.StatusOK, "Delay codes retrieved successfully", list)
}

// UpsertDelayCode godoc
// @Summary Create or update a delay code
// @Tags unit-status
// @Security BearerAuth
// @Param body body dto.UpsertDelayCodeRequest true "Delay code"
// @Router /api/delay-codes [put]
func (h *MdtHandler) UpsertDelayCode(c *gin.Context) {
	var body dto.UpsertDelayCodeRequest
	if !bindJSON(c, &body) {
		return
	}
	d, err := h.mdtUsecase.UpsertDelayCode(c.Request.Context(), body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to save delay code")
		return
	}
	response.Success(c, http.StatusOK, "Delay code saved", d)
}

// StatusLogs godoc
// @Summary Unit status history (?imei=&from=&to=, default last 24 h)
// @Tags unit-status
// @Security BearerAuth
// @Router /api/unit-status [get]
func (h *MdtHandler) StatusLogs(c *gin.Context) {
	var q dto.UnitStatusQuery
	if !bindQuery(c, &q) {
		return
	}
	list, err := h.mdtUsecase.StatusLogs(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get unit status")
		return
	}
	response.Success(c, http.StatusOK, "Unit status retrieved successfully", list)
}

// CurrentStatuses godoc
// @Summary Current status of every unit
// @Tags unit-status
// @Security BearerAuth
// @Router /api/unit-status/current [get]
func (h *MdtHandler) CurrentStatuses(c *gin.Context) {
	list, err := h.mdtUsecase.CurrentStatuses(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get unit status")
		return
	}
	response.Success(c, http.StatusOK, "Unit status retrieved successfully", list)
}

// SetStatus godoc
// @Summary Set a unit's status by hand (supervisor)
// @Tags unit-status
// @Security BearerAuth
// @Param body body dto.SetUnitStatusRequest true "Status"
// @Router /api/unit-status [post]
func (h *MdtHandler) SetStatus(c *gin.Context) {
	var body dto.SetUnitStatusRequest
	if !bindJSON(c, &body) {
		return
	}
	l, err := h.mdtUsecase.SetStatus(c.Request.Context(), body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to set unit status")
		return
	}
	response.Success(c, http.StatusCreated, "Unit status saved", l)
}

// ---------- Tablet side (fms-mdt) ----------

// Pair godoc
// @Summary Pair this tablet with a unit using the code from the supervisor
// @Tags mdt
// @Param body body dto.MdtPairRequest true "Code"
// @Success 200 {object} response.Response{data=dto.MdtPairResponse}
// @Router /api/mdt/pair [post]
func (h *MdtHandler) Pair(c *gin.Context) {
	var body dto.MdtPairRequest
	if !bindJSON(c, &body) {
		return
	}
	r, err := h.mdtUsecase.Pair(c.Request.Context(), body)
	if err != nil {
		respondError(c, err, "Failed to pair tablet")
		return
	}
	response.Success(c, http.StatusOK, "Tablet paired", r)
}

// Login godoc
// @Summary Operator login on the tablet (NIK + PIN)
// @Tags mdt
// @Param body body dto.MdtLoginRequest true "Login"
// @Router /api/mdt/login [post]
func (h *MdtHandler) Login(c *gin.Context) {
	var body dto.MdtLoginRequest
	if !bindJSON(c, &body) {
		return
	}
	op, err := h.mdtUsecase.Login(c.Request.Context(), body)
	if err != nil {
		respondError(c, err, "Failed to log in")
		return
	}
	response.Success(c, http.StatusOK, "Login OK", op)
}

// Bootstrap godoc
// @Summary Everything the tablet caches: unit, duty, production, delay codes, operators
// @Tags mdt
// @Success 200 {object} response.Response{data=dto.MdtBootstrap}
// @Router /api/mdt/bootstrap [get]
func (h *MdtHandler) Bootstrap(c *gin.Context) {
	b, err := h.mdtUsecase.Bootstrap(c.Request.Context(), c.GetString(middleware.CtxMdtImei), c.GetString(middleware.CtxMdtTablet))
	if err != nil {
		respondError(c, err, "Failed to load MDT data")
		return
	}
	response.Success(c, http.StatusOK, "MDT data", b)
}

// Position godoc
// @Summary Latest position of this tablet's unit (from its tracker)
// @Tags mdt
// @Success 200 {object} response.Response{data=dto.MdtPosition}
// @Router /api/mdt/position [get]
func (h *MdtHandler) Position(c *gin.Context) {
	response.Success(c, http.StatusOK, "Unit position", h.mdtUsecase.Position(c.GetString(middleware.CtxMdtImei)))
}

// SubmitStatuses godoc
// @Summary Status changes made on the tablet (sent when there is signal)
// @Tags mdt
// @Param body body dto.MdtStatusBatch true "Changes"
// @Success 200 {object} response.Response{data=dto.MdtStatusBatchResult}
// @Router /api/mdt/status [post]
func (h *MdtHandler) SubmitStatuses(c *gin.Context) {
	var body dto.MdtStatusBatch
	if !bindJSON(c, &body) {
		return
	}
	r, err := h.mdtUsecase.SubmitStatuses(c.Request.Context(), c.GetString(middleware.CtxMdtImei), c.GetString(middleware.CtxMdtTablet), body)
	if err != nil {
		respondError(c, err, "Failed to save status")
		return
	}
	response.Success(c, http.StatusOK, "Status saved", r)
}
