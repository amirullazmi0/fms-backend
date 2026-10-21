package usecase

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/analytics"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
)

// Panel dispatcher (P2-5 … P2-7): excavator per fleet, live match factor, DT antre, hanging time
// excavator, DT tidak aktif + alasannya, dan saran memindahkan DT antar fleet.
const (
	mfLow          = 0.9
	mfHigh         = 1.1
	hangingWindow  = 60 * 60_000
	gpsStaleMs     = 15 * 60_000 // older GPS = unit counted as not producing (blank spot / mati)
	stillKmh       = 3
	recentStagesMs = 3 * hourMs
	fallbackDays   = 7
	// Without enough data: notes say 1 ritase 22–27 min, loading a DT ±3 min.
	defaultCycle = 25 * 60_000
	defaultLoad  = 3 * 60_000
	// loadersPerFleet: 1 fleet = 1 excavator (belum diisi pun dihitung 1 agar MF tetap terbaca).
	loadersPerFleet = 1
)

type DispatchUsecase interface {
	Board(ctx context.Context) (*dto.DispatchBoard, error)
	SetLoader(ctx context.Context, routeID string, body dto.SetLoaderRequest, userID string) error
}

type dispatchUsecase struct {
	repo    repository.DispatchRepository
	sites   repository.SiteRepository
	prod    repository.ProductionRepository
	mdt     repository.MdtRepository
	tracker *tracker.Tracker
}

func NewDispatchUsecase(repo repository.DispatchRepository, sites repository.SiteRepository, prod repository.ProductionRepository,
	mdt repository.MdtRepository, t *tracker.Tracker) DispatchUsecase {
	return &dispatchUsecase{repo: repo, sites: sites, prod: prod, mdt: mdt, tracker: t}
}

func (u *dispatchUsecase) SetLoader(ctx context.Context, routeID string, body dto.SetLoaderRequest, userID string) error {
	routes, _, err := u.sites.GetRoutes(ctx, &dto.QueryGlobal{})
	if err != nil {
		return err
	}
	found := false
	for _, r := range routes {
		found = found || r.ID == routeID
	}
	if !found {
		return apperror.NotFound("Rute tidak ditemukan")
	}
	if body.Imei != "" {
		known := false
		for _, d := range u.tracker.Devices() {
			known = known || d.Imei == body.Imei
		}
		if !known {
			return apperror.NotFound("Unit tidak ditemukan")
		}
	}
	if err := u.repo.SetLoader(ctx, routeID, body.Imei, body.ProductivityTph, userID); err != nil {
		return err
	}
	u.tracker.Changed("dispatch")
	return nil
}

// stagesBy: average stages per route over [now-window, now).
func (u *dispatchUsecase) stagesBy(ctx context.Context, now, window int64) (map[string]dto.StageRow, error) {
	rows, err := u.prod.Stages(ctx, dto.TripFilter{From: now - window, To: now})
	if err != nil {
		return nil, err
	}
	out := map[string]dto.StageRow{}
	for _, r := range rows {
		out[r.RouteID] = r
	}
	return out, nil
}

func loadMsOf(s dto.StageStats) float64 {
	v := s.AvgWorkMs
	if s.AvgSpotMs != nil {
		v += *s.AvgSpotMs
	}
	return v
}

