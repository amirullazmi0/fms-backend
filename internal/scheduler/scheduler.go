// Package scheduler runs the shift clock and keeps the stored ritase (trips table) fresh.
// Port of fmctest/server/shifts.ts.
//
// Rules:
//   - An assignment's window = [valid from, valid until) inside [shift start, shift end].
//   - A ritase belongs to the shift in which the truck LEFT the loading site. It may
//     arrive after the end, so records are read up to 6 hours after the shift closed.
//   - Ritase are stored in trips (the report base) and recomputed every 5 minutes while a
//     shift is active / just closed, plus whenever a shift starts, closes, or its
//     assignments change.
package scheduler

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/analytics"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"go.uber.org/zap"
)

const (
	dayMs         = 24 * 3600_000
	lateArrivalMs = 6 * 3600_000
	tickEvery     = time.Minute
	finishingMs   = 60_000
	claimSlackMs  = 30 * 60_000 // a stay may start this long before the assignment
	recomputeMs   = 5 * 60_000
	recordLimit   = 200000
)

type Scheduler struct {
	shifts  repository.ShiftRepository
	sites   repository.SiteRepository
	trips   repository.TripRepository
	visits  repository.VisitRepository // optional: stays at loading/unloading sites
	devices repository.DeviceRepository
	drivers repository.DriverRepository
	tracker *tracker.Tracker

	running       atomic.Bool
	lastRecompute int64
	lastFinishing int64
	recomputeMu   sync.Mutex // one recompute at a time (tick vs. API triggers)
}

func New(shifts repository.ShiftRepository, sites repository.SiteRepository, trips repository.TripRepository,
	devices repository.DeviceRepository, drivers repository.DriverRepository, t *tracker.Tracker) *Scheduler {
	return &Scheduler{shifts: shifts, sites: sites, trips: trips, devices: devices, drivers: drivers, tracker: t}
}

// WithVisits also stores the stays at loading/unloading sites on every recompute.
func (s *Scheduler) WithVisits(v repository.VisitRepository) *Scheduler {
	s.visits = v
	return s
}

func (s *Scheduler) log(text string) {
	s.tracker.Log("conn", "", text, "info")
}

