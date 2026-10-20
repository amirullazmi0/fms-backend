package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/scheduler"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
)

const msgShiftClosed = "Shift udah selesai, buka lagi dulu kalau mau ngubah penugasan"

type ShiftUsecase interface {
	// Drivers
	GetDrivers(ctx context.Context, q *dto.QueryGlobal) ([]dto.Driver, int, error)
	LastAssignments(ctx context.Context) (map[string]dto.DriverLast, error)
	CreateDriver(ctx context.Context, body dto.UpsertDriverRequest, userID string) (*dto.Driver, error)
	UpdateDriver(ctx context.Context, id string, body dto.UpsertDriverRequest, userID string) (*dto.Driver, error)
	DeleteDriver(ctx context.Context, id, userID string) (*dto.DeleteResult, error)

	// Shift templates
	GetTemplates(ctx context.Context) ([]dto.ShiftTemplate, error)
	CreateTemplate(ctx context.Context, body dto.UpsertShiftTemplateRequest, userID string) (*dto.ShiftTemplate, error)
	UpdateTemplate(ctx context.Context, id string, body dto.UpsertShiftTemplateRequest, userID string) (*dto.ShiftTemplate, error)
	DeleteTemplate(ctx context.Context, id, userID string) error

	// Shifts
	GetShifts(ctx context.Context, q dto.ShiftListQuery) ([]dto.Shift, error)
	GetShift(ctx context.Context, id string) (*dto.ShiftDetail, error)
	CreateShift(ctx context.Context, body dto.CreateShiftRequest, userID string) (*dto.Shift, error)
	UpdateShift(ctx context.Context, id string, body dto.UpdateShiftRequest, userID string) (*dto.Shift, error)
	DeleteShift(ctx context.Context, id, userID string) error
	StartShift(ctx context.Context, id string) (*dto.Shift, error)
	CloseShift(ctx context.Context, id, userID string) (*dto.Shift, error)
	ReopenShift(ctx context.Context, id string) (*dto.Shift, error)

	// Assignments
	AddAssignment(ctx context.Context, shiftID string, body dto.AssignmentRequest) (*dto.Assignment, error)
	CopyPrevious(ctx context.Context, shiftID string) (*dto.CopyPreviousResponse, error)
	UpdateAssignment(ctx context.Context, id string, body dto.AssignmentRequest) (*dto.Assignment, error)
	EndAssignment(ctx context.Context, id string) (*dto.DeleteResult, error)

	// Reports
	Report(ctx context.Context, q dto.ReportQuery) (*dto.Report, error)
	ReportTrips(ctx context.Context, q dto.ReportQuery) ([]dto.TripRecord, int, error)
	ReportVisits(ctx context.Context, q dto.ReportQuery) (*dto.VisitReport, error)
	ReportVisitList(ctx context.Context, q dto.ReportQuery) ([]dto.SiteVisitRecord, int, error)
	// WithVisits enables ReportVisits (time at loading/unloading sites).
	WithVisits(v repository.VisitRepository) ShiftUsecase
}

type shiftUsecase struct {
	visitRepo repository.VisitRepository
	shiftRepo  repository.ShiftRepository
	driverRepo repository.DriverRepository
	tripRepo   repository.TripRepository
	scheduler  *scheduler.Scheduler
	tracker    *tracker.Tracker
	report     *config.ReportConfig
}

func NewShiftUsecase(shiftRepo repository.ShiftRepository, driverRepo repository.DriverRepository, tripRepo repository.TripRepository,
	s *scheduler.Scheduler, t *tracker.Tracker, reportCfg *config.ReportConfig) ShiftUsecase {
	return &shiftUsecase{shiftRepo: shiftRepo, driverRepo: driverRepo, tripRepo: tripRepo, scheduler: s, tracker: t, report: reportCfg}
}

// changed tells dashboards shifts changed and recomputes that shift's ritase.
func (u *shiftUsecase) changed(shiftID string) {
	u.tracker.Changed("shifts")
	if shiftID != "" {
		u.scheduler.RecomputeShift(shiftID)
	}
}

// ---------- Drivers ----------

func (u *shiftUsecase) GetDrivers(ctx context.Context, q *dto.QueryGlobal) ([]dto.Driver, int, error) {
	return u.driverRepo.GetDrivers(ctx, q)
}

func (u *shiftUsecase) LastAssignments(ctx context.Context) (map[string]dto.DriverLast, error) {
	return u.driverRepo.LastAssignments(ctx)
}