func (u *dispatchUsecase) Board(ctx context.Context) (*dto.DispatchBoard, error) {
	now := time.Now().UnixMilli()
	routes, _, err := u.sites.GetRoutes(ctx, &dto.QueryGlobal{})
	if err != nil {
		return nil, err
	}
	loaders, err := u.repo.CurrentLoaders(ctx)
	if err != nil {
		return nil, err
	}
	recent, err := u.stagesBy(ctx, now, recentStagesMs)
	if err != nil {
		return nil, err
	}
	week, err := u.stagesBy(ctx, now, fallbackDays*24*hourMs)
	if err != nil {
		return nil, err
	}
	statuses, err := u.mdt.CurrentStatuses(ctx)
	if err != nil {
		return nil, err
	}
	siteIDs := []string{}
	for _, r := range routes {
		siteIDs = append(siteIDs, r.SiteIDs()...)
	}
	sites, err := u.sites.SitesByIDs(ctx, siteIDs, true)
	if err != nil {
		return nil, err
	}
	siteBy := map[string]dto.Site{}
	for _, s := range sites {
		siteBy[s.ID] = s
	}

	board := &dto.DispatchBoard{At: now, Fleets: []dto.Fleet{}, Moves: []dto.FleetMove{}}
	for _, r := range routes {
		f := dto.Fleet{RouteID: r.ID, RouteName: r.Name, LoadingName: siteBy[r.LoadingID].Name,
			UnloadingName: siteNames(siteBy, r.Unloadings()), Trucks: []dto.FleetTruck{}, HangingWindow: hangingWindow}
		if l, ok := loaders[r.ID]; ok {
			f.Loader = &l
		}
		front := siteBy[r.LoadingID]
		atFront := 0
		for _, d := range r.OnDuty {
			if d.Phase != "active" {
				continue
			}
			t := u.truckOf(d, front, statuses, now)
			if t.Inactive == nil {
				f.Active++
			}
			if t.AtFront {
				atFront++
			}
			f.Trucks = append(f.Trucks, t)
		}
		if atFront > 1 {
			f.Queue = atFront - 1 // one is being loaded
		}

		// Waktu muat & cycle time: 3 jam terakhir, kalau belum ada 7 hari, kalau belum ada perkiraan.
		f.LoadTimeMs, f.CycleMs, f.BasisWindow = defaultLoad, defaultCycle, "perkiraan"
		for _, src := range []struct {
			m     map[string]dto.StageRow
			label string
		}{{recent, "3 jam"}, {week, "7 hari"}} {
			if s, ok := src.m[r.ID]; ok && s.CycleMs > 0 && loadMsOf(s.Loading) > 0 {
				f.LoadTimeMs, f.CycleMs, f.BasisWindow = loadMsOf(s.Loading), s.CycleMs, src.label
				break
			}
		}
		// Without GPS loading data, the excavator's productivity gives the loading time.
		if f.BasisWindow == "perkiraan" && f.Loader != nil && f.Loader.ProductivityTph != nil {
			f.LoadTimeMs = tracker.DefaultPayloadTon / *f.Loader.ProductivityTph * hourMs
		}
		f.MatchFactor, f.Match, f.IdealTrucks = matchFactor(f.Active, loadersPerFleet, f.LoadTimeMs, f.CycleMs)

		if f.Loader != nil && len(f.Trucks) > 0 {
			stays, err := u.repo.SiteStays(ctx, r.LoadingID, now-hangingWindow, now)
			if err != nil {
				return nil, err
			}
			h := hangingWindow - coveredMs(stays, now-hangingWindow, now)
			f.HangingMs = &h
		}
		board.Fleets = append(board.Fleets, f)
	}
	board.Moves = suggestMoves(board.Fleets)
	return board, nil
}

