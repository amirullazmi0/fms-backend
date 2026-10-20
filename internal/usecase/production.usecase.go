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
	return nil, apperror.NotFound("Target nggak ada")
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
		return nil, apperror.NotFound("Target nggak ada")
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
		return apperror.NotFound("Target nggak ada")
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
