package usecase

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/analytics"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
	"github.com/amirullazmi0/fms-backend/pkg/routing"
)

const dayMs = 24 * 3600_000

type SiteUsecase interface {
	GetSites(ctx context.Context, q *dto.QueryGlobal) ([]dto.Site, int, error)
	CreateSite(ctx context.Context, body dto.UpsertSiteRequest, userID string) (*dto.Site, error)
	UpdateSite(ctx context.Context, id string, body dto.UpsertSiteRequest, userID string) (*dto.Site, error)
	DeleteSite(ctx context.Context, id, userID string) error
	UpdateZones(ctx context.Context, id string, body dto.UpdateSiteZonesRequest, userID string) (*dto.Site, error)

	GetRoutes(ctx context.Context, q *dto.QueryGlobal) ([]dto.Route, int, error)
	CreateRoute(ctx context.Context, body dto.UpsertRouteRequest, userID string) (*dto.Route, error)
	UpdateRoute(ctx context.Context, id string, body dto.UpsertRouteRequest, userID string) (*dto.Route, error)
	DeleteRoute(ctx context.Context, id, userID string) error
	RouteHistory(ctx context.Context, id string, days int) (*dto.RouteHistory, error)
	Road(ctx context.Context, fromID, toID string) (*dto.RoadRoute, error)
}

type siteUsecase struct {
	siteRepo   repository.SiteRepository
	deviceRepo repository.DeviceRepository
	router     *routing.Router
	tracker    *tracker.Tracker
}

func NewSiteUsecase(siteRepo repository.SiteRepository, deviceRepo repository.DeviceRepository, router *routing.Router, t *tracker.Tracker) SiteUsecase {
	return &siteUsecase{siteRepo: siteRepo, deviceRepo: deviceRepo, router: router, tracker: t}
}

func (u *siteUsecase) GetSites(ctx context.Context, q *dto.QueryGlobal) ([]dto.Site, int, error) {
	return u.siteRepo.GetSites(ctx, q)
}

