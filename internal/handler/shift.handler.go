package handler

import (
	"net/http"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

type ShiftHandler struct {
	shiftUsecase usecase.ShiftUsecase
}

func NewShiftHandler(shiftUsecase usecase.ShiftUsecase) *ShiftHandler {
	return &ShiftHandler{shiftUsecase: shiftUsecase}
}

// ---------- Drivers ----------

// GetDrivers godoc
// @Summary List drivers (active first)
// @Tags drivers
// @Security BearerAuth
// @Router /api/drivers [get]
func (h *ShiftHandler) GetDrivers(c *gin.Context) {
	q, ok := listQuery(c)
	if !ok {
		return
	}
	drivers, total, err := h.shiftUsecase.GetDrivers(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get drivers")
		return
	}
	response.SuccessWithMeta(c, http.StatusOK, "Drivers retrieved successfully", drivers, listMeta(q, total))
}

// LastAssignments godoc
// @Summary Each driver's latest truck & route (form prefill)
// @Tags drivers
// @Security BearerAuth
// @Router /api/drivers/last [get]
func (h *ShiftHandler) LastAssignments(c *gin.Context) {
	last, err := h.shiftUsecase.LastAssignments(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get last assignments")
		return
	}
	response.Success(c, http.StatusOK, "Last assignments retrieved successfully", last)
}

// CreateDriver godoc
// @Summary Create a driver (admin/supervisor)
// @Tags drivers
// @Security BearerAuth
// @Router /api/drivers [post]
func (h *ShiftHandler) CreateDriver(c *gin.Context) {
	var body dto.UpsertDriverRequest
	if !bindJSON(c, &body) {
		return
	}
	d, err := h.shiftUsecase.CreateDriver(c.Request.Context(), body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to create operator")
		return
	}
	response.Success(c, http.StatusCreated, "Operator created successfully", d)
}

// UpdateDriver godoc
// @Summary Update a driver (admin/supervisor)
// @Tags drivers
// @Security BearerAuth
// @Router /api/drivers/{id} [put]
func (h *ShiftHandler) UpdateDriver(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.UpsertDriverRequest
	if !bindJSON(c, &body) {
		return
	}
	d, err := h.shiftUsecase.UpdateDriver(c.Request.Context(), id, body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to update operator")
		return
	}
	response.Success(c, http.StatusOK, "Operator updated successfully", d)
}

// DeleteDriver godoc
// @Summary Delete a driver; one ever assigned is deactivated instead
// @Tags drivers
// @Security BearerAuth
// @Router /api/drivers/{id} [delete]
func (h *ShiftHandler) DeleteDriver(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	r, err := h.shiftUsecase.DeleteDriver(c.Request.Context(), id, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to delete operator")
		return
	}
	msg := "Operator deleted successfully"
	if r.Result == "deactivated" {
		msg = "Operator sudah pernah bertugas, sehingga dinonaktifkan (bukan dihapus)"
	}
	response.Success(c, http.StatusOK, msg, r)
}

// ---------- Shift templates ----------

// GetTemplates godoc
// @Summary List shift templates
// @Tags shift-templates
// @Security BearerAuth
// @Router /api/shift-templates [get]
func (h *ShiftHandler) GetTemplates(c *gin.Context) {
	list, err := h.shiftUsecase.GetTemplates(c.Request.Context())
	if err != nil {
		respondError(c, err, "Failed to get shift templates")
		return
	}
	response.Success(c, http.StatusOK, "Shift templates retrieved successfully", list)
}

// CreateTemplate godoc
// @Summary Create a shift template (admin/supervisor)
// @Tags shift-templates
// @Security BearerAuth
// @Router /api/shift-templates [post]
func (h *ShiftHandler) CreateTemplate(c *gin.Context) {
	var body dto.UpsertShiftTemplateRequest
	if !bindJSON(c, &body) {
		return
	}
	t, err := h.shiftUsecase.CreateTemplate(c.Request.Context(), body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to create shift template")
		return
	}
	response.Success(c, http.StatusCreated, "Shift template created successfully", t)
}

// UpdateTemplate godoc
// @Summary Update a shift template (admin/supervisor)
// @Tags shift-templates
// @Security BearerAuth
// @Router /api/shift-templates/{id} [put]
func (h *ShiftHandler) UpdateTemplate(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.UpsertShiftTemplateRequest
	if !bindJSON(c, &body) {
		return
	}
	t, err := h.shiftUsecase.UpdateTemplate(c.Request.Context(), id, body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to update shift template")
		return
	}
	response.Success(c, http.StatusOK, "Shift template updated successfully", t)
}

// DeleteTemplate godoc
// @Summary Delete a shift template (admin/supervisor)
// @Tags shift-templates
// @Security BearerAuth
// @Router /api/shift-templates/{id} [delete]
func (h *ShiftHandler) DeleteTemplate(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	if err := h.shiftUsecase.DeleteTemplate(c.Request.Context(), id, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to delete shift template")
		return
	}
	response.Success(c, http.StatusOK, "Shift template deleted successfully", gin.H{"id": id})
}

// ---------- Shifts ----------

// GetShifts godoc
// @Summary Shifts by work date (?from=YYYY-MM-DD&to=YYYY-MM-DD) plus every active shift
// @Tags shifts
// @Security BearerAuth
// @Router /api/shifts [get]
func (h *ShiftHandler) GetShifts(c *gin.Context) {
	var q dto.ShiftListQuery
	if !bindQuery(c, &q) {
		return
	}
	list, err := h.shiftUsecase.GetShifts(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get shifts")
		return
	}
	response.Success(c, http.StatusOK, "Shifts retrieved successfully", list)
}

// GetShift godoc
// @Summary One shift with its stored ritase
// @Tags shifts
// @Security BearerAuth
// @Router /api/shifts/{id} [get]
func (h *ShiftHandler) GetShift(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	s, err := h.shiftUsecase.GetShift(c.Request.Context(), id)
	if err != nil {
		respondError(c, err, "Failed to get shift")
		return
	}
	response.Success(c, http.StatusOK, "Shift retrieved successfully", s)
}

// CreateShift godoc
// @Summary Plan a shift (admin/supervisor)
// @Tags shifts
// @Security BearerAuth
// @Router /api/shifts [post]
func (h *ShiftHandler) CreateShift(c *gin.Context) {
	var body dto.CreateShiftRequest
	if !bindJSON(c, &body) {
		return
	}
	s, err := h.shiftUsecase.CreateShift(c.Request.Context(), body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to create shift")
		return
	}
	response.Success(c, http.StatusCreated, "Shift created successfully", s)
}

// UpdateShift godoc
// @Summary Partial update of a shift (admin/supervisor)
// @Tags shifts
// @Security BearerAuth
// @Router /api/shifts/{id} [patch]
func (h *ShiftHandler) UpdateShift(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.UpdateShiftRequest
	if !bindJSON(c, &body) {
		return
	}
	s, err := h.shiftUsecase.UpdateShift(c.Request.Context(), id, body, c.GetString("user_id"))
	if err != nil {
		respondError(c, err, "Failed to update shift")
		return
	}
	response.Success(c, http.StatusOK, "Shift updated successfully", s)
}

// DeleteShift godoc
// @Summary Delete a planned shift (admin/supervisor)
// @Tags shifts
// @Security BearerAuth
// @Router /api/shifts/{id} [delete]
func (h *ShiftHandler) DeleteShift(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	if err := h.shiftUsecase.DeleteShift(c.Request.Context(), id, c.GetString("user_id")); err != nil {
		respondError(c, err, "Failed to delete shift")
		return
	}
	response.Success(c, http.StatusOK, "Shift deleted successfully", gin.H{"id": id})
}

func (h *ShiftHandler) transition(c *gin.Context, do func(id string) (*dto.Shift, error), msg string) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	s, err := do(id)
	if err != nil {
		respondError(c, err, "Failed to update shift")
		return
	}
	response.Success(c, http.StatusOK, msg, s)
}

