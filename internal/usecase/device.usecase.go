package usecase

import (
	"context"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/analytics"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
	"github.com/amirullazmi0/fms-backend/pkg/geocode"
)

type DeviceUsecase interface {
	RawRecords(ctx context.Context, imei string, q dto.RawRecordsQuery) ([]dto.AvlRecord, int, int, int, error)
	GetDevices(ctx context.Context) []dto.Device
	GetDevice(ctx context.Context, imei string, limit int) (*dto.DeviceDetail, error)
	UpdateDevice(ctx context.Context, imei string, req dto.UpdateDeviceRequest, userID string) (*dto.Device, error)
	DeleteDevice(ctx context.Context, imei, userID string) error
	Day(ctx context.Context, imei string, q dto.DayQuery) (*dto.DaySummary, error)
	Ritase(ctx context.Context, imei string, q dto.DayQuery) (*dto.RouteReport, error)
	Records(ctx context.Context, imei string, q dto.RecordsQuery) ([]dto.AvlRecord, error)
	SendCommand(ctx context.Context, imei, cmd string) (string, error)

	Events() []dto.FleetEvent
	Commands(ctx context.Context) ([]dto.CommandLine, error)
	Status() dto.ServerStatus
	Logs(limit int) []dto.LogEntry
	Place(lat, lng float64) *string
}

type deviceUsecase struct {
	deviceRepo repository.DeviceRepository
	eventRepo  repository.EventRepository
	siteRepo   repository.SiteRepository
	tracker    *tracker.Tracker
	geocoder   *geocode.Geocoder
	loc        *time.Location
}

func NewDeviceUsecase(deviceRepo repository.DeviceRepository, eventRepo repository.EventRepository, siteRepo repository.SiteRepository,
	t *tracker.Tracker, g *geocode.Geocoder, reportCfg *config.ReportConfig) DeviceUsecase {
	return &deviceUsecase{deviceRepo: deviceRepo, eventRepo: eventRepo, siteRepo: siteRepo, tracker: t, geocoder: g, loc: reportCfg.Location}
}

func (u *deviceUsecase) GetDevices(_ context.Context) []dto.Device {
	return u.tracker.Devices()
}

func (u *deviceUsecase) GetDevice(_ context.Context, imei string, limit int) (*dto.DeviceDetail, error) {
	if limit <= 0 {
		limit = 1000
	}
	d, ok := u.tracker.Device(imei, limit)
	if !ok {
		return nil, apperror.NotFound("Alat belum pernah terhubung")
	}
	return d, nil
}

func (u *deviceUsecase) UpdateDevice(ctx context.Context, imei string, req dto.UpdateDeviceRequest, userID string) (*dto.Device, error) {
	d, err := u.tracker.UpdateMeta(ctx, imei, req, userID)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (u *deviceUsecase) DeleteDevice(ctx context.Context, imei, userID string) error {
	ok, err := u.tracker.Delete(ctx, imei, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Alat tidak ditemukan")
	}
	return nil
}

// rangeOf: ?from=&to= (ms). Default: from today's midnight (report time zone) to now.
func (u *deviceUsecase) rangeOf(q dto.DayQuery) (int64, int64) {
	to := q.To
	if to <= 0 {
		to = time.Now().UnixMilli()
	}
	from := q.From
	if from <= 0 {
		t := time.UnixMilli(to).In(u.loc)
		from = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, u.loc).UnixMilli()
	}
	return from, to
}

func (u *deviceUsecase) Day(ctx context.Context, imei string, q dto.DayQuery) (*dto.DaySummary, error) {
	from, to := u.rangeOf(q)
	recs, err := u.deviceRepo.RecordsBetween(ctx, imei, from, to, 20000)
	if err != nil {
		return nil, err
	}
	day := analytics.SummarizeDay(recs, from, to)
	// Place names: stops and each trip's destination. Uncached ones come on a later request.
	for i := range day.Segments {
		day.Segments[i].Place = u.geocoder.PlaceName(day.Segments[i].To.Lat, day.Segments[i].To.Lng)
	}
	return &day, nil
}

// Ritase: loading -> unloading report per route, max 31 days. Default: today.
func (u *deviceUsecase) Ritase(ctx context.Context, imei string, q dto.DayQuery) (*dto.RouteReport, error) {
	from, to := u.rangeOf(q)
	from = max(from, to-31*dayMs)
	routes, err := u.siteRepo.RoutesForDevice(ctx, imei, from-dayMs, to)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, r := range routes {
		for _, id := range []string{r.LoadingID, r.UnloadingID} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sites, err := u.siteRepo.SitesByIDs(ctx, ids, true)
	if err != nil {
		return nil, err
	}
	// Records start a day early so a ritase that began yesterday is read whole.
	recs := []dto.AvlRecord{}
	if len(sites) > 0 {
		if recs, err = u.deviceRepo.RecordsBetween(ctx, imei, from-dayMs, to, 200000); err != nil {
			return nil, err
		}
	}
	rep := analytics.BuildReport(recs, routes, sites, from, to, time.Now().UnixMilli())
	return &rep, nil
}

func (u *deviceUsecase) Records(ctx context.Context, imei string, q dto.RecordsQuery) ([]dto.AvlRecord, error) {
	to := q.To
	if to <= 0 {
		to = time.Now().UnixMilli()
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	return u.deviceRepo.RecordsBetween(ctx, imei, max(q.From, 0), to, min(limit, 5000))
}

// RawRecords: one page of stored records (newest first). Defaults to today.
func (u *deviceUsecase) RawRecords(ctx context.Context, imei string, q dto.RawRecordsQuery) ([]dto.AvlRecord, int, int, int, error) {
	to := q.To
	if to <= 0 {
		to = time.Now().UnixMilli()
	}
	from := q.From
	if from <= 0 {
		t := time.UnixMilli(to).In(u.loc)
		from = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, u.loc).UnixMilli()
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 500)
	page := max(q.Page, 1)
	recs, total, err := u.deviceRepo.RecordsPage(ctx, imei, from, to, limit, (page-1)*limit)
	return recs, total, page, limit, err
}

func (u *deviceUsecase) SendCommand(ctx context.Context, imei, cmd string) (string, error) {
	reply, err := u.tracker.SendCommand(ctx, imei, cmd)
	if err != nil {
		return "", apperror.Conflict(err.Error())
	}
	return reply, nil
}

func (u *deviceUsecase) Events() []dto.FleetEvent { return u.tracker.Events(200) }

func (u *deviceUsecase) Commands(ctx context.Context) ([]dto.CommandLine, error) {
	return u.eventRepo.LastCommands(ctx, 300)
}

func (u *deviceUsecase) Status() dto.ServerStatus { return u.tracker.Status() }

func (u *deviceUsecase) Logs(limit int) []dto.LogEntry { return u.tracker.Logs(limit) }

func (u *deviceUsecase) Place(lat, lng float64) *string { return u.geocoder.PlaceName(lat, lng) }
