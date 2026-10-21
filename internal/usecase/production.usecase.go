package usecase

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
)

const (
	hourMs = 3600_000
	// finalAfterMs: an hour older than this is final even if a truck never sent data past it
	// (parked with the tracker off).
	finalAfterMs   = 12 * hourMs
	freshnessMs    = 12 * hourMs
	lateRecordMs   = 2 * 60_000
	maxHourlyHours = 24 * 31
)

type ProductionUsecase interface {
	GetTargets(ctx context.Context) ([]dto.ProductionTarget, error)
	CreateTarget(ctx context.Context, body dto.UpsertTargetRequest, userID string) (*dto.ProductionTarget, error)
	UpdateTarget(ctx context.Context, id string, body dto.UpsertTargetRequest, userID string) (*dto.ProductionTarget, error)
	DeleteTarget(ctx context.Context, id, userID string) error

	Hourly(ctx context.Context, q dto.ReportQuery) (*dto.HourlyReport, error)
	Freshness(ctx context.Context) ([]dto.Freshness, error)
	Stages(ctx context.Context, q dto.ReportQuery) (*dto.StageReport, error)
	Availability(ctx context.Context, q dto.AvailabilityQuery) (*dto.AvailabilityReport, error)
	Heatmap(ctx context.Context, q dto.HeatmapQuery) (*dto.HeatmapReport, error)
}

type productionUsecase struct {
	repo     repository.ProductionRepository
	tripRepo repository.TripRepository
	tracker  *tracker.Tracker
	report   *config.ReportConfig
}

func NewProductionUsecase(repo repository.ProductionRepository, tripRepo repository.TripRepository, t *tracker.Tracker, reportCfg *config.ReportConfig) ProductionUsecase {
	return &productionUsecase{repo: repo, tripRepo: tripRepo, tracker: t, report: reportCfg}
}

// ---------- Targets ----------

func (u *productionUsecase) GetTargets(ctx context.Context) ([]dto.ProductionTarget, error) {
	return u.repo.GetTargets(ctx)
}