// StartShift godoc
// @Summary planned → active
// @Tags shifts
// @Security BearerAuth
// @Router /api/shifts/{id}/start [post]
func (h *ShiftHandler) StartShift(c *gin.Context) {
	h.transition(c, func(id string) (*dto.Shift, error) { return h.shiftUsecase.StartShift(c.Request.Context(), id) }, "Shift dimulai")
}

// CloseShift godoc
// @Summary active → closed (manual)
// @Tags shifts
// @Security BearerAuth
// @Router /api/shifts/{id}/close [post]
func (h *ShiftHandler) CloseShift(c *gin.Context) {
	h.transition(c, func(id string) (*dto.Shift, error) {
		return h.shiftUsecase.CloseShift(c.Request.Context(), id, c.GetString("user_id"))
	}, "Shift ditutup")
}

// ReopenShift godoc
// @Summary closed → active
// @Tags shifts
// @Security BearerAuth
// @Router /api/shifts/{id}/reopen [post]
func (h *ShiftHandler) ReopenShift(c *gin.Context) {
	h.transition(c, func(id string) (*dto.Shift, error) { return h.shiftUsecase.ReopenShift(c.Request.Context(), id) }, "Shift dibuka kembali")
}

// ---------- Assignments ----------

// AddAssignment godoc
// @Summary Assign a driver + truck to a shift
// @Tags assignments
// @Security BearerAuth
// @Router /api/shifts/{id}/assignments [post]
func (h *ShiftHandler) AddAssignment(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.AssignmentRequest
	if !bindJSON(c, &body) {
		return
	}
	a, err := h.shiftUsecase.AddAssignment(c.Request.Context(), id, body)
	if err != nil {
		respondError(c, err, "Failed to add assignment")
		return
	}
	response.Success(c, http.StatusCreated, "Penugasan ditambah", a)
}

