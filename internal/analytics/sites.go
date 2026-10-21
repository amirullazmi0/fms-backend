package analytics

import (
	"math"
	"sort"
	"strings"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

// Ritase report from device records + loading/unloading sites:
//
//	visit  = when the truck enters & leaves a site radius,
//	ritase = leave the loading site of a route until arriving at that route's unloading site.
//
// Computed from records (not live events), so re-sent device data (backfill) counts too.
const (
	ExitPoints = 2           // left only after 2 consecutive points outside the radius...
	ExitMs     = 60_000      // ...or outside for more than 1 minute (GPS jumps a few metres)
	MergeMs    = 2 * 60_000  // leave & re-enter the same site within 2 minutes = one visit
	OngoingMs  = 15 * 60_000 // last data still fresh = ritase still in progress
	LoadMinMs  = 60_000      // loading = truck stops & waits; passing through (< 1 min) starts no ritase
	PassGapMs  = 3 * 60_000  // max gap between records to guess the truck passed a site between them
)

type openVisit struct {
	arrive   int64
	lastIn   int64
	firstOut *int64
	outCount int
}

// passNear is the closest distance (metres) from segment a→b to the site centre,
// plus the relative position on the segment (0..1). With a small radius a truck can
// cross the site between two GPS records while unloading on the move.
func passNear(a, b *dto.AvlRecord, site *dto.Site) (d, t float64) {
	kx := 111320 * math.Cos(site.Lat*math.Pi/180)
	ky := 110540.0
	ax := (a.Lng - site.Lng) * kx
	ay := (a.Lat - site.Lat) * ky
	dx := (b.Lng - a.Lng) * kx
	dy := (b.Lat - a.Lat) * ky
	l := dx*dx + dy*dy
	if l != 0 {
		t = math.Min(1, math.Max(0, -(ax*dx+ay*dy)/l))
	}
	return math.Hypot(ax+t*dx, ay+t*dy), t
}

func i64(v int64) *int64 { return &v }

// FindVisits returns every site visit ordered by arrival.
func FindVisits(records []dto.AvlRecord, sites []dto.Site) []dto.SiteVisit {
	valid := make([]dto.AvlRecord, 0, len(records))
	for _, r := range records {
		if r.Valid {
			valid = append(valid, r)
		}
	}
	visits := []dto.SiteVisit{}
	for si := range sites {
		site := &sites[si]
		mine := []dto.SiteVisit{}
		var open *openVisit
		closeVisit := func(o *openVisit, depart *int64) {
			end := o.lastIn
			if depart != nil {
				end = *depart
			}
			if n := len(mine); n > 0 {
				prev := &mine[n-1]
				if prev.Depart != nil && o.arrive-*prev.Depart <= MergeMs {
					prev.Depart = depart
					prev.DwellMs = end - prev.Arrive
					return
				}
			}
			mine = append(mine, dto.SiteVisit{SiteID: site.ID, Name: site.Name, Arrive: o.arrive, Depart: depart, DwellMs: end - o.arrive})
		}
		var prev *dto.AvlRecord
		for ri := range valid {
			r := &valid[ri]
			before := prev
			prev = r
			inside := DistanceM(r.Lat, r.Lng, site.Lat, site.Lng) <= float64(site.Radius)
			if !inside && open == nil && before != nil && r.Time-before.Time <= PassGapMs {
				// Two records outside, but the line between them crosses the radius = passed without stopping.
				d, t := passNear(before, r, site)
				if d <= float64(site.Radius) && t > 0 && t < 1 {
					at := int64(math.Floor(float64(before.Time) + t*float64(r.Time-before.Time) + 0.5))
					closeVisit(&openVisit{arrive: at, lastIn: at}, i64(at))
				}
				continue
			}
			if inside {
				if open == nil {
					open = &openVisit{arrive: r.Time, lastIn: r.Time}
				}
				open.lastIn = r.Time
				open.firstOut = nil
				open.outCount = 0
				continue
			}
			if open == nil {
				continue
			}
			if open.firstOut == nil {
				open.firstOut = i64(r.Time)
			}
			open.outCount++
			if open.outCount >= ExitPoints || r.Time-open.lastIn > ExitMs {
				closeVisit(open, open.firstOut)
				open = nil
			}
		}
		if open != nil {
			closeVisit(open, nil) // still inside until the last record
		}
		visits = append(visits, mine...)
	}
	sort.SliceStable(visits, func(i, j int) bool { return visits[i].Arrive < visits[j].Arrive })
	return visits
}

// fuelAround is the fuel level around a moment: median of the 3 nearest records (tank sloshes).
func fuelAround(records []dto.AvlRecord, t int64, unit *string) *float64 {
	if unit == nil {
		return nil
	}
	type near struct {
		d int64
		f float64
	}
	cands := []near{}
	for i := range records {
		r := &records[i]
		d := r.Time - t
		if d < 0 {
			d = -d
		}
		if d > 10*60_000 {
			continue
		}
		if f := FuelOf(r); f != nil && f.Unit == *unit {
			cands = append(cands, near{d, f.Value})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].d < cands[j].d })
	if len(cands) > 3 {
		cands = cands[:3]
	}
	if len(cands) == 0 {
		return nil
	}
	vals := make([]float64, len(cands))
	for i, c := range cands {
		vals[i] = c.f
	}
	sort.Float64s(vals)
	return ptrF(round1(vals[len(vals)/2]))
}