func (u *productionUsecase) targetByID(ctx context.Context, id string) (*dto.ProductionTarget, error) {
	list, err := u.repo.GetTargets(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, apperror.NotFound("Target tidak ditemukan")
}

func (u *productionUsecase) CreateTarget(ctx context.Context, body dto.UpsertTargetRequest, userID string) (*dto.ProductionTarget, error) {
	id, err := u.repo.CreateTarget(ctx, body, userID)
	if err != nil {
		return nil, err
	}
	u.tracker.Changed("trips")
	return u.targetByID(ctx, id)
}

func (u *productionUsecase) UpdateTarget(ctx context.Context, id string, body dto.UpsertTargetRequest, userID string) (*dto.ProductionTarget, error) {
	ok, err := u.repo.UpdateTarget(ctx, id, body, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("Target tidak ditemukan")
	}
	u.tracker.Changed("trips")
	return u.targetByID(ctx, id)
}

func (u *productionUsecase) DeleteTarget(ctx context.Context, id, userID string) error {
	ok, err := u.repo.DeleteTarget(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Target tidak ditemukan")
	}
	u.tracker.Changed("trips")
	return nil
}

// targetOn: the route's target valid on date (latest valid_from <= date).
func targetOn(targets []dto.ProductionTarget, routeID, date string) *dto.ProductionTarget {
	var best *dto.ProductionTarget
	for i := range targets {
		t := &targets[i]
		if t.RouteID == routeID && t.ValidFrom <= date && (best == nil || t.ValidFrom > best.ValidFrom) {
			best = t
		}
	}
	return best
}

// ---------- Reports ----------

func (u *productionUsecase) filterOf(q dto.ReportQuery) dto.TripFilter {
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

func round1(v float64) float64 { return math.Floor(v*10+0.5) / 10 }

// Hourly: ritase & tonase per hour (GPS time of leaving loading), target, achievement and
// whether the hour is final (every truck on duty sent data past the hour).
func (u *productionUsecase) Hourly(ctx context.Context, q dto.ReportQuery) (*dto.HourlyReport, error) {
	f := u.filterOf(q)
	now := time.Now().UnixMilli()
	rep, err := u.tripRepo.Report(ctx, "hour", f, u.report.TimeZone)
	if err != nil {
		return nil, err
	}
	byKey := map[string]dto.ReportRow{}
	for _, r := range rep.Rows {
		byKey[r.Key] = r
	}
	windows, err := u.repo.AssignmentWindows(ctx, f.From, f.To, f.RouteID, f.ShiftID)
	if err != nil {
		return nil, err
	}
	targets, err := u.repo.GetTargets(ctx)
	if err != nil {
		return nil, err
	}

	loc := u.report.Location
	start := time.UnixMilli(f.From).In(loc).Truncate(time.Hour)
	last := f.To
	if last > now {
		last = now
	}
	out := &dto.HourlyReport{From: f.From, To: f.To, Rows: []dto.HourlyRow{}}
	total := dto.HourlyRow{Key: "total", Label: "Total", Status: dto.HourFinal, Pending: []dto.PendingUnit{}}
	pendingAll := map[string]bool{}
	for h, n := start, 0; h.UnixMilli() <= last && n < maxHourlyHours; h, n = h.Add(time.Hour), n+1 {
		hs, he := h.UnixMilli(), h.Add(time.Hour).UnixMilli()
		row := dto.HourlyRow{Key: h.Format("2006-01-02 15"), Label: h.Format("15:04"), Date: h.Format("2006-01-02"),
			Start: hs, End: he, Pending: []dto.PendingUnit{}}
		if r, ok := byKey[row.Key]; ok {
			row.Rit, row.Ton = r.Done, round1(r.Ton)
		}

		units := map[string]bool{}
		routes := map[string]bool{}
		for _, w := range windows {
			end := now
			if w.End != nil {
				end = *w.End
			}
			if w.Start < he && end > hs {
				units[w.Imei] = true
				routes[w.RouteID] = true
			}
		}
		row.Units = len(units)
		for routeID := range routes {
			if t := targetOn(targets, routeID, row.Date); t != nil {
				if t.RitPerHour != nil {
					row.TargetRit += *t.RitPerHour
				}
				if t.TonPerHour != nil {
					row.TargetTon += *t.TonPerHour
				}
			}
		}
		switch {
		case row.TargetTon > 0:
			a := round1(row.Ton / row.TargetTon * 100)
			row.Achievement = &a
		case row.TargetRit > 0:
			a := round1(float64(row.Rit) / row.TargetRit * 100)
			row.Achievement = &a
		}

		switch {
		case he > now:
			row.Status = dto.HourRunning
		case now-he > finalAfterMs:
			row.Status = dto.HourFinal
		default:
			row.Status = dto.HourFinal
			for imei := range units {
				label, lastGps, _ := u.tracker.UnitInfo(imei)
				if lastGps == nil || *lastGps < he {
					row.Status = dto.HourProvisional
					row.Pending = append(row.Pending, dto.PendingUnit{Imei: imei, Truck: label, LastGpsAt: lastGps})
					if !pendingAll[imei] {
						pendingAll[imei] = true
						total.Pending = append(total.Pending, dto.PendingUnit{Imei: imei, Truck: label, LastGpsAt: lastGps})
					}
				}
			}
			sort.Slice(row.Pending, func(i, j int) bool { return row.Pending[i].Truck < row.Pending[j].Truck })
		}

		total.Rit += row.Rit
		total.Ton += row.Ton
		total.TargetRit += row.TargetRit
		total.TargetTon += row.TargetTon
		total.Units = max(total.Units, row.Units)
		if row.Status != dto.HourFinal {
			total.Status = dto.HourProvisional
		}
		out.Rows = append(out.Rows, row)
	}
	total.Ton = round1(total.Ton)
	switch {
	case total.TargetTon > 0:
		a := round1(total.Ton / total.TargetTon * 100)
		total.Achievement = &a
	case total.TargetRit > 0:
		a := round1(float64(total.Rit) / total.TargetRit * 100)
		total.Achievement = &a
	}
	out.Total = total
	return out, nil
}

// Freshness: per unit, the newest GPS time, when the server last heard from it and how late
// its data arrived over the last 12 hours.
func (u *productionUsecase) Freshness(ctx context.Context) ([]dto.Freshness, error) {
	delays, err := u.repo.Freshness(ctx, freshnessMs, lateRecordMs)
	if err != nil {
		return nil, err
	}
	out := []dto.Freshness{}
	for _, d := range u.tracker.Devices() {
		f := delays[d.Imei]
		f.Imei = d.Imei
		f.Truck = tracker.UnitLabel(d)
		if d.Last != nil {
			f.LastGpsAt = &d.Last.Time
		}
		f.LastReceivedAt = d.LastSeen
		f.AvgDelayMs, f.MaxDelayMs = math.Round(f.AvgDelayMs), math.Round(f.MaxDelayMs)
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Truck < out[j].Truck })
	return out, nil
}

func (u *productionUsecase) Stages(ctx context.Context, q dto.ReportQuery) (*dto.StageReport, error) {
	f := u.filterOf(q)
	rows, err := u.repo.Stages(ctx, f)
	if err != nil {
		return nil, err
	}
	return &dto.StageReport{From: f.From, To: f.To, Rows: rows}, nil
}

// ---------- PA & UA (P2-2) ----------

const maxAvailabilityDays = 400

type period struct {
	key        string
	start, end int64
}

// periods splits [from, to) into calendar days / months in the report time zone ("total" = one).
func (u *productionUsecase) periods(from, to int64, bucket string) []period {
	if bucket != "day" && bucket != "month" {
		return []period{{"", from, to}}
	}
	loc := u.report.Location
	t := time.UnixMilli(from).In(loc)
	cur := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	if bucket == "month" {
		cur = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
	}
	out := []period{}
	for cur.UnixMilli() < to {
		next, key := cur.AddDate(0, 0, 1), cur.Format("2006-01-02")
		if bucket == "month" {
			next, key = cur.AddDate(0, 1, 0), cur.Format("2006-01")
		}
		s, e := max(cur.UnixMilli(), from), min(next.UnixMilli(), to)
		if e > s {
			out = append(out, period{key, s, e})
		}
		cur = next
	}
	return out
}

func pct(num, den float64) *float64 {
	if den <= 0 {
		return nil
	}
	v := round1(num / den * 100)
	return &v
}

// finish fills NoStatusMs and the indicators from the time per category.
func finish(r *dto.AvailabilityRow) {
	used := r.WorkingMs + r.StandbyPlannedMs + r.StandbyUnplannedMs + r.BreakdownMs
	r.NoStatusMs = math.Max(0, r.TotalMs-used)
	r.PA = pct(r.TotalMs-r.BreakdownMs, r.TotalMs)
	r.UA = pct(r.WorkingMs, r.TotalMs-r.BreakdownMs)
	r.MA = pct(r.WorkingMs, r.WorkingMs+r.BreakdownMs)
	r.EU = pct(r.WorkingMs, r.TotalMs)
}

func addRow(dst, src *dto.AvailabilityRow) {
	dst.Units += src.Units
	dst.TotalMs += src.TotalMs
	dst.WorkingMs += src.WorkingMs
	dst.StandbyPlannedMs += src.StandbyPlannedMs
	dst.StandbyUnplannedMs += src.StandbyUnplannedMs
	dst.BreakdownMs += src.BreakdownMs
}

// Availability: PA, UA, MA, EU per unit, per unit type and for all units, from the unit status
// log (MDT / supervisor / auto). Calendar time is the base (24 jam per hari).
func (u *productionUsecase) Availability(ctx context.Context, q dto.AvailabilityQuery) (*dto.AvailabilityReport, error) {
	f := u.filterOf(dto.ReportQuery{From: q.From, To: q.To})
	now := time.Now().UnixMilli()
	if f.To > now {
		f.To = now
	}
	if f.To <= f.From {
		return nil, apperror.BadRequest("Periode tidak valid")
	}
	if f.To-f.From > maxAvailabilityDays*24*hourMs {
		return nil, apperror.BadRequest("Periode maksimal 400 hari")
	}
	bucket := q.Bucket
	if bucket == "" {
		bucket = "total"
	}
	units, err := u.repo.AvailabilityUnits(ctx)
	if err != nil {
		return nil, err
	}
	logs, err := u.repo.StatusIntervals(ctx, f.From, f.To)
	if err != nil {
		return nil, err
	}
	ps := u.periods(f.From, f.To, bucket)

	type key struct{ imei, period string }
	rows := map[key]*dto.AvailabilityRow{}
	order := []key{}
	for _, un := range units {
		if (q.Imei != "" && un.Imei != q.Imei) || (q.UnitType != "" && un.UnitType != q.UnitType) {
			continue
		}
		for _, p := range ps {
			k := key{un.Imei, p.key}
			rows[k] = &dto.AvailabilityRow{Key: un.Imei, Label: un.Label, UnitType: un.UnitType, Period: p.key, Units: 1, TotalMs: float64(p.end - p.start)}
			order = append(order, k)
		}
	}
	for _, l := range logs {
		for _, p := range ps {
			r := rows[key{l.Imei, p.key}]
			s, e := max(l.Start, p.start), min(l.End, p.end)
			if r == nil || e <= s {
				continue
			}
			d := float64(e - s)
			switch l.Category {
			case "working":
				r.WorkingMs += d
			case "standby_planned":
				r.StandbyPlannedMs += d
			case "breakdown":
				r.BreakdownMs += d
			default:
				r.StandbyUnplannedMs += d
			}
		}
	}

	rep := &dto.AvailabilityReport{From: f.From, To: f.To, Bucket: bucket,
		Units: []dto.AvailabilityRow{}, Types: []dto.AvailabilityRow{}, Total: []dto.AvailabilityRow{}}
	types := map[key]*dto.AvailabilityRow{}
	totals := map[string]*dto.AvailabilityRow{}
	typeOrder := []key{}
	for _, k := range order {
		r := rows[k]
		tk := key{r.UnitType, k.period}
		if types[tk] == nil {
			types[tk] = &dto.AvailabilityRow{Key: r.UnitType, Label: r.UnitType, UnitType: r.UnitType, Period: k.period}
			typeOrder = append(typeOrder, tk)
		}
		addRow(types[tk], r)
		if totals[k.period] == nil {
			totals[k.period] = &dto.AvailabilityRow{Key: "all", Label: "Semua unit", Period: k.period}
		}
		addRow(totals[k.period], r)
		finish(r)
		rep.Units = append(rep.Units, *r)
	}
	sort.SliceStable(typeOrder, func(i, j int) bool {
		if typeOrder[i].period != typeOrder[j].period {
			return typeOrder[i].period < typeOrder[j].period
		}
		return typeOrder[i].imei < typeOrder[j].imei
	})
	for _, tk := range typeOrder {
		finish(types[tk])
		rep.Types = append(rep.Types, *types[tk])
	}
	for _, p := range ps {
		if t := totals[p.key]; t != nil {
			finish(t)
			rep.Total = append(rep.Total, *t)
		}
	}
	return rep, nil
}
