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
	// extraLoadMs: a unit returning to / loading at front loading when the shift closed must leave
	// front loading within this long to still finish one more ritase (repository.FinishingTrip).
	extraLoadMs = 3600_000
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

	lateMu sync.Mutex
	late   map[string]lateRange // imei → GPS time range of late records not recomputed yet

	mdt          repository.MdtRepository // optional: operator status for the queue alert
	queueMu      sync.Mutex
	queueSince   map[string]int64 // site|imei → first seen queuing by GPS
	queueAlerted map[string]int64 // site → last alert

	loc        *time.Location // report time zone: break times (HH:MM) are local times
	lastBreaks int64

	zones         repository.SpeedZoneRepository // optional: live overspeed alert
	overspeedAt   map[string]int64               // imei|zone → last alert
	lastOverspeed int64
}

type lateRange struct{ from, to int64 }

// lateDebounce: late records come in many packets; wait until the burst is over.
const lateDebounce = 30 * time.Second

// EnqueueLate marks records of imei in [from, to] as arrived late. The shifts they belong
// to are recomputed shortly after, whatever their age (not only active / just closed).
func (s *Scheduler) EnqueueLate(imei string, from, to int64) {
	s.lateMu.Lock()
	defer s.lateMu.Unlock()
	if s.late == nil {
		s.late = map[string]lateRange{}
	}
	r, ok := s.late[imei]
	if !ok {
		r = lateRange{from, to}
	}
	r.from, r.to = min(r.from, from), max(r.to, to)
	s.late[imei] = r
}

// flushLate recomputes the shifts touched by late records.
func (s *Scheduler) flushLate(ctx context.Context) {
	s.lateMu.Lock()
	pending := s.late
	s.late = nil
	s.lateMu.Unlock()
	if len(pending) == 0 {
		return
	}
	seen := map[string]bool{}
	list := []dto.Shift{}
	for imei, r := range pending {
		shifts, err := s.shifts.ShiftsTouching(ctx, imei, r.from, r.to, lateArrivalMs)
		if err != nil {
			s.log("Gagal mencari shift untuk data terlambat: " + err.Error())
			continue
		}
		for _, sh := range shifts {
			if !seen[sh.ID] {
				seen[sh.ID] = true
				list = append(list, sh)
			}
		}
		s.log(fmt.Sprintf("Data terlambat %s (%s s/d %s) → hitung ulang %d shift", imei,
			time.UnixMilli(r.from).Format("02/01 15:04"), time.UnixMilli(r.to).Format("02/01 15:04"), len(shifts)))
	}
	s.Recompute(ctx, list)
}