func measure(records []dto.AvlRecord, start, end int64) dto.DaySummary {
	slice := make([]dto.AvlRecord, 0)
	for _, r := range records {
		if r.Time >= start && r.Time <= end {
			slice = append(slice, r)
		}
	}
	return SummarizeDay(slice, start, end)
}

type routeRef struct {
	id, name string
}

type returning struct {
	route *dto.Route
	from  dto.SiteVisit
}

// BuildReport finds ritase (go = loading → unloading, back = unloading → loading) per route.
func BuildReport(records []dto.AvlRecord, routes []dto.Route, sites []dto.Site, from, to, now int64) dto.RouteReport {
	sorted := append([]dto.AvlRecord(nil), records...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time < sorted[j].Time })
	visits := FindVisits(sorted, sites)
	var lastTime int64
	if len(sorted) > 0 {
		lastTime = sorted[len(sorted)-1].Time
	}
	fuelUnit := fuelUnitOf(sortedValid(sorted))

	trips := []dto.RouteTrip{}
	push := func(leg string, route routeRef, loading dto.SiteVisit, unloading *dto.SiteVisit, status string) {
		start := *loading.Depart
		end := lastTime
		if unloading != nil {
			end = unloading.Arrive
		}
		if end <= start {
			return
		}
		s := measure(sorted, start, end)
		stops := 0
		for _, g := range s.Segments {
			if g.Type == "stop" {
				stops++
			}
		}
		trip := dto.RouteTrip{
			RouteID:       route.id,
			RouteName:     route.name,
			Leg:           leg,
			Loading:       loading,
			Unloading:     unloading,
			Status:        status,
			Start:         start,
			End:           end,
			DurationMs:    end - start,
			Distance:      s.Distance,
			MovingMs:      s.MovingMs,
			Stops:         stops,
			StoppedMs:     s.StoppedMs,
			MaxSpeed:      s.MaxSpeed,
			AvgSpeed:      s.AvgSpeed,
			FuelAtLoading: fuelAround(sorted, start, fuelUnit),
		}
		if sameUnit(s.FuelUnit, fuelUnit) {
			trip.FuelUsed = s.FuelUsed
		}
		if unloading != nil {
			trip.FuelAtUnloading = fuelAround(sorted, end, fuelUnit)
		}
		trips = append(trips, trip)
	}

	// Ritase = leave a route's loading site, then arrive at the unloading site of THE SAME route.
	// Sites have no fixed role: it comes from the route. The loading used is the last visit to
	// a loading site before that unloading (the truck may stop elsewhere first).
	//
	// The way back = leave that unloading site until arriving again at the same route's loading
	// site. So within one shift a round trip is recorded: go, back, go, back, …
	loadingIDs := map[string]bool{}
	for _, r := range routes {
		loadingIDs[r.LoadingID] = true
	}
	tailStatus := "unfinished"
	if now-lastTime <= OngoingMs && to >= now-OngoingMs {
		tailStatus = "ongoing"
	}
	var pending *dto.SiteVisit
	var ret *returning
	for vi := range visits {
		v := visits[vi]
		// Back finished: arrived again at the loading site of the route just delivered.
		if back := ret; back != nil && back.from.Depart != nil && v.SiteID == back.route.LoadingID && v.Arrive >= *back.from.Depart {
			push("back", routeRef{back.route.ID, back.route.Name}, back.from, &v, "done")
			ret = nil
		}
		if p := pending; p != nil && p.Depart != nil && v.Arrive >= *p.Depart && v.SiteID != p.SiteID {
			for ri := range routes {
				route := &routes[ri]
				if route.LoadingID == p.SiteID && route.HasUnloading(v.SiteID) {
					arrived := v
					push("go", routeRef{route.ID, route.Name}, *p, &arrived, "done")
					pending = nil
					ret = &returning{route: route, from: v}
					break
				}
			}
		}
		// This site is another route's loading (e.g. unloading A = loading B): the next ritase starts here.
		// Loading always waits to load; a truck merely passing a loading site starts no new ritase.
		if loadingIDs[v.SiteID] && v.DwellMs >= LoadMinMs {
			pv := v
			pending = &pv
		}
	}
	if last := pending; last != nil && last.Depart != nil && (ret == nil || ret.from.Arrive < last.Arrive) {
		// Not arrived yet: candidate routes = every route loading at this site.
		names := []string{}
		var first *dto.Route
		for ri := range routes {
			if routes[ri].LoadingID == last.SiteID {
				if first == nil {
					first = &routes[ri]
				}
				names = append(names, routes[ri].Name)
			}
		}
		if first != nil {
			push("go", routeRef{first.ID, strings.Join(names, " / ")}, *last, nil, tailStatus)
		}
	} else if ret != nil && ret.from.Depart != nil {
		push("back", routeRef{ret.route.ID, ret.route.Name}, ret.from, nil, tailStatus) // on the way back
	}

	// Only what touches the requested range (records are fetched earlier so cross-day ritase reads whole).
	inRange := []dto.RouteTrip{}
	for _, r := range trips {
		if r.End >= from && r.Start <= to {
			inRange = append(inRange, r)
		}
	}
	shownVisits := []dto.SiteVisit{}
	for _, v := range visits {
		dep := lastTime
		if v.Depart != nil {
			dep = *v.Depart
		}
		if dep >= from && v.Arrive <= to {
			shownVisits = append(shownVisits, v)
		}
	}

	totals := dto.RouteReportTotals{}
	var doneSum, backSum float64
	fuels := []float64{}
	for _, r := range inRange {
		if r.Leg == "go" {
			totals.Trips++
			if r.Status == "done" {
				totals.Done++
				doneSum += float64(r.DurationMs)
			}
		} else if r.Status == "done" {
			totals.BackDone++
			backSum += float64(r.DurationMs)
		}
		totals.Distance += r.Distance
		totals.Stops += r.Stops
		totals.StoppedMs += r.StoppedMs
		if r.FuelUsed != nil {
			fuels = append(fuels, *r.FuelUsed)
		}
	}
	if totals.Done > 0 {
		totals.AvgDurationMs = doneSum / float64(totals.Done)
	}
	if totals.BackDone > 0 {
		totals.BackAvgMs = backSum / float64(totals.BackDone)
	}
	if len(fuels) > 0 {
		s := 0.0
		for _, f := range fuels {
			s += f
		}
		totals.FuelUsed = ptrF(round1(s))
	}

	if routes == nil {
		routes = []dto.Route{}
	}
	if sites == nil {
		sites = []dto.Site{}
	}
	return dto.RouteReport{
		From: from, To: to, FuelUnit: fuelUnit,
		Routes: routes, Sites: sites, Visits: shownVisits, Trips: inRange, Totals: totals,
	}
}

