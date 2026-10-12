// Package analytics turns raw AVL records into trips, stops, site visits and
// ritase (loading -> unloading). Port of fmctest/server/trips.ts and sites.ts;
// the thresholds below must stay identical or the ritase counts drift.
package analytics

import (
	"math"
	"sort"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

const (
	MovingKmh   = 5           // below this the truck is considered still (GPS drifts 1-3 km/h)
	MinStopMs   = 2 * 60_000  // shorter stops are traffic, still part of the trip
	MinTripMs   = 30_000      // shorter moves are GPS jumps, not a trip
	MinTripM    = 80.0        // ...or shorter than this distance
	GapMs       = 15 * 60_000 // no data this long = device still/off
	fuelWindow  = 30 * 60_000 // refuel/drop window
	earthRadius = 6371000.0
)

// Fuel jump thresholds: rise/fall this much in a short time = refuel / loss, not consumption.
var fuelJump = map[string]struct{ refuel, drop float64 }{
	"L": {10, 5},
	"%": {8, 4},
}

// FuelReading is a fuel level and its unit.
type FuelReading struct {
	Value float64
	Unit  string // L | %
}

// round1 is Math.round(x * 10) / 10 (JS rounds .5 up, towards +Inf).
func round1(x float64) float64 {
	return math.Floor(x*10+0.5) / 10
}

func ptrF(v float64) *float64 { return &v }
func ptrS(v string) *string   { return &v }

func ioNumber(io dto.IoMap, id string) (float64, bool) {
	v, ok := io[id]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

// FuelOf reads the CAN fuel level: litres (IO 84, x0.1) first, else percent (IO 89).
func FuelOf(r *dto.AvlRecord) *FuelReading {
	if l, ok := ioNumber(r.Io, "84"); ok && l > 0 {
		return &FuelReading{Value: l / 10, Unit: "L"}
	}
	if p, ok := ioNumber(r.Io, "89"); ok && p > 0 {
		return &FuelReading{Value: p, Unit: "%"}
	}
	return nil
}

func rad(d float64) float64 { return d * math.Pi / 180 }

// DistanceM is the haversine distance in metres.
func DistanceM(aLat, aLng, bLat, bLng float64) float64 {
	x := math.Pow(math.Sin(rad(bLat-aLat)/2), 2) + math.Cos(rad(aLat))*math.Cos(rad(bLat))*math.Pow(math.Sin(rad(bLng-aLng)/2), 2)
	return 2 * earthRadius * math.Asin(math.Sqrt(x))
}

func sameUnit(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

type run struct {
	moving   bool
	from, to int // point indexes, inclusive
}

// sortedValid returns the valid records ordered by time (stable).
func sortedValid(records []dto.AvlRecord) []dto.AvlRecord {
	valid := make([]dto.AvlRecord, 0, len(records))
	for _, r := range records {
		if r.Valid {
			valid = append(valid, r)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].Time < valid[j].Time })
	return valid
}

// fuelUnitOf is the unit of the first valid record with a fuel reading.
func fuelUnitOf(valid []dto.AvlRecord) *string {
	for i := range valid {
		if f := FuelOf(&valid[i]); f != nil {
			return ptrS(f.Unit)
		}
	}
	return nil
}

// SummarizeDay groups records into trip / stop segments with distance, speed and fuel.
func SummarizeDay(records []dto.AvlRecord, from, to int64) dto.DaySummary {
	valid := sortedValid(records)
	fuelUnit := fuelUnitOf(valid)
	points := make([]dto.TrackPoint, len(valid))
	for i := range valid {
		r := &valid[i]
		p := dto.TrackPoint{T: r.Time, Lat: r.Lat, Lng: r.Lng, Speed: r.Speed, Angle: r.Angle}
		if v, ok := r.Io["239"]; ok && v != nil {
			n, _ := v.(float64)
			on := n == 1
			p.Ign = &on
		}
		if f := FuelOf(r); f != nil && fuelUnit != nil && f.Unit == *fuelUnit {
			p.Fuel = ptrF(f.Value)
		}
		points[i] = p
	}

	sum := dto.DaySummary{
		From: from, To: to, FuelUnit: fuelUnit,
		FuelEvents: []dto.FuelEvent{}, Segments: []dto.TripSegment{}, Points: points,
	}
	if len(points) == 0 {
		return sum
	}

	// 1. Group consecutive points into "moving" / "still" runs.
	runs := []run{}
	for i, p := range points {
		moving := p.Speed >= MovingKmh
		gap := i > 0 && p.T-points[i-1].T > GapMs
		// A data gap only splits trips. A stop stays one segment (devices rarely send while parked).
		if n := len(runs); n > 0 && runs[n-1].moving == moving && (!gap || !moving) {
			runs[n-1].to = i
		} else {
			runs = append(runs, run{moving: moving, from: i, to: i})
		}
	}

	dur := func(r run) int64 { return points[r.to].T - points[r.from].T }
	dist := func(r run) float64 {
		d := 0.0
		for i := r.from + 1; i <= r.to; i++ {
			d += stepDistance(&points[i-1], &points[i])
		}
		return d
	}
	merge := func(keep func(r run, i int, all []run) bool) {
		out := []run{}
		for i, r := range runs {
			flipped := r
			if !keep(r, i, runs) {
				flipped.moving = !r.moving
			}
			if n := len(out); n > 0 {
				prev := &out[n-1]
				contiguous := points[flipped.from].T-points[prev.to].T <= GapMs
				if prev.moving == flipped.moving && (contiguous || !flipped.moving) {
					prev.to = flipped.to
					continue
				}
			}
			out = append(out, flipped)
		}
		runs = out
	}

	// 2. Short stops inside a trip (traffic) are merged into the trip.
	merge(func(r run, i int, all []run) bool {
		return r.moving || dur(r) >= MinStopMs || i == 0 || i == len(all)-1
	})
	// 3. "Moves" that are too short (GPS jumps while parked) are merged into the stop.
	merge(func(r run, _ int, _ []run) bool {
		return !r.moving || (dur(r) >= MinTripMs && dist(r) >= MinTripM)
	})

	// 4. Build segments. A stop lasts until the first point of the next segment.
	segments := make([]dto.TripSegment, len(runs))
	for i, r := range runs {
		a := points[r.from]
		b := points[r.to]
		end := b.T
		if !r.moving && i+1 < len(runs) {
			end = points[runs[i+1].from].T
		}
		slice := points[r.from : r.to+1]
		distance := 0.0
		if r.moving {
			distance = dist(r)
		}
		ms := end - a.T
		ignOn := 0
		maxSpeed := 0
		for _, p := range slice {
			if p.Ign != nil && *p.Ign {
				ignOn++
			}
			if p.Speed > maxSpeed {
				maxSpeed = p.Speed
			}
		}
		seg := dto.TripSegment{
			Type:      "stop",
			Start:     a.T,
			End:       end,
			Distance:  distance,
			From:      dto.LatLng{Lat: a.Lat, Lng: a.Lng},
			To:        dto.LatLng{Lat: b.Lat, Lng: b.Lng},
			Idle:      !r.moving && float64(ignOn) > float64(len(slice))/2, // engine on while still
			FromIndex: r.from,
			ToIndex:   r.to,
		}
		if r.moving {
			seg.Type = "trip"
			seg.MaxSpeed = maxSpeed
			if ms > 0 {
				seg.AvgSpeed = distance / 1000 / (float64(ms) / 3600e3)
			}
			seg.FuelUsed = fuelDrop(slice)
		}
		segments[i] = seg
	}

	var fuelEvents []dto.FuelEvent
	if fuelUnit != nil {
		fuelEvents = detectFuelEvents(points, *fuelUnit)
	}
	if fuelEvents == nil {
		fuelEvents = []dto.FuelEvent{}
	}

	var movingMs, stoppedMs, idleMs int64
	var distance float64
	trips, stops, maxSpeed := 0, 0, 0
	for _, s := range segments {
		if s.Type == "trip" {
			trips++
			movingMs += s.End - s.Start
			distance += s.Distance
			if s.MaxSpeed > maxSpeed {
				maxSpeed = s.MaxSpeed
			}
		} else {
			stops++
			stoppedMs += s.End - s.Start
			if s.Idle {
				idleMs += s.End - s.Start
			}
		}
	}

	sum.Distance = distance
	sum.MovingMs = movingMs
	sum.StoppedMs = stoppedMs
	sum.IdleMs = idleMs
	sum.MaxSpeed = maxSpeed
	if movingMs > 0 {
		sum.AvgSpeed = distance / 1000 / (float64(movingMs) / 3600e3)
	}
	sum.Trips = trips
	sum.Stops = stops
	sum.EngineMs = engineOnMs(points)
	if fuelUnit != nil {
		sum.FuelUsed = ptrF(totalFuelUsed(points, fuelEvents))
	}
	sum.FuelEvents = fuelEvents
	sum.Segments = segments
	return sum
}

// stepDistance is the distance between two points, ignoring impossible GPS jumps.
func stepDistance(a, b *dto.TrackPoint) float64 {
	d := DistanceM(a.Lat, a.Lng, b.Lat, b.Lng)
	hours := float64(b.T-a.T) / 3600e3
	if hours <= 0 {
		return 0
	}
	if d/1000/hours > 180 { // faster than 180 km/h: GPS jump
		return 0
	}
	if a.Speed < 2 && b.Speed < 2 && d < 25 { // both still: small shift = noise
		return 0
	}
	return d
}

// engineOnMs sums intervals whose start point has ignition ON (long data gaps excluded).
func engineOnMs(points []dto.TrackPoint) int64 {
	var ms int64
	for i := 1; i < len(points); i++ {
		dt := points[i].T - points[i-1].T
		if points[i-1].Ign != nil && *points[i-1].Ign && dt <= GapMs {
			ms += dt
		}
	}
	return ms
}

func median(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	return c[len(c)/2]
}

// fuelDrop is the fuel used over one trip (start - end, medians to ride out tank sloshing).
func fuelDrop(slice []dto.TrackPoint) *float64 {
	vals := []float64{}
	for _, p := range slice {
		if p.Fuel != nil {
			vals = append(vals, *p.Fuel)
		}
	}
	if len(vals) < 2 {
		return nil
	}
	k := len(vals) / 5
	if k < 1 {
		k = 1
	}
	used := median(vals[:k]) - median(vals[len(vals)-k:])
	if used > 0 {
		return ptrF(round1(used))
	}
	return ptrF(0)
}

func withFuel(points []dto.TrackPoint) []dto.TrackPoint {
	out := []dto.TrackPoint{}
	for _, p := range points {
		if p.Fuel != nil {
			out = append(out, p)
		}
	}
	return out
}

// detectFuelEvents: refuel = sharp rise; suspected loss = sharp fall while still.
func detectFuelEvents(points []dto.TrackPoint, unit string) []dto.FuelEvent {
	th := fuelJump[unit]
	wf := withFuel(points)
	events := []dto.FuelEvent{}
	for i := 1; i < len(wf); i++ {
		a := wf[i-1]
		b := wf[i]
		if b.T-a.T > fuelWindow {
			continue
		}
		delta := *b.Fuel - *a.Fuel
		typ := ""
		if delta >= th.refuel {
			typ = "refuel"
		} else if -delta >= th.drop && a.Speed < MovingKmh && b.Speed < MovingKmh {
			typ = "drop"
		}
		if typ == "" {
			continue
		}
		if n := len(events); n > 0 && events[n-1].Type == typ && b.T-events[n-1].Time <= fuelWindow {
			last := &events[n-1]
			last.After = *b.Fuel
			last.Amount = round1(math.Abs(last.After - last.Before))
			continue
		}
		events = append(events, dto.FuelEvent{
			Type: typ, Time: b.T, Amount: round1(math.Abs(delta)),
			Before: *a.Fuel, After: *b.Fuel, Lat: b.Lat, Lng: b.Lng,
		})
	}
	return events
}

// totalFuelUsed sums every fall in level, minus the ones flagged as loss.
func totalFuelUsed(points []dto.TrackPoint, events []dto.FuelEvent) float64 {
	wf := withFuel(points)
	used := 0.0
	for i := 1; i < len(wf); i++ {
		if d := *wf[i-1].Fuel - *wf[i].Fuel; d > 0 {
			used += d
		}
	}
	lost := 0.0
	for _, e := range events {
		if e.Type == "drop" {
			lost += e.Amount
		}
	}
	return math.Max(0, round1(used-lost))
}
