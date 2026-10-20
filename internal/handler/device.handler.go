package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/amirullazmi0/fms-backend/pkg/teltonika"
	"github.com/gin-gonic/gin"
)

type DeviceHandler struct {
	deviceUsecase usecase.DeviceUsecase
}

func NewDeviceHandler(deviceUsecase usecase.DeviceUsecase) *DeviceHandler {
	return &DeviceHandler{deviceUsecase: deviceUsecase}
}

// GetDevices godoc
// @Summary Live state of every tracker
// @Tags devices
// @Security BearerAuth
// @Router /api/devices [get]
func (h *DeviceHandler) GetDevices(c *gin.Context) {
	response.Success(c, http.StatusOK, "Devices retrieved successfully", h.deviceUsecase.GetDevices(c.Request.Context()))
}

// GetDevice godoc
// @Summary One tracker with its recent history (?limit=, max 3000)
// @Tags devices
// @Security BearerAuth
// @Router /api/devices/{imei} [get]
func (h *DeviceHandler) GetDevice(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	d, err := h.deviceUsecase.GetDevice(c.Request.Context(), imei, limit)
	if err != nil {
		respondError(c, err, "Failed to get device")
		return
	}
	response.Success(c, http.StatusOK, "Device retrieved successfully", d)
}

// UpdateDevice godoc
// @Summary Set truck name / plate / vehicle
// @Tags devices
// @Security BearerAuth
// @Router /api/devices/{imei} [patch]
func (h *DeviceHandler) UpdateDevice(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	var req dto.UpdateDeviceRequest
	if !bindJSON(c, &req) {
		return
	}
	d, err := h.deviceUsecase.UpdateDevice(c.Request.Context(), imei, req, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to update device")
		return
	}
	response.Success(c, http.StatusOK, "Device updated successfully", d)
}

// DeleteDevice godoc
// @Summary Remove a tracker (soft delete; it re-registers if it connects again)
// @Tags devices
// @Security BearerAuth
// @Router /api/devices/{imei} [delete]
func (h *DeviceHandler) DeleteDevice(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	if err := h.deviceUsecase.DeleteDevice(c.Request.Context(), imei, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to delete device")
		return
	}
	response.Success(c, http.StatusOK, "Device deleted successfully", gin.H{"imei": imei})
}

// Day godoc
// @Summary Trips & stops of a period (?from=&to= ms, default today)
// @Tags devices
// @Security BearerAuth
// @Router /api/devices/{imei}/day [get]
func (h *DeviceHandler) Day(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	var q dto.DayQuery
	if !bindQuery(c, &q) {
		return
	}
	day, err := h.deviceUsecase.Day(c.Request.Context(), imei, q)
	if err != nil {
		respondError(c, err, "Failed to build day summary")
		return
	}
	response.Success(c, http.StatusOK, "Day summary retrieved successfully", day)
}

// Ritase godoc
// @Summary Loading → unloading ritase per route (?from=&to= ms, max 31 days)
// @Tags devices
// @Security BearerAuth
// @Router /api/devices/{imei}/ritase [get]
func (h *DeviceHandler) Ritase(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	var q dto.DayQuery
	if !bindQuery(c, &q) {
		return
	}
	rep, err := h.deviceUsecase.Ritase(c.Request.Context(), imei, q)
	if err != nil {
		respondError(c, err, "Failed to build ritase report")
		return
	}
	response.Success(c, http.StatusOK, "Ritase retrieved successfully", rep)
}

// Records godoc
// @Summary Raw AVL records (?from=&to= ms &limit= max 5000)
// @Tags devices
// @Security BearerAuth
// @Router /api/devices/{imei}/records [get]
func (h *DeviceHandler) Records(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	var q dto.RecordsQuery
	if !bindQuery(c, &q) {
		return
	}
	recs, err := h.deviceUsecase.Records(c.Request.Context(), imei, q)
	if err != nil {
		respondError(c, err, "Failed to get records")
		return
	}
	response.Success(c, http.StatusOK, "Records retrieved successfully", recs)
}

