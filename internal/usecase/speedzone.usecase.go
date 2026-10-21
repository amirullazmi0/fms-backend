package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
)

// Zona batas kecepatan & overspeed per operator (P3-3).
const (
	maxOverspeedDays   = 31
	maxOverspeedPoints = 50000
	// overspeedGapMs: records further apart than this start a new episode.
	overspeedGapMs = 60_000
)

type SpeedZoneUsecase interface {
	List(ctx context.Context) ([]dto.SpeedZone, error)
	Create(ctx context.Context, b dto.UpsertSpeedZoneRequest, userID string) error
	Update(ctx context.Context, id string, b dto.UpsertSpeedZoneRequest, userID string) error
	Delete(ctx context.Context, id, userID string) error
	Report(ctx context.Context, q dto.ReportQuery) (*dto.OverspeedReport, error)
}

type speedZoneUsecase struct {
	repo    repository.SpeedZoneRepository
	tracker *tracker.Tracker
	report  *config.ReportConfig
}

func NewSpeedZoneUsecase(repo repository.SpeedZoneRepository, t *tracker.Tracker, reportCfg *config.ReportConfig) SpeedZoneUsecase {
	return &speedZoneUsecase{repo: repo, tracker: t, report: reportCfg}
}

func (u *speedZoneUsecase) List(ctx context.Context) ([]dto.SpeedZone, error) {
	return u.repo.List(ctx, false)
}

func (u *speedZoneUsecase) Create(ctx context.Context, b dto.UpsertSpeedZoneRequest, userID string) error {
	if _, err := u.repo.Create(ctx, b, userID); err != nil {
		return err
	}
	u.tracker.Changed("speed_zones")
	return nil
}

func (u *speedZoneUsecase) Update(ctx context.Context, id string, b dto.UpsertSpeedZoneRequest, userID string) error {
	ok, err := u.repo.Update(ctx, id, b, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Zona tidak ditemukan")
	}
	u.tracker.Changed("speed_zones")
	return nil
}

func (u *speedZoneUsecase) Delete(ctx context.Context, id, userID string) error {
	ok, err := u.repo.Delete(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Zona tidak ditemukan")
	}
	u.tracker.Changed("speed_zones")
	return nil
}

func (u *speedZoneUsecase) Report(ctx context.Context, q dto.ReportQuery) (*dto.OverspeedReport, error) {
	to := q.To
	if to <= 0 {
		to = time.Now().UnixMilli()
	}
	from := q.From
	if from <= 0 {
		t := time.UnixMilli(to).In(u.report.Location)
		from = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, u.report.Location).UnixMilli()
	}
	if to-from > maxOverspeedDays*24*hourMs {
		return nil, apperror.BadRequest("Periode maksimal 31 hari")
	}
	points, err := u.repo.OverspeedPoints(ctx, from, to, maxOverspeedPoints)
	if err != nil {
		return nil, err
	}
	episodes := overspeedEpisodes(points)
	for i := range episodes {
		episodes[i].Unit, _, _ = u.tracker.UnitInfo(episodes[i].Imei)
	}
	sort.Slice(episodes, func(i, j int) bool { return episodes[i].StartAt > episodes[j].StartAt })
	return &dto.OverspeedReport{From: from, To: to, Episodes: episodes, Drivers: overspeedDrivers(episodes),
		Truncated: len(points) >= maxOverspeedPoints}, nil
}

// overspeedEpisodes groups points (ordered by unit, zone, time) into episodes.
func overspeedEpisodes(points []dto.OverspeedPoint) []dto.Overspeed {
	out := []dto.Overspeed{}
	for _, p := range points {
		n := len(out)
		if n > 0 {
			e := &out[n-1]
			if e.Imei == p.Imei && e.ZoneID == p.ZoneID && p.Time-e.EndAt <= overspeedGapMs {
				e.EndAt, e.Points = p.Time, e.Points+1
				e.MaxSpeed = max(e.MaxSpeed, p.Speed)
				if e.DriverName == nil {
					e.DriverName = p.DriverName
				}
				continue
			}
		}
		out = append(out, dto.Overspeed{Imei: p.Imei, DriverName: p.DriverName, ZoneID: p.ZoneID, ZoneName: p.ZoneName,
			LimitKmh: p.LimitKmh, MaxSpeed: p.Speed, StartAt: p.Time, EndAt: p.Time, Points: 1})
	}
	return out
}

// overspeedDrivers: per operator, most episodes first. Episodes without operator = "Tanpa operator".
func overspeedDrivers(episodes []dto.Overspeed) []dto.OverspeedDriver {
	by := map[string]*dto.OverspeedDriver{}
	for _, e := range episodes {
		name := "Tanpa operator"
		if e.DriverName != nil {
			name = *e.DriverName
		}
		d := by[name]
		if d == nil {
			d = &dto.OverspeedDriver{DriverName: name}
			by[name] = d
		}
		d.Episodes++
		d.MaxOver = max(d.MaxOver, e.MaxSpeed-e.LimitKmh)
		d.TotalMs += e.EndAt - e.StartAt
	}
	out := make([]dto.OverspeedDriver, 0, len(by))
	for _, d := range by {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Episodes != out[j].Episodes {
			return out[i].Episodes > out[j].Episodes
		}
		return out[i].MaxOver > out[j].MaxOver
	})
	return out
}
