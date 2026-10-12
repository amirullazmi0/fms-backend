package analytics

import (
	"math"
	"sort"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

// Usual road per direction: of the latest finished ritase, the GPS trace that overlaps
// the others the most (a medoid). Pergi = loading → unloading, pulang = unloading → loading.
const (
	usualMaxTrips = 25  // latest ritase compared per direction
	usualNearM    = 50  // a point "is on" another trace within this distance
	usualSameRoad = 0.8 // traces sharing ≥80% of points (both ways) took the same road
	usualCmpPts   = 120 // points per trace used for comparing
	usualOutPts   = 600 // points returned for drawing
)

type tracedTrip struct {
	trip   dto.RouteTrip
	coords [][2]float64
}

// UsualPaths builds the usual pergi & pulang roads of one route from the trucks' records.
func UsualPaths(route dto.Route, sites []dto.Site, perTruck []TruckRecords, from, to, now int64) *dto.RouteUsualPaths {
	siteAt := map[string][2]float64{}
	for _, s := range sites {
		siteAt[s.ID] = [2]float64{s.Lat, s.Lng}
	}
	var goTrips, backTrips []tracedTrip
	for _, t := range perTruck {
		rep := BuildReport(t.Records, []dto.Route{route}, sites, from, to, now)
		for _, trip := range rep.Trips {
			if trip.Status != "done" || trip.Unloading == nil {
				continue
			}
			c := traceOf(t.Records, trip, siteAt)
			if len(c) < 2 {
				continue
			}
			if trip.Leg == "go" {
				goTrips = append(goTrips, tracedTrip{trip, c})
			} else if trip.Leg == "back" {
				backTrips = append(backTrips, tracedTrip{trip, c})
			}
		}
	}
	out := &dto.RouteUsualPaths{Go: usualOf(goTrips), Back: usualOf(backTrips)}
	if out.Go == nil && out.Back == nil {
		return nil
	}
	return out
}

// traceOf: start site, the valid GPS points of the trip, end site.
func traceOf(records []dto.AvlRecord, trip dto.RouteTrip, siteAt map[string][2]float64) [][2]float64 {
	c := [][2]float64{}
	if p, ok := siteAt[trip.Loading.SiteID]; ok {
		c = append(c, p)
	}
	for _, r := range records {
		if r.Valid && r.Time >= trip.Start && r.Time <= trip.End {
			c = append(c, [2]float64{r.Lat, r.Lng})
		}
	}
	if p, ok := siteAt[trip.Unloading.SiteID]; ok {
		c = append(c, p)
	}
	return c
}

func usualOf(trips []tracedTrip) *dto.UsualPath {
	if len(trips) == 0 {
		return nil
	}
	sort.Slice(trips, func(i, j int) bool { return trips[i].trip.End > trips[j].trip.End })
	total := len(trips)
	durations := 0.0
	for _, t := range trips {
		durations += float64(t.trip.DurationMs)
	}
	if len(trips) > usualMaxTrips {
		trips = trips[:usualMaxTrips]
	}

	n := len(trips)
	thin := make([][][2]float64, n)
	for i, t := range trips {
		thin[i] = thinOut(t.coords, usualCmpPts)
	}
	// sim[i][j] = share of i's points lying on j's trace; same road = both ways high.
	same := make([][]float64, n)
	for i := range same {
		same[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			s := math.Min(coverage(thin[i], thin[j]), coverage(thin[j], thin[i]))
			same[i][j], same[j][i] = s, s
		}
	}
	best, bestScore := 0, -1.0
	for i := 0; i < n; i++ {
		score := 0.0
		for j := 0; j < n; j++ {
			if i != j {
				score += same[i][j]
			}
		}
		// Ties (e.g. one trip): the latest wins, trips are sorted newest first.
		if score > bestScore+1e-9 {
			best, bestScore = i, score
		}
	}
	follow := 0
	for j := 0; j < n; j++ {
		if j == best || same[best][j] >= usualSameRoad {
			follow++
		}
	}
	t := trips[best]
	return &dto.UsualPath{
		Trips:         total,
		Compared:      n,
		Follow:        follow,
		Distance:      t.trip.Distance,
		DurationMs:    t.trip.DurationMs,
		AvgDurationMs: durations / float64(total),
		LastEnd:       trips[0].trip.End,
		Coords:        thinOut(t.coords, usualOutPts),
	}
}

// thinOut keeps at most max points (always the first and the last).
func thinOut(c [][2]float64, max int) [][2]float64 {
	if len(c) <= max {
		return c
	}
	step := float64(len(c)-1) / float64(max-1)
	out := make([][2]float64, 0, max)
	for i := 0; i < max; i++ {
		out = append(out, c[int(math.Round(float64(i)*step))])
	}
	return out
}

// coverage: share of a's points within usualNearM of b's polyline.
func coverage(a, b [][2]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	lat0 := a[0][0]
	kx := math.Cos(rad(lat0)) * 111320
	const ky = 110540.0
	xy := func(p [2]float64) (float64, float64) { return p[1] * kx, p[0] * ky }
	hit := 0
	for _, p := range a {
		px, py := xy(p)
		for k := 0; k < len(b); k++ {
			var d float64
			if k == len(b)-1 || len(b) == 1 {
				bx, by := xy(b[k])
				d = math.Hypot(px-bx, py-by)
			} else {
				ax, ay := xy(b[k])
				bx, by := xy(b[k+1])
				d = segDist(px, py, ax, ay, bx, by)
			}
			if d <= usualNearM {
				hit++
				break
			}
		}
	}
	return float64(hit) / float64(len(a))
}

func segDist(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l := dx*dx + dy*dy
	if l == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}