// CopyPrevious godoc
// @Summary Copy assignments from the previous shift on the same route
// @Tags assignments
// @Security BearerAuth
// @Router /api/shifts/{id}/copy-previous [post]
func (h *ShiftHandler) CopyPrevious(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	r, err := h.shiftUsecase.CopyPrevious(c.Request.Context(), id)
	if err != nil {
		respondError(c, err, "Failed to copy assignments")
		return
	}
	response.Success(c, http.StatusOK, "Penugasan disalin", r)
}

// UpdateAssignment godoc
// @Summary Change driver / truck of an assignment
// @Tags assignments
// @Security BearerAuth
// @Router /api/assignments/{id} [put]
func (h *ShiftHandler) UpdateAssignment(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	var body dto.AssignmentRequest
	if !bindJSON(c, &body) {
		return
	}
	a, err := h.shiftUsecase.UpdateAssignment(c.Request.Context(), id, body)
	if err != nil {
		respondError(c, err, "Failed to update assignment")
		return
	}
	response.Success(c, http.StatusOK, "Penugasan diubah", a)
}

// EndAssignment godoc
// @Summary Release a driver (deleted, or ended when it already has ritase)
// @Tags assignments
// @Security BearerAuth
// @Router /api/assignments/{id} [delete]
func (h *ShiftHandler) EndAssignment(c *gin.Context) {
	id, ok := validUUID(c, "id")
	if !ok {
		return
	}
	r, err := h.shiftUsecase.EndAssignment(c.Request.Context(), id)
	if err != nil {
		respondError(c, err, "Failed to end assignment")
		return
	}
	msg := "Penugasan dihapus"
	if r.Result == "ended" {
		msg = "Penugasan diakhiri (ritase tetap tercatat)"
	}
	response.Success(c, http.StatusOK, msg, r)
}

// ---------- Reports ----------

// Report godoc
// @Summary Ritase report grouped by route|driver|truck|shift|day
// @Tags reports
// @Security BearerAuth
// @Router /api/reports [get]
func (h *ShiftHandler) Report(c *gin.Context) {
	var q dto.ReportQuery
	if !bindQuery(c, &q) {
		return
	}
	r, err := h.shiftUsecase.Report(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to build report")
		return
	}
	response.Success(c, http.StatusOK, "Report retrieved successfully", r)
}

// ReportVisits godoc
// @Summary Time at loading / unloading sites (finished + trucks inside now)
// @Tags reports
// @Security BearerAuth
// @Success 200 {object} response.Response{data=dto.VisitReport}
// @Router /api/reports/visits [get]
func (h *ShiftHandler) ReportVisits(c *gin.Context) {
	var q dto.ReportQuery
	if !bindQuery(c, &q) {
		return
	}
	r, err := h.shiftUsecase.ReportVisits(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to build visit report")
		return
	}
	response.Success(c, http.StatusOK, "Visit report retrieved successfully", r)
}

// ReportVisitList godoc
// @Summary Raw stays at loading / unloading sites (?role=loading|unloading &limit= max 2000 &offset=)
// @Tags reports
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]dto.SiteVisitRecord}
// @Router /api/reports/visits/list [get]
func (h *ShiftHandler) ReportVisitList(c *gin.Context) {
	var q dto.ReportQuery
	if !bindQuery(c, &q) {
		return
	}
	list, total, err := h.shiftUsecase.ReportVisitList(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get visits")
		return
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, 2000)
	response.SuccessWithMeta(c, http.StatusOK, "Visits retrieved successfully", list, &response.Meta{
		TotalData: total, TotalPage: (total + limit - 1) / limit, CurrentPage: q.Offset/limit + 1, Limit: limit,
	})
}

// ReportTrips godoc
// @Summary Stored ritase list (?limit= max 2000 &offset=)
// @Tags reports
// @Security BearerAuth
// @Router /api/reports/trips [get]
func (h *ShiftHandler) ReportTrips(c *gin.Context) {
	var q dto.ReportQuery
	if !bindQuery(c, &q) {
		return
	}
	trips, total, err := h.shiftUsecase.ReportTrips(c.Request.Context(), q)
	if err != nil {
		respondError(c, err, "Failed to get trips")
		return
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, 2000)
	page := q.Offset/limit + 1
	response.SuccessWithMeta(c, http.StatusOK, "Trips retrieved successfully", trips, &response.Meta{
		TotalData: total, TotalPage: (total + limit - 1) / limit, CurrentPage: page, Limit: limit,
	})
}