func (u *shiftUsecase) CreateDriver(ctx context.Context, body dto.UpsertDriverRequest, userID string) (*dto.Driver, error) {
	if strings.TrimSpace(body.Name) == "" {
		return nil, apperror.BadRequest("Nama operator wajib diisi")
	}
	id, err := u.driverRepo.CreateDriver(ctx, body, userID)
	if err != nil {
		return nil, err
	}
	u.changed("")
	return u.driverRepo.GetDriverByID(ctx, id)
}

func (u *shiftUsecase) UpdateDriver(ctx context.Context, id string, body dto.UpsertDriverRequest, userID string) (*dto.Driver, error) {
	if strings.TrimSpace(body.Name) == "" {
		return nil, apperror.BadRequest("Nama operator wajib diisi")
	}
	ok, err := u.driverRepo.UpdateDriver(ctx, id, body, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("Operator nggak ada")
	}
	u.changed("")
	return u.driverRepo.GetDriverByID(ctx, id)
}

// DeleteDriver: a driver who was ever assigned stays (old reports need the name), only deactivated.
func (u *shiftUsecase) DeleteDriver(ctx context.Context, id, userID string) (*dto.DeleteResult, error) {
	used, err := u.driverRepo.DriverEverAssigned(ctx, id)
	if err != nil {
		return nil, err
	}
	result := "deleted"
	var ok bool
	if used {
		result = "deactivated"
		ok, err = u.driverRepo.DeactivateDriver(ctx, id, userID)
	} else {
		ok, err = u.driverRepo.DeleteDriver(ctx, id, userID)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("Operator nggak ada")
	}
	u.changed("")
	return &dto.DeleteResult{Result: result}, nil
}

// ---------- Shift templates ----------

func (u *shiftUsecase) GetTemplates(ctx context.Context) ([]dto.ShiftTemplate, error) {
	return u.driverRepo.GetTemplates(ctx)
}

func (u *shiftUsecase) CreateTemplate(ctx context.Context, body dto.UpsertShiftTemplateRequest, userID string) (*dto.ShiftTemplate, error) {
	id, err := u.driverRepo.CreateTemplate(ctx, body, userID)
	if err != nil {
		return nil, err
	}
	u.changed("")
	return u.driverRepo.GetTemplateByID(ctx, id)
}

func (u *shiftUsecase) UpdateTemplate(ctx context.Context, id string, body dto.UpsertShiftTemplateRequest, userID string) (*dto.ShiftTemplate, error) {
	ok, err := u.driverRepo.UpdateTemplate(ctx, id, body, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("Template nggak ada")
	}
	u.changed("")
	return u.driverRepo.GetTemplateByID(ctx, id)
}

func (u *shiftUsecase) DeleteTemplate(ctx context.Context, id, userID string) error {
	ok, err := u.driverRepo.DeleteTemplate(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Template nggak ada")
	}
	u.changed("")
	return nil
}

// ---------- Shifts ----------

func isDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// GetShifts: ?from=YYYY-MM-DD&to=YYYY-MM-DD (work dates). Active shifts are always included.
func (u *shiftUsecase) GetShifts(ctx context.Context, q dto.ShiftListQuery) ([]dto.Shift, error) {
	from := q.From
	if !isDate(from) {
		from = time.Now().In(u.report.Location).Format("2006-01-02")
	}
	to := q.To
	if !isDate(to) {
		to = from
	}
	return u.shiftRepo.ListShifts(ctx, from, to)
}

func (u *shiftUsecase) mustShift(ctx context.Context, id string) (*dto.Shift, error) {
	s, err := u.shiftRepo.GetShift(ctx, id)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, apperror.NotFound("Shift nggak ada")
	}
	return s, nil
}

func (u *shiftUsecase) GetShift(ctx context.Context, id string) (*dto.ShiftDetail, error) {
	s, err := u.mustShift(ctx, id)
	if err != nil {
		return nil, err
	}
	trips, _, err := u.tripRepo.ListTrips(ctx, dto.TripFilter{From: 0, To: time.Now().Add(365 * 24 * time.Hour).UnixMilli(), ShiftID: s.ID}, 2000, 0)
	if err != nil {
		return nil, err
	}
	return &dto.ShiftDetail{Shift: *s, Trips: trips}, nil
}

func checkPlan(start, end int64) error {
	if end <= start {
		return apperror.BadRequest("Jam selesai harus setelah jam mulai")
	}
	if end-start > dayMs {
		return apperror.BadRequest("Satu shift maksimal 24 jam")
	}
	return nil
}