// runLate flushes late records every lateDebounce until ctx is done.
func (s *Scheduler) runLate(ctx context.Context) {
	t := time.NewTicker(lateDebounce)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.flushLate(ctx)
		}
	}
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
	// An excavator loads; it drives no ritase.
	if shift.StartedAt == nil || s.tracker.UnitType(a.Imei) == "EX" {
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
	sites, err := s.sites.SitesByIDs(ctx, route.SiteIDs(), true)
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

	// Returning to front loading or still loading when the shift closed: the unit loads once more
	// and finishes that ritase (repository.FinishingTrip), so it still belongs here.
	if a.EndAt == nil && shift.ClosedAt != nil && end == *shift.ClosedAt {
		lastLeg, lastStart, extra := "", int64(0), int64(-1)
		for _, t := range rep.Trips {
			if t.Start >= start && t.Start < end && t.Start >= lastStart {
				lastLeg, lastStart = t.Leg, t.Start
			}
			if t.Leg == "go" && t.Start >= end && t.Start < end+extraLoadMs && (extra < 0 || t.Start < extra) {
				extra = t.Start
			}
		}
		if lastLeg == "back" && extra >= 0 {
			// Not when the unit already works in another shift by then (no ritase counted twice).
			if other, err := s.shifts.ShiftsTouching(ctx, a.Imei, extra, extra, 0); err == nil && !touchesOther(other, shift.ID) {
				end = extra + 1
			}
		}
	}

	payload := s.tracker.PayloadTon(a.Imei)
	out := []dto.TripUpsert{}
	for _, t := range rep.Trips {
		if t.Start < start || t.Start >= end {
			continue
		}
		// loading_* / unloading_* = origin / destination of this trip: go = pit → the stockpile / jetty
		// reached (still on the way: the unit's own one), back = that stockpile / jetty → pit.
		from, to := route.LoadingID, route.UnloadingID
		if a.UnloadingID != nil {
			to = *a.UnloadingID
		}
		if t.Leg == "go" && t.Unloading != nil {
			to = t.Unloading.SiteID
		}
		if t.Leg != "go" {
			from, to = t.Loading.SiteID, route.LoadingID
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
		if t.Leg == "go" {
			u.PayloadTon = &payload
		}
		out = append(out, u)
	}
	changed, err := s.trips.ReplaceTrips(ctx, a.ID, out)
	if err != nil || s.visits == nil {
		return changed, err
	}
	// The last ritase that left front loading before the end: its stay at dumping belongs here,
	// even when it arrives or is still dumping after the shift stopped.
	var lastArrival *int64
	var lastStart int64
	for _, t := range rep.Trips {
		if t.Leg == "go" && t.Start < end && t.Unloading != nil && (lastArrival == nil || t.Start > lastStart) {
			arr := t.Unloading.Arrive
			lastArrival, lastStart = &arr, t.Start
		}
	}
	n, err := s.visits.ReplaceVisits(ctx, a.ID, visitsOf(records, shift, a, route, sites, start, end, lastArrival))
	return changed + n, err
}

func touchesOther(shifts []dto.Shift, id string) bool {
	for _, x := range shifts {
		if x.ID != id {
			return true
		}
	}
	return false
}

// visitsOf: stays at the route's loading/unloading sites during the assignment. Passing
// through without stopping (zero dwell) is not a stay.
func visitsOf(records []dto.AvlRecord, shift dto.Shift, a dto.Assignment, route dto.Route, sites []dto.Site, start, end int64, lastArrival *int64) []dto.SiteVisitUpsert {
	out := []dto.SiteVisitUpsert{}
	closed := shift.ClosedAt != nil || a.EndAt != nil
	byID := map[string]dto.Site{}
	for _, x := range sites {
		byID[x.ID] = x
	}
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
		st := analytics.SplitVisit(records, byID[v.SiteID], v)
		out = append(out, dto.SiteVisitUpsert{
			AssignmentID: a.ID, ShiftID: shift.ID, DriverID: a.DriverID, Imei: a.Imei, RouteID: route.ID,
			SiteID: v.SiteID, SiteName: v.Name, Role: role, ArriveAt: v.Arrive, DepartAt: v.Depart, DwellMs: v.DwellMs,
			// Still there when the shift is over: the unit is done ("Selesai"), not waiting. Except the
			// final dumping of the last ritase: it stays "inside" until the unit leaves dumping.
			Finished: closed && v.Depart == nil && !finalStop,
			QueueMs:  st.QueueMs, SpotMs: st.SpotMs, WorkMs: st.WorkMs,
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
		s.log("Gagal membaca rute untuk menghitung ritase: " + err.Error())
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
				s.log(fmt.Sprintf("Gagal menghitung ritase %s (%s): %v", sh.Name, a.Imei, err))
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
		text := fmt.Sprintf("Shift %s (%s) sudah melewati jam selesai dan masih berjalan. Tutup atau perpanjang di menu Shift.", x.Name, x.Date)
		s.tracker.Log("conn", "", text, "warn")
		s.tracker.Notice("warn", "Shift melewati jam selesai", text, "/shifts")
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
	s.checkQueues(ctx, now)
	s.checkBreaks(ctx, now)
	s.checkOverspeed(ctx, now)

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
		logger.Warn("Gagal membuat template shift awal", zap.Error(err))
	}
	s.tracker.OnLate(s.EnqueueLate)
	go s.runLate(ctx)
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