// truckOf: what one unit on duty is doing now and why it does not produce (if so).
func (u *dispatchUsecase) truckOf(d dto.OnDuty, front dto.Site, statuses map[string]dto.UnitStatusLog, now int64) dto.FleetTruck {
	label, _, _ := u.tracker.UnitInfo(d.Imei)
	t := dto.FleetTruck{Imei: d.Imei, Label: label, DriverName: d.DriverName}
	if st, ok := statuses[d.Imei]; ok {
		if st.Category != nil {
			t.Category = *st.Category
		} else {
			t.Category = map[string]string{"working": "working", "breakdown": "breakdown"}[st.Status]
			if t.Category == "" {
				t.Category = "standby_unplanned"
			}
		}
		t.Status = st.DelayName
	}
	pos, speed, ok := u.tracker.Snapshot(d.Imei)
	if ok {
		age := now - pos.Time
		t.GpsAgeMs = &age
		t.AtFront = age <= gpsStaleMs && speed <= stillKmh && front.ID != "" &&
			analytics.DistanceM(pos.Lat, pos.Lng, front.Lat, front.Lng) <= float64(front.Radius)
	}
	reason := ""
	switch {
	case t.Category == "breakdown":
		reason = "Breakdown"
	case strings.HasPrefix(t.Category, "standby"):
		reason = "Standby"
	}
	if reason != "" && t.Status != nil {
		reason += " · " + *t.Status
	}
	if reason == "" && (t.GpsAgeMs == nil || *t.GpsAgeMs > gpsStaleMs) {
		if t.GpsAgeMs == nil {
			reason = "Belum ada data GPS"
		} else {
			reason = fmt.Sprintf("Tidak ada data GPS %d mnt (blank spot / alat mati)", *t.GpsAgeMs/60_000)
		}
	}
	if reason != "" {
		t.Inactive = &reason
	}
	return t
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// matchFactor: MF = (DT × waktu muat) ÷ (excavator × cycle time DT), its class, and the DT count for MF = 1.
func matchFactor(trucks, loaders int, loadMs, cycleMs float64) (*float64, string, *float64) {
	if loaders <= 0 || loadMs <= 0 || cycleMs <= 0 {
		return nil, "", nil
	}
	ideal := round2(float64(loaders) * cycleMs / loadMs)
	if trucks == 0 {
		return nil, "", &ideal
	}
	mf := round2(float64(trucks) * loadMs / (float64(loaders) * cycleMs))
	class := "match"
	switch {
	case mf < mfLow:
		class = "under"
	case mf > mfHigh:
		class = "over"
	}
	return &mf, class, &ideal
}

// coveredMs: how long at least one stay covers [from, to) (union of the intervals).
func coveredMs(stays []dto.StayInterval, from, to int64) int64 {
	list := append([]dto.StayInterval{}, stays...)
	sort.Slice(list, func(i, j int) bool { return list[i].Start < list[j].Start })
	var total, curS, curE int64
	open := false
	for _, s := range list {
		a, b := max(s.Start, from), min(s.End, to)
		if b <= a {
			continue
		}
		if !open || a > curE {
			if open {
				total += curE - curS
			}
			curS, curE, open = a, b, true
		} else if b > curE {
			curE = b
		}
	}
	if open {
		total += curE - curS
	}
	return total
}

// suggestMoves: for each undermatched fleet (excavator waits), take one active DT from the most
// overmatched fleet that keeps at least one DT, as long as both fleets end up closer to MF 1.
func suggestMoves(fleets []dto.Fleet) []dto.FleetMove {
	type state struct {
		f      *dto.Fleet
		active int
	}
	all := make([]*state, len(fleets))
	for i := range fleets {
		all[i] = &state{f: &fleets[i], active: fleets[i].Active}
	}
	mfOf := func(s *state, n int) *float64 {
		mf, _, _ := matchFactor(n, loadersPerFleet, s.f.LoadTimeMs, s.f.CycleMs)
		return mf
	}
	dist := func(mf *float64) float64 {
		if mf == nil {
			return 1
		}
		return math.Abs(*mf - 1)
	}
	used := map[string]bool{}
	moves := []dto.FleetMove{}
	for _, to := range all {
		if to.f.Loader == nil || to.f.Match != "under" {
			continue
		}
		var best *state
		for _, from := range all {
			if from == to || from.active < 2 {
				continue
			}
			mf := mfOf(from, from.active)
			if mf == nil || *mf <= mfHigh {
				continue
			}
			if best == nil || *mf > *mfOf(best, best.active) {
				best = from
			}
		}
		if best == nil {
			continue
		}
		// Both fleets must get closer to MF 1.
		if dist(mfOf(to, to.active+1)) >= dist(mfOf(to, to.active)) || dist(mfOf(best, best.active-1)) >= dist(mfOf(best, best.active)) {
			continue
		}
		var truck *dto.FleetTruck
		for i := range best.f.Trucks {
			t := &best.f.Trucks[i]
			if t.Inactive == nil && !used[t.Imei] && (truck == nil || (truck.AtFront && !t.AtFront)) {
				truck = t // prefer one not being loaded right now
			}
		}
		if truck == nil {
			continue
		}
		used[truck.Imei] = true
		m := dto.FleetMove{Imei: truck.Imei, Label: truck.Label,
			FromRouteID: best.f.RouteID, FromRouteName: best.f.RouteName, ToRouteID: to.f.RouteID, ToRouteName: to.f.RouteName,
			FromMfAfter: mfOf(best, best.active-1), ToMfAfter: mfOf(to, to.active+1)}
		m.Text = fmt.Sprintf("Pindahkan %s dari fleet %s ke fleet %s", truck.Label, best.f.RouteName, to.f.RouteName)
		moves = append(moves, m)
		best.active--
		to.active++
	}
	return moves
}

// siteNames: the names of ids, joined (a route's stockpiles / jetties).
func siteNames(by map[string]dto.Site, ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if s, ok := by[id]; ok {
			names = append(names, s.Name)
		}
	}
	return strings.Join(names, " / ")
}