// computeAssignment rebuilds the stored ritase of one assignment.
func (s *Scheduler) computeAssignment(ctx context.Context, shift dto.Shift, a dto.Assignment, routes map[string]dto.Route, now int64) (int, error) {
	if shift.StartedAt == nil {
		return s.trips.ReplaceTrips(ctx, a.ID, nil)
	}
	// Assignment window inside the shift: [start, end). Moving route/truck mid-shift ends the
	// old assignment exactly when the new one starts, so no ritase is counted twice.
	start := *shift.StartedAt
	if a.StartAt != nil && *a.StartAt > start {
		start = *a.StartAt
	}
	end := int64(math.MaxInt64)
	if a.EndAt != nil {
		end = *a.EndAt
	}
	closedAt := now + 1
	if shift.ClosedAt != nil {
		closedAt = *shift.ClosedAt
	}
	if closedAt < end {
		end = closedAt
	}
	route, ok := routes[a.RouteID]
	if !ok {
		return 0, nil
	}
	sites, err := s.sites.SitesByIDs(ctx, []string{route.LoadingID, route.UnloadingID}, true)
	if err != nil || len(sites) < 2 {
		return 0, err
	}
	until := now
	if end < math.MaxInt64-lateArrivalMs && end+lateArrivalMs < until {
		until = end + lateArrivalMs
	}
	records, err := s.devices.RecordsBetween(ctx, a.Imei, start-dayMs, until, recordLimit)
	if err != nil {
		return 0, err
	}
	rep := analytics.BuildReport(records, []dto.Route{route}, sites, start, until, now)
	byID := map[string]dto.Site{}
	for _, x := range sites {
		byID[x.ID] = x
	}

	out := []dto.TripUpsert{}
	for _, t := range rep.Trips {
		if t.Start < start || t.Start >= end {
			continue
		}
		// loading_* / unloading_* = origin / destination of this trip (back: reversed from the route).
		from, to := route.LoadingID, route.UnloadingID
		if t.Leg != "go" {
			from, to = route.UnloadingID, route.LoadingID
		}
		loadingName := t.Loading.Name
		if x, ok := byID[from]; ok {
			loadingName = x.Name
		}
		unloadingName := ""
		if t.Unloading != nil {
			unloadingName = t.Unloading.Name
		}
		if x, ok := byID[to]; ok {
			unloadingName = x.Name
		}
		u := dto.TripUpsert{
			AssignmentID: a.ID, ShiftID: shift.ID, DriverID: a.DriverID, Imei: a.Imei,
			RouteID: route.ID, RouteName: route.Name, Leg: t.Leg,
			LoadingSiteID: from, LoadingName: loadingName, UnloadingSiteID: to, UnloadingName: unloadingName,
			DepartAt: t.Start, DurationMs: t.DurationMs, Distance: t.Distance, MovingMs: t.MovingMs,
			Stops: t.Stops, StoppedMs: t.StoppedMs, MaxSpeed: t.MaxSpeed, AvgSpeed: t.AvgSpeed,
			FuelUnit: rep.FuelUnit, FuelAtLoading: t.FuelAtLoading, FuelAtUnloading: t.FuelAtUnloading, FuelUsed: t.FuelUsed,
			LoadDwellMs: t.Loading.DwellMs, Status: "unfinished",
		}
		if t.Unloading != nil {
			arrive := t.End
			dwell := t.Unloading.DwellMs
			u.ArriveAt, u.UnloadDwellMs = &arrive, &dwell
		}
		if t.Status == "done" {
			u.Status = "done"
		}
		out = append(out, u)
	}
	changed, err := s.trips.ReplaceTrips(ctx, a.ID, out)
	if err != nil || s.visits == nil {
		return changed, err
	}
	// The ritase still on its way when the shift stopped: its arrival at unloading belongs here.
	var lastArrival *int64
	for _, t := range rep.Trips {
		if t.Leg == "go" && t.Start < end && t.Unloading != nil && t.Unloading.Arrive >= end {
			arr := t.Unloading.Arrive
			lastArrival = &arr
		}
	}
	n, err := s.visits.ReplaceVisits(ctx, a.ID, visitsOf(records, shift, a, route, sites, start, end, lastArrival))
	return changed + n, err
}

// visitsOf: stays at the route's loading/unloading sites during the assignment. Passing
// through without stopping (zero dwell) is not a stay.
func visitsOf(records []dto.AvlRecord, shift dto.Shift, a dto.Assignment, route dto.Route, sites []dto.Site, start, end int64, lastArrival *int64) []dto.SiteVisitUpsert {
	out := []dto.SiteVisitUpsert{}
	closed := shift.ClosedAt != nil || a.EndAt != nil
	for _, v := range analytics.FindVisits(records, sites) {
		finalStop := lastArrival != nil && v.Arrive == *lastArrival
		if v.DwellMs <= 0 || (v.Arrive >= end && !finalStop) || (v.Depart != nil && *v.Depart <= start) {
			continue
		}
		// A truck already parked there long before this assignment (e.g. overnight after the
		// previous shift) is not this assignment's stay: it stays with the earlier one.
		if v.Arrive < start-claimSlackMs {
			continue
		}
		role := "unloading"
		if v.SiteID == route.LoadingID {
			role = "loading"
		}
		out = append(out, dto.SiteVisitUpsert{
			AssignmentID: a.ID, ShiftID: shift.ID, DriverID: a.DriverID, Imei: a.Imei, RouteID: route.ID,
			SiteID: v.SiteID, SiteName: v.Name, Role: role, ArriveAt: v.Arrive, DepartAt: v.Depart, DwellMs: v.DwellMs,
			// Still there when the shift is over: the truck is done ("Selesai"), not waiting.
			Finished: closed && v.Depart == nil,
		})
	}
	return out
}