// TruckRecords is one truck's records over the same range.
type TruckRecords struct {
	Imei    string
	Records []dto.AvlRecord
}

// RouteHistory: real path + estimates from finished ritase of every truck on one route.
func RouteHistory(route dto.Route, sites []dto.Site, perTruck []TruckRecords, from, to, now int64, days int) dto.RouteHistory {
	type doneTrip struct {
		dto.RouteTrip
		imei    string
		records []dto.AvlRecord
	}
	done := []doneTrip{}
	var fuelUnit *string
	for _, t := range perTruck {
		rep := BuildReport(t.Records, []dto.Route{route}, sites, from, to, now)
		if fuelUnit == nil {
			fuelUnit = rep.FuelUnit
		}
		for _, trip := range rep.Trips {
			if trip.Leg == "go" && trip.Status == "done" {
				done = append(done, doneTrip{trip, t.Imei, t.Records})
			}
		}
	}

	avg := func(xs []float64) float64 {
		if len(xs) == 0 {
			return 0
		}
		s := 0.0
		for _, x := range xs {
			s += x
		}
		return s / float64(len(xs))
	}
	durations, distances, stops, fuels := []float64{}, []float64{}, []float64{}, []float64{}
	var minD, maxD int64
	for i, d := range done {
		durations = append(durations, float64(d.DurationMs))
		distances = append(distances, d.Distance)
		stops = append(stops, float64(d.Stops))
		if d.FuelUsed != nil {
			fuels = append(fuels, *d.FuelUsed)
		}
		if i == 0 || d.DurationMs < minD {
			minD = d.DurationMs
		}
		if i == 0 || d.DurationMs > maxD {
			maxD = d.DurationMs
		}
	}

	var latest *dto.RouteHistoryLatest
	if len(done) > 0 {
		last := done[0]
		for _, d := range done[1:] {
			if d.End > last.End {
				last = d
			}
		}
		pts := []dto.AvlRecord{}
		for _, r := range last.records {
			if r.Valid && r.Time >= last.Start && r.Time <= last.End {
				pts = append(pts, r)
			}
		}
		// Start at the loading site, end at the unloading site, thinned out to stay light.
		step := int(math.Ceil(float64(len(pts)) / 600))
		if step < 1 {
			step = 1
		}
		coords := [][2]float64{}
		for _, s := range sites {
			if s.ID == last.Loading.SiteID {
				coords = append(coords, [2]float64{s.Lat, s.Lng})
				break
			}
		}
		for i, r := range pts {
			if i%step == 0 || i == len(pts)-1 {
				coords = append(coords, [2]float64{r.Lat, r.Lng})
			}
		}
		if last.Unloading != nil {
			for _, s := range sites {
				if s.ID == last.Unloading.SiteID {
					coords = append(coords, [2]float64{s.Lat, s.Lng})
					break
				}
			}
		}
		latest = &dto.RouteHistoryLatest{
			Imei: last.imei, Start: last.Start, End: last.End,
			Distance: last.Distance, DurationMs: last.DurationMs, Coords: coords,
		}
	}

	h := dto.RouteHistory{
		Days:          days,
		Trips:         len(done),
		AvgDurationMs: avg(durations),
		MinDurationMs: minD,
		MaxDurationMs: maxD,
		AvgDistance:   avg(distances),
		AvgStops:      avg(stops),
		FuelUnit:      fuelUnit,
		Latest:        latest,
	}
	if len(fuels) > 0 {
		h.AvgFuel = ptrF(round1(avg(fuels)))
	}
	return h
}