func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func (u *shiftUsecase) CreateShift(ctx context.Context, body dto.CreateShiftRequest, userID string) (*dto.Shift, error) {
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return nil, apperror.BadRequest("Nama shift wajib diisi")
	}
	if err := checkPlan(body.PlannedStart, body.PlannedEnd); err != nil {
		return nil, err
	}
	routeID := body.RouteID
	in := dto.ShiftInput{
		Name: name, RouteID: &routeID, Date: body.Date, PlannedStart: body.PlannedStart, PlannedEnd: body.PlannedEnd,
		AutoClose: body.AutoClose == nil || *body.AutoClose, Note: trimmed(body.Note),
	}
	id, err := u.shiftRepo.CreateShift(ctx, in, userID)
	if err != nil {
		return nil, err
	}
	u.tracker.Log("conn", "", "Shift "+name+" ("+body.Date+") dibuat", "info")
	u.changed("")
	return u.mustShift(ctx, id)
}

func (u *shiftUsecase) UpdateShift(ctx context.Context, id string, body dto.UpdateShiftRequest, userID string) (*dto.Shift, error) {
	cur, err := u.mustShift(ctx, id)
	if err != nil {
		return nil, err
	}
	n := dto.ShiftInput{
		Name: cur.Name, RouteID: cur.RouteID, Date: cur.Date, PlannedStart: cur.PlannedStart, PlannedEnd: cur.PlannedEnd,
		AutoClose: cur.AutoClose, Note: cur.Note,
	}
	if body.Name != nil {
		if n.Name = strings.TrimSpace(*body.Name); n.Name == "" {
			return nil, apperror.BadRequest("Nama shift wajib diisi")
		}
	}
	if body.Date != nil {
		n.Date = *body.Date
	}
	if body.PlannedStart != nil || body.PlannedEnd != nil {
		if body.PlannedStart == nil || body.PlannedEnd == nil {
			return nil, apperror.BadRequest("Jam mulai/selesai wajib diisi")
		}
		if err := checkPlan(*body.PlannedStart, *body.PlannedEnd); err != nil {
			return nil, err
		}
		n.PlannedStart, n.PlannedEnd = *body.PlannedStart, *body.PlannedEnd
	}
	if body.AutoClose != nil {
		n.AutoClose = *body.AutoClose
	}
	if body.Note != nil {
		n.Note = trimmed(body.Note)
	}
	routeChanged := false
	if body.RouteID != nil && (cur.RouteID == nil || *body.RouteID != *cur.RouteID) {
		// The route can only change before the shift starts (running ritase stick to their route).
		if cur.Status != "planned" {
			return nil, apperror.Conflict("Rute nggak bisa diganti setelah shift mulai")
		}
		n.RouteID = body.RouteID
		routeChanged = true
	}
	if err := u.shiftRepo.UpdateShift(ctx, id, n, routeChanged, userID); err != nil {
		return nil, err
	}
	s, err := u.mustShift(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.Status == "planned" {
		u.changed("")
	} else {
		u.changed(s.ID)
	}
	return s, nil
}

func (u *shiftUsecase) DeleteShift(ctx context.Context, id, userID string) error {
	ok, err := u.shiftRepo.DeleteShift(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.Conflict("Cuma shift yang belum mulai yang bisa dihapus")
	}
	u.changed("")
	return nil
}

func (u *shiftUsecase) StartShift(ctx context.Context, id string) (*dto.Shift, error) {
	ok, err := u.shiftRepo.StartShift(ctx, id, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.Conflict("Shift udah jalan atau udah selesai")
	}
	u.tracker.Log("conn", "", "Shift dimulai manual", "info")
	u.changed(id)
	return u.mustShift(ctx, id)
}

func (u *shiftUsecase) CloseShift(ctx context.Context, id, userID string) (*dto.Shift, error) {
	ok, err := u.shiftRepo.CloseShift(ctx, id, "manual", &userID, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.Conflict("Shift belum mulai atau udah selesai")
	}
	u.tracker.Log("conn", "", "Shift ditutup manual", "info")
	u.changed(id)
	return u.mustShift(ctx, id)
}

func (u *shiftUsecase) ReopenShift(ctx context.Context, id string) (*dto.Shift, error) {
	ok, err := u.shiftRepo.ReopenShift(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.Conflict("Shift belum selesai")
	}
	u.changed(id)
	return u.mustShift(ctx, id)
}

// ---------- Assignments ----------

// check: in one shift a driver/truck may not have 2 open assignments; across shifts,
// not while still on duty in another shift whose hours overlap.
func (u *shiftUsecase) check(ctx context.Context, shift *dto.Shift, a dto.AssignmentRequest, exceptID string) error {
	if shift.Status == "closed" {
		return apperror.Conflict(msgShiftClosed)
	}
	for _, x := range shift.Assignments {
		if x.EndAt != nil || x.ID == exceptID {
			continue
		}
		if x.DriverID == a.DriverID {
			return apperror.Conflict("Operator ini udah dapat tugas di shift ini")
		}
	}
	for _, x := range shift.Assignments {
		if x.EndAt != nil || x.ID == exceptID {
			continue
		}
		if x.Imei == a.Imei {
			return apperror.Conflict("Unit ini udah dipakai operator lain di shift ini")
		}
	}
	end := shift.PlannedEnd
	if shift.Status == "active" {
		end = max(end, time.Now().UnixMilli())
	}
	from := shift.PlannedStart
	if shift.StartedAt != nil {
		from = *shift.StartedAt
	}
	clash, err := u.shiftRepo.ClashOtherShift(ctx, shift.ID, a.DriverID, a.Imei, from, end)
	if err != nil {
		return err
	}
	if clash != "" {
		return apperror.Conflict(clash)
	}
	return nil
}

func (u *shiftUsecase) addAssignment(ctx context.Context, shift *dto.Shift, a dto.AssignmentRequest) (*dto.Assignment, error) {
	routeID := ""
	if shift.RouteID != nil {
		routeID = *shift.RouteID
	} else if a.RouteID != nil {
		routeID = *a.RouteID
	}
	if routeID == "" {
		return nil, apperror.BadRequest("Shift ini belum punya rute")
	}
	if err := u.check(ctx, shift, a, ""); err != nil {
		return nil, err
	}
	// Running shift: valid from the shift start if this driver & truck were not used in it yet
	// (e.g. forgotten at the briefing); if they were (a swap), valid from now.
	var startAt *int64
	if shift.Status == "active" {
		for _, x := range shift.Assignments {
			if x.DriverID == a.DriverID || x.Imei == a.Imei {
				now := time.Now().UnixMilli()
				startAt = &now
				break
			}
		}
	}
	return u.shiftRepo.InsertAssignment(ctx, shift.ID, a.DriverID, a.Imei, routeID, trimmed(a.Note), startAt)
}

func (u *shiftUsecase) AddAssignment(ctx context.Context, shiftID string, body dto.AssignmentRequest) (*dto.Assignment, error) {
	shift, err := u.mustShift(ctx, shiftID)
	if err != nil {
		return nil, err
	}
	a, err := u.addAssignment(ctx, shift, body)
	if err != nil {
		return nil, err
	}
	u.changed(shiftID)
	return a, nil
}

// CopyPrevious copies the last assignment of each driver from the previous shift on the
// same route (its state when that shift ended). Clashing ones are skipped.
func (u *shiftUsecase) CopyPrevious(ctx context.Context, shiftID string) (*dto.CopyPreviousResponse, error) {
	prev, err := u.shiftRepo.PreviousShift(ctx, shiftID)
	if err != nil {
		return nil, err
	}
	if prev == nil {
		return nil, apperror.NotFound("Belum ada shift sebelumnya di rute ini yang punya penugasan")
	}
	lastOf := map[string]dto.Assignment{}
	order := []string{}
	for _, a := range prev.Assignments {
		if _, ok := lastOf[a.DriverID]; !ok {
			order = append(order, a.DriverID)
		}
		lastOf[a.DriverID] = a
	}
	out := &dto.CopyPreviousResponse{From: prev.Name + " " + prev.Date, Skipped: []string{}}
	for _, driverID := range order {
		a := lastOf[driverID]
		shift, err := u.mustShift(ctx, shiftID) // re-read: earlier copies change the clash check
		if err != nil {
			return nil, err
		}
		_, err = u.addAssignment(ctx, shift, dto.AssignmentRequest{DriverID: a.DriverID, Imei: a.Imei, Note: a.Note})
		var ae *apperror.AppError
		switch {
		case err == nil:
			out.Added++
		case errors.As(err, &ae):
			out.Skipped = append(out.Skipped, ae.Message)
		default:
			return nil, err
		}
	}
	u.changed(shiftID)
	return out, nil
}

// UpdateAssignment: a planned shift is overwritten. On a running shift, a change of
// driver/truck/route ends the old assignment now and opens a new one, so earlier ritase
// stay on the old route/truck.
func (u *shiftUsecase) UpdateAssignment(ctx context.Context, id string, body dto.AssignmentRequest) (*dto.Assignment, error) {
	old, err := u.shiftRepo.GetAssignment(ctx, id)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, apperror.NotFound("Penugasan nggak ada")
	}
	if old.EndAt != nil {
		return nil, apperror.Conflict("Penugasan ini udah selesai")
	}
	shift, err := u.mustShift(ctx, old.ShiftID)
	if err != nil {
		return nil, err
	}
	routeID := old.RouteID
	if shift.RouteID != nil {
		routeID = *shift.RouteID
	} else if body.RouteID != nil {
		routeID = *body.RouteID
	}
	if err := u.check(ctx, shift, body, id); err != nil {
		return nil, err
	}
	moved := body.DriverID != old.DriverID || body.Imei != old.Imei || routeID != old.RouteID
	var a *dto.Assignment
	if shift.Status == "active" && moved {
		now := time.Now().UnixMilli()
		if err := u.shiftRepo.EndAssignment(ctx, id, now); err != nil {
			return nil, err
		}
		a, err = u.shiftRepo.InsertAssignment(ctx, shift.ID, body.DriverID, body.Imei, routeID, trimmed(body.Note), &now)
	} else {
		a, err = u.shiftRepo.UpdateAssignment(ctx, id, body.DriverID, body.Imei, routeID, trimmed(body.Note))
	}
	if err != nil {
		return nil, err
	}
	u.changed(a.ShiftID)
	return a, nil
}