func (u *siteUsecase) siteByID(ctx context.Context, id string) (*dto.Site, error) {
	list, err := u.siteRepo.SitesByIDs(ctx, []string{id}, false)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func (u *siteUsecase) CreateSite(ctx context.Context, body dto.UpsertSiteRequest, userID string) (*dto.Site, error) {
	if strings.TrimSpace(body.Name) == "" {
		return nil, apperror.BadRequest("Nama titik wajib diisi")
	}
	id, err := u.siteRepo.CreateSite(ctx, body, userID)
	if err != nil {
		return nil, err
	}
	site, err := u.siteByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.tracker.Log("conn", "", "Titik \""+site.Name+"\" dibuat", "info")
	u.tracker.Changed("sites")
	return site, nil
}

func (u *siteUsecase) UpdateSite(ctx context.Context, id string, body dto.UpsertSiteRequest, userID string) (*dto.Site, error) {
	if strings.TrimSpace(body.Name) == "" {
		return nil, apperror.BadRequest("Nama titik wajib diisi")
	}
	ok, err := u.siteRepo.UpdateSite(ctx, id, body, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("Titik tidak ditemukan")
	}
	site, err := u.siteByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.tracker.Log("conn", "", "Titik \""+site.Name+"\" diubah", "info")
	u.tracker.Changed("sites")
	return site, nil
}

// UpdateZones sets the queue / spot zones and queue alert thresholds of a site.
func (u *siteUsecase) UpdateZones(ctx context.Context, id string, body dto.UpdateSiteZonesRequest, userID string) (*dto.Site, error) {
	ok, err := u.siteRepo.ReplaceZones(ctx, id, body, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("Titik tidak ditemukan")
	}
	site, err := u.siteByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.tracker.Log("conn", "", "Zona antre/spot titik \""+site.Name+"\" diubah", "info")
	u.tracker.Changed("sites")
	return site, nil
}

func (u *siteUsecase) DeleteSite(ctx context.Context, id, userID string) error {
	used, err := u.siteRepo.RoutesUsingSite(ctx, id)
	if err != nil {
		return err
	}
	if len(used) > 0 {
		return apperror.Conflict("Titik masih dipakai rute: " + strings.Join(used, ", ") + ". Hapus/ubah rutenya terlebih dahulu.")
	}
	ok, err := u.siteRepo.DeleteSite(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Titik tidak ditemukan")
	}
	u.tracker.Changed("sites")
	return nil
}

func (u *siteUsecase) GetRoutes(ctx context.Context, q *dto.QueryGlobal) ([]dto.Route, int, error) {
	return u.siteRepo.GetRoutes(ctx, q)
}

// checkRouteSites resolves the loading site and the unloading sites (names joined) of a route body.
func (u *siteUsecase) checkRouteSites(ctx context.Context, body dto.UpsertRouteRequest) (loading *dto.Site, unloading string, err error) {
	if strings.TrimSpace(body.Name) == "" {
		return nil, "", apperror.BadRequest("Nama rute wajib diisi")
	}
	ids := body.Unloadings()
	if len(ids) == 0 {
		return nil, "", apperror.BadRequest("Pilih stockpile / jetty")
	}
	seen := map[string]bool{body.LoadingID: true}
	for _, id := range ids {
		if id == body.LoadingID {
			return nil, "", apperror.BadRequest("Pit dan stockpile / jetty harus beda")
		}
		if seen[id] {
			return nil, "", apperror.BadRequest("Stockpile / jetty yang sama dipilih dua kali")
		}
		seen[id] = true
	}
	found, err := u.siteRepo.SitesByIDs(ctx, append([]string{body.LoadingID}, ids...), false)
	if err != nil {
		return nil, "", err
	}
	byID := map[string]*dto.Site{}
	for i := range found {
		byID[found[i].ID] = &found[i]
	}
	loading = byID[body.LoadingID]
	if loading == nil {
		return nil, "", apperror.BadRequest("Pilih pit")
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		if byID[id] == nil {
			return nil, "", apperror.BadRequest("Pilih stockpile / jetty")
		}
		names[i] = byID[id].Name
	}
	return loading, strings.Join(names, " / "), nil
}

func (u *siteUsecase) CreateRoute(ctx context.Context, body dto.UpsertRouteRequest, userID string) (*dto.Route, error) {
	l, ul, err := u.checkRouteSites(ctx, body)
	if err != nil {
		return nil, err
	}
	id, err := u.siteRepo.CreateRoute(ctx, body, userID)
	if err != nil {
		return nil, err
	}
	route, err := u.siteRepo.GetRouteByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.tracker.Log("conn", "", "Rute \""+route.Name+"\" dibuat: "+l.Name+" → "+ul, "info")
	u.tracker.Changed("sites")
	return route, nil
}

func (u *siteUsecase) UpdateRoute(ctx context.Context, id string, body dto.UpsertRouteRequest, userID string) (*dto.Route, error) {
	l, ul, err := u.checkRouteSites(ctx, body)
	if err != nil {
		return nil, err
	}
	ok, err := u.siteRepo.UpdateRoute(ctx, id, body, userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("Rute tidak ditemukan")
	}
	route, err := u.siteRepo.GetRouteByID(ctx, id)
	if err != nil {
		return nil, err
	}
	u.tracker.Log("conn", "", "Rute \""+route.Name+"\" diubah: "+l.Name+" → "+ul, "info")
	u.tracker.Changed("sites")
	return route, nil
}

func (u *siteUsecase) DeleteRoute(ctx context.Context, id, userID string) error {
	used, err := u.siteRepo.RouteInUse(ctx, id)
	if err != nil {
		return err
	}
	if used {
		return apperror.Conflict("Rute ini sudah pernah dipakai di shift, sehingga tidak dapat dihapus (laporan membutuhkan rute ini)")
	}
	ok, err := u.siteRepo.DeleteRoute(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.NotFound("Rute tidak ditemukan")
	}
	u.tracker.Changed("sites")
	return nil
}

// RouteHistory: real path + estimates from the finished ritase of the trucks on this route.
func (u *siteUsecase) RouteHistory(ctx context.Context, id string, days int) (*dto.RouteHistory, error) {
	route, err := u.siteRepo.GetRouteByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if route == nil {
		return nil, apperror.NotFound("Rute tidak ditemukan")
	}
	if days <= 0 {
		days = 14
	}
	days = min(days, 60)
	to := time.Now().UnixMilli()
	from := to - int64(days)*dayMs
	sites, err := u.siteRepo.SitesByIDs(ctx, route.SiteIDs(), true)
	if err != nil {
		return nil, err
	}
	trucks, err := u.siteRepo.TrucksOnRoute(ctx, route.ID, from, to)
	if err != nil {
		return nil, err
	}
	perTruck := make([]analytics.TruckRecords, 0, len(trucks))
	for _, imei := range trucks {
		recs, err := u.deviceRepo.RecordsBetween(ctx, imei, from, to, 200000)
		if err != nil {
			return nil, err
		}
		perTruck = append(perTruck, analytics.TruckRecords{Imei: imei, Records: recs})
	}
	h := analytics.RouteHistory(*route, sites, perTruck, from, to, to, days)
	h.Paths = analytics.UsualPaths(*route, sites, perTruck, from, to, to)
	return &h, nil
}

func (u *siteUsecase) Road(ctx context.Context, fromID, toID string) (*dto.RoadRoute, error) {
	found, err := u.siteRepo.SitesByIDs(ctx, []string{fromID, toID}, true)
	if err != nil {
		return nil, err
	}
	var a, b *dto.Site
	for i := range found {
		if found[i].ID == fromID {
			a = &found[i]
		}
		if found[i].ID == toID {
			b = &found[i]
		}
	}
	if a == nil || b == nil {
		return nil, apperror.NotFound("Titik tidak ditemukan")
	}
	road, err := u.router.RoadRoute(ctx, routing.LatLng{Lat: a.Lat, Lng: a.Lng}, routing.LatLng{Lat: b.Lat, Lng: b.Lng})
	if err != nil {
		return nil, apperror.New(http.StatusBadGateway, err.Error())
	}
	return road, nil
}