// RawRecords godoc
// @Summary Stored records of one truck with every IO/CAN value, newest first (?from=&to= ms &page= &limit= max 500)
// @Tags devices
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.AvlRecord}
// @Router /api/devices/{imei}/raw [get]
func (h *DeviceHandler) RawRecords(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	var q dto.RawRecordsQuery
	if !bindQuery(c, &q) {
		return
	}
	recs, total, page, limit, err := h.deviceUsecase.RawRecords(c.Request.Context(), imei, q)
	if err != nil {
		respondError(c, err, "Failed to get records")
		return
	}
	response.SuccessWithMeta(c, http.StatusOK, "Records retrieved successfully", recs, &response.Meta{
		TotalData: total, TotalPage: (total + limit - 1) / limit, CurrentPage: page, Limit: limit,
	})
}

// Command godoc
// @Summary Send a GPRS (Codec 12) command and wait for the reply (20 s)
// @Tags devices
// @Security BearerAuth
// @Router /api/devices/{imei}/command [post]
func (h *DeviceHandler) Command(c *gin.Context) {
	imei, ok := validImei(c)
	if !ok {
		return
	}
	var req dto.CommandRequest
	if !bindJSON(c, &req) {
		return
	}
	reply, err := h.deviceUsecase.SendCommand(c.Request.Context(), imei, strings.TrimSpace(req.Cmd))
	if err != nil {
		respondError(c, err, "Failed to send command")
		return
	}
	response.Success(c, http.StatusOK, "Command answered", dto.CommandResponse{Response: reply})
}

// Events godoc
// @Summary Latest 200 fleet events
// @Tags monitor
// @Security BearerAuth
// @Router /api/events [get]
func (h *DeviceHandler) Events(c *gin.Context) {
	response.Success(c, http.StatusOK, "Events retrieved successfully", h.deviceUsecase.Events())
}

// Commands godoc
// @Summary Latest 300 GPRS command lines
// @Tags monitor
// @Security BearerAuth
// @Router /api/commands [get]
func (h *DeviceHandler) Commands(c *gin.Context) {
	lines, err := h.deviceUsecase.Commands(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get commands")
		return
	}
	response.Success(c, http.StatusOK, "Commands retrieved successfully", lines)
}

// Status godoc
// @Summary Server / tunnel / device port status
// @Tags monitor
// @Security BearerAuth
// @Router /api/status [get]
func (h *DeviceHandler) Status(c *gin.Context) {
	response.Success(c, http.StatusOK, "Status retrieved successfully", h.deviceUsecase.Status())
}

// Logs godoc
// @Summary Device log ring buffer (?limit=)
// @Tags monitor
// @Security BearerAuth
// @Router /api/logs [get]
func (h *DeviceHandler) Logs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	response.Success(c, http.StatusOK, "Logs retrieved successfully", h.deviceUsecase.Logs(limit))
}

// IoMap godoc
// @Summary AVL IO ID dictionary
// @Tags monitor
// @Security BearerAuth
// @Router /api/io-map [get]
func (h *DeviceHandler) IoMap(c *gin.Context) {
	response.Success(c, http.StatusOK, "IO map retrieved successfully", gin.H{"io": teltonika.IO, "groups": teltonika.Groups})
}

// Place godoc
// @Summary Road/area name of a coordinate (?lat=&lng=); null until looked up
// @Tags monitor
// @Security BearerAuth
// @Router /api/place [get]
func (h *DeviceHandler) Place(c *gin.Context) {
	lat, err1 := strconv.ParseFloat(c.Query("lat"), 64)
	lng, err2 := strconv.ParseFloat(c.Query("lng"), 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		response.Error(c, http.StatusBadRequest, "lat/lng tidak valid", nil)
		return
	}
	response.Success(c, http.StatusOK, "Place retrieved successfully", dto.PlaceResponse{Place: h.deviceUsecase.Place(lat, lng)})
}