// EndAssignment releases a driver: no ritase yet = deleted; otherwise ended now (ritase stay).
func (u *shiftUsecase) EndAssignment(ctx context.Context, id string) (*dto.DeleteResult, error) {
	st, err := u.shiftRepo.AssignmentState(ctx, id)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, apperror.NotFound("Penugasan nggak ada")
	}
	if st.Status == "closed" {
		return nil, apperror.Conflict(msgShiftClosed)
	}
	if st.Status == "active" && st.Trips > 0 {
		if st.EndAt == nil {
			if err := u.shiftRepo.EndAssignment(ctx, id, time.Now().UnixMilli()); err != nil {
				return nil, err
			}
		}
		u.changed(st.ShiftID)
		u.tracker.Changed("trips")
		return &dto.DeleteResult{Result: "ended"}, nil
	}
	if err := u.shiftRepo.DeleteAssignment(ctx, id); err != nil {
		return nil, err
	}
	u.changed("")
	u.tracker.Changed("trips")
	return &dto.DeleteResult{Result: "deleted"}, nil
}

// ---------- Reports ----------

// filterOf: ?from=&to= (ms), default today (report time zone).
func (u *shiftUsecase) filterOf(q dto.ReportQuery) dto.TripFilter {
	to := q.To
	if to <= 0 {
		to = time.Now().UnixMilli()
	}
	from := q.From
	if from <= 0 {
		t := time.UnixMilli(to).In(u.report.Location)
		from = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, u.report.Location).UnixMilli()
	}
	return dto.TripFilter{From: from, To: to, RouteID: q.Route, DriverID: q.Driver, Imei: q.Imei, ShiftID: q.Shift}
}