// Recompute rebuilds the ritase of every assignment of the given shifts.
func (s *Scheduler) Recompute(ctx context.Context, shifts []dto.Shift) int {
	if len(shifts) == 0 {
		return 0
	}
	s.recomputeMu.Lock()
	defer s.recomputeMu.Unlock()
	list, _, err := s.sites.GetRoutes(ctx, &dto.QueryGlobal{})
	if err != nil {
		s.log("Gagal baca rute buat ngitung ritase: " + err.Error())
		return 0
	}
	routes := map[string]dto.Route{}
	for _, r := range list {
		routes[r.ID] = r
	}
	now := time.Now().UnixMilli()
	changed := 0
	for _, sh := range shifts {
		for _, a := range sh.Assignments {
			n, err := s.computeAssignment(ctx, sh, a, routes, now)
			if err != nil {
				s.log(fmt.Sprintf("Gagal ngitung ritase %s (%s): %v", sh.Name, a.Imei, err))
				continue
			}
			changed += n
		}
	}
	if changed > 0 {
		s.tracker.Changed("trips")
	}
	return changed
}

// RecomputeShift recomputes one shift in the background (API calls must not wait for it).
func (s *Scheduler) RecomputeShift(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		sh, err := s.shifts.GetShift(ctx, id)
		if err != nil || sh == nil {
			return
		}
		s.Recompute(ctx, []dto.Shift{*sh})
	}()
}

func (s *Scheduler) tick(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	now := time.Now().UnixMilli()
	toStart, toClose, overdue, err := s.shifts.DueShifts(ctx, now)
	if err != nil {
		s.log("Penjadwal shift error: " + err.Error())
		return
	}
	changed := false
	for _, x := range toStart {
		// Auto start counts from the planned time, so ritase from the shift start are included.
		if ok, err := s.shifts.StartShift(ctx, x.ID, x.PlannedStart); err == nil && ok {
			s.log(fmt.Sprintf("Shift %s (%s) mulai otomatis", x.Name, x.Date))
			changed = true
		}
	}
	for _, x := range toClose {
		if ok, err := s.shifts.CloseShift(ctx, x.ID, "auto", nil, x.PlannedEnd); err == nil && ok {
			s.log(fmt.Sprintf("Shift %s (%s) ditutup otomatis", x.Name, x.Date))
			changed = true
			if sh, err := s.shifts.GetShift(ctx, x.ID); err == nil && sh != nil {
				s.Recompute(ctx, []dto.Shift{*sh})
			}
		}
	}
	for _, x := range overdue {
		if err := s.shifts.MarkOverdueNoted(ctx, x.ID); err != nil {
			continue
		}
		text := fmt.Sprintf("Shift %s (%s) udah lewat jam selesai dan masih jalan. Tutup atau perpanjang di menu Shift.", x.Name, x.Date)
		s.tracker.Log("conn", "", text, "warn")
		s.tracker.Notice("warn", "Shift lewat jam", text, "/shifts")
		changed = true
	}
	if changed {
		s.tracker.Changed("shifts")
	}

	// A stopped shift with a truck still driving its last ritase: check every minute, so the
	// truck shows "Selesai" soon after it reaches unloading.
	if now-s.lastFinishing >= finishingMs && now-s.lastRecompute < recomputeMs {
		s.lastFinishing = now
		if fin, err := s.shifts.FinishingShifts(ctx); err == nil && len(fin) > 0 {
			if s.Recompute(ctx, fin) == 0 {
				s.tracker.Changed("sites") // phase may still change (arrival seen), refresh duty
			}
		}
	}
	// Safety net: a stay can only be "inside" while its shift runs.
	if s.visits != nil {
		if n, err := s.visits.CloseStale(ctx); err == nil && n > 0 {
			s.tracker.Changed("trips")
		}
	}
	if now-s.lastRecompute >= recomputeMs {
		s.lastRecompute = now
		active, err1 := s.shifts.ActiveShifts(ctx)
		recent, err2 := s.shifts.RecentlyClosedShifts(ctx, lateArrivalMs)
		if err1 != nil || err2 != nil {
			s.log(fmt.Sprintf("Penjadwal shift error: %v %v", err1, err2))
			return
		}
		s.Recompute(ctx, append(active, recent...))
	}
}

// Run seeds the default templates and ticks every minute until ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	if err := s.drivers.SeedTemplates(ctx); err != nil {
		logger.Warn("Gagal bikin template shift awal", zap.Error(err))
	}
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		s.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