func (u *shiftUsecase) Report(ctx context.Context, q dto.ReportQuery) (*dto.Report, error) {
	group := q.Group
	if group == "" {
		group = "route"
	}
	return u.tripRepo.Report(ctx, group, u.filterOf(q), u.report.TimeZone)
}

func (u *shiftUsecase) ReportTrips(ctx context.Context, q dto.ReportQuery) ([]dto.TripRecord, int, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	return u.tripRepo.ListTrips(ctx, u.filterOf(q), min(limit, 2000), q.Offset)
}

// WithVisits enables GET /reports/visits (time at loading/unloading sites).
func (u *shiftUsecase) WithVisits(v repository.VisitRepository) ShiftUsecase {
	u.visitRepo = v
	return u
}

func (u *shiftUsecase) ReportVisits(ctx context.Context, q dto.ReportQuery) (*dto.VisitReport, error) {
	if u.visitRepo == nil {
		return &dto.VisitReport{Sites: []dto.SiteDwellRow{}, Current: []dto.SiteStay{}}, nil
	}
	return u.visitRepo.Report(ctx, u.filterOf(q), time.Now().UnixMilli())
}

func (u *shiftUsecase) ReportVisitList(ctx context.Context, q dto.ReportQuery) ([]dto.SiteVisitRecord, int, error) {
	if u.visitRepo == nil {
		return []dto.SiteVisitRecord{}, 0, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	return u.visitRepo.ListVisits(ctx, u.filterOf(q), q.Role, min(limit, 2000), max(q.Offset, 0), time.Now().UnixMilli())
}
