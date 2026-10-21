// Package simulator: fake FMC150 devices. They connect over TCP, send Codec 8E data and
// answer Codec 12 commands, so the dashboard can be developed and demoed without real
// hardware. Used by cmd/simulator and by the backend's "simulation" feature (Run).
//
// Trucks assigned in an active shift drive loading → unloading of that route and back
// ALONG ROADS (OpenStreetMap), stopping ~3 minutes at each site. Routes are read from the
// database every 20 s, so changing a route needs no restart. Unassigned trucks park with
// the engine off. SIM_LOOP=1: unassigned trucks loop around central Jakarta instead.
// A stopped shift: each truck finishes its ritase (still loading or returning: it loads once
// more), dumps, then parks.
//
// The other kinds follow the routes in use: EX works at front loading (of its fleet when set
// in Fleet → excavator), LV drives between front loading and dumping, SUPPORT drives the
// haul road slowly.
//
// Port of fmctest/server/simulator.ts.
package simulator

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/amirullazmi0/fms-backend/pkg/routing"
	"github.com/amirullazmi0/fms-backend/pkg/teltonika"
	"github.com/lib/pq"
)

type latLng [2]float64

// Default loop through central/south Jakarta.
var loopRoute = []latLng{
	{-6.1754, 106.8272}, {-6.1862, 106.8233}, {-6.195, 106.823}, {-6.2088, 106.82},
	{-6.22, 106.812}, {-6.227, 106.802}, {-6.244, 106.8}, {-6.242, 106.815},
	{-6.23, 106.83}, {-6.224, 106.844}, {-6.21, 106.85}, {-6.196, 106.842},
	{-6.188, 106.84}, {-6.18, 106.833},
}

// Settings of the current run (one run at a time, see Run).
var (
	simLoop   bool
	blankSpot bool
	queueMode bool
	interval  = 3
	host      = "127.0.0.1"
	port      = "3101"
	graph     *routing.RoadGraph
	logf      = func(format string, args ...any) { fmt.Printf(format+"\n", args...) }
)

const (
	// coverageM: with BlankSpot, there is signal only this close to the loading site (the pit
	// office / shift assign point) and when parked (garage).
	coverageM = 500
	// maxBatch: records per Codec 8E packet when flushing the buffer (the count is one byte).
	maxBatch = 50
	// queueGapM: in queue mode, a truck waits this far before a spot another truck is using.
	queueGapM = 60
	// exitDumpM: after the last dumping of a stopped shift, park this far from the dumping point
	// (outside its radius, so the departure is recorded).
	exitDumpM = 400
)

// spots: loading / dumping positions in use (queue mode). key = point, value = imei.
var spots sync.Map

func spotKey(p latLng) string { return fmt.Sprintf("%.5f,%.5f", p[0], p[1]) }

// path is the polyline one truck follows. After the last point it returns to loopFrom.
type path struct {
	key      string
	name     string
	points   []latLng
	loopFrom int
	depots   map[int]bool // indexes where the truck stops to load/unload
	unload   int          // index of the unloading site (-1 = none)
}

var loopPath = &path{key: "loop", name: "loop Jakarta", points: loopRoute, depots: map[int]bool{}, unload: -1}

func ll(p latLng) routing.LatLng { return routing.LatLng{Lat: p[0], Lng: p[1]} }

func coords(r *routing.RoadResult) []latLng {
	out := make([]latLng, len(r.Coords))
	for i, c := range r.Coords {
		out[i] = latLng(c)
	}
	return out
}

// approachWait: how long a truck waits for the road path from where it stands to the
// loading site. That leg often needs fresh road data (Overpass can take minutes or time
// out); after this the truck starts at the loading site and drives the route loop.
const approachWait = 20 * time.Second

// routePath: current position → loading → unloading → loading → … along roads.
// Go and return are computed apart (one-way roads may differ). Only the loop legs are
// required; the approach leg is skipped when it is not ready in time.
func routePath(ctx context.Context, key, name string, from, l, u latLng) (*path, error) {
	type result struct {
		r   *routing.RoadResult
		err error
	}
	approach := make(chan result, 1)
	go func() {
		r, err := graph.RoadPath(ctx, ll(from), ll(l))
		approach <- result{r, err}
	}()

	var (
		wg   sync.WaitGroup
		res  [2]*routing.RoadResult
		errs [2]error
		legs = [2][2]latLng{{l, u}, {u, l}}
	)
	for i := range legs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res[i], errs[i] = graph.RoadPath(ctx, ll(legs[i][0]), ll(legs[i][1]))
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	lu, ul := coords(res[0]), coords(res[1])

	gol := []latLng{l} // fallback: start right at the loading site
	if distM(from, l) > 30 {
		select {
		case a := <-approach:
			if a.err == nil {
				gol = coords(a.r)
			} else {
				logf("[%s] jalur ke front loading gagal (%v), mulai dari front loading", name, a.err)
			}
		case <-time.After(approachWait):
			logf("[%s] jalur ke front loading belum terhitung, mulai dari front loading", name)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	points := append(append(append([]latLng{}, gol...), lu[1:]...), ul[1:len(ul)-1]...)
	li := len(gol) - 1     // loading index
	ui := li + len(lu) - 1 // unloading index
	return &path{key: key, name: name, points: points, loopFrom: li, depots: map[int]bool{li: true, ui: true}, unload: ui}, nil
}

func rad(d float64) float64 { return d * math.Pi / 180 }

func distM(a, b latLng) float64 {
	x := math.Pow(math.Sin(rad(b[0]-a[0])/2), 2) + math.Cos(rad(a[0]))*math.Cos(rad(b[0]))*math.Pow(math.Sin(rad(b[1]-a[1])/2), 2)
	return 2 * 6371000 * math.Asin(math.Sqrt(x))
}

func bearing(a, b latLng) int {
	y := math.Sin(rad(b[1]-a[1])) * math.Cos(rad(b[0]))
	x := math.Cos(rad(a[0]))*math.Sin(rad(b[0])) - math.Sin(rad(a[0]))*math.Cos(rad(b[0]))*math.Cos(rad(b[1]-a[1]))
	return int(math.Round(math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)))
}

func rnd(a, b float64) float64 { return a + rand.Float64()*(b-a) }

func io(id int, v float64) teltonika.OutIo {
	return teltonika.OutIo{ID: id, V: int64(math.Round(v)), Size: teltonika.IO[id].Size}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

type greenEvent struct {
	typ int
	g   float64
}

type routeInfo struct {
	id, name string
	l, u     latLng
	// finish: the shift was stopped; drive on to unloading, unload, then park there.
	finish bool
}

// UnitKind is the unit type of a fake device (devices.unit_type).
type UnitKind string

const (
	DT      UnitKind = "DT"      // dump truck: hauls its shift route
	EX      UnitKind = "EX"      // excavator: works at front loading
	LV      UnitKind = "LV"      // light vehicle: drives between front loading and dumping
	SUPPORT UnitKind = "SUPPORT" // grader / water truck: drives the haul road slowly
)

// imeiBase: the IMEI number of the first unit of each kind (DT keeps 1, 2, … as before).
var imeiBase = map[UnitKind]int{DT: 1, EX: 101, LV: 201, SUPPORT: 301}

// Unit is one fake device of a run.
type Unit struct {
	Imei string
	Kind UnitKind
	Name string // shown while the device has no name of its own
}

type fakeDevice struct {
	mu         sync.Mutex
	imei       string
	kind       UnitKind
	working    bool // EX: at front loading, engine on
	heading    int  // EX: swing direction
	path       *path // nil = parked (no route)
	here       latLng
	routeKey   string
	fails      int
	retryAt    time.Time
	lastError  string
	seg        int
	frac       float64
	speed      float64
	ign        bool
	parkTicks  int
	depotTicks int
	finish     bool    // shift stopped: stop after the next unloading
	exitFrom   *latLng // last dumping done: drive out of its radius, then park
	odometer   float64 // metres
	trip       float64
	fuel       float64 // 0.1 L
	fuelUsed   float64
	tick       int
	lastRec    *teltonika.OutRecord
	door       int // DIN2: tipper door
	dout1      int // DOUT1: engine cut
	ibutton    float64
	cargoTemp  float64 // 0.1 °C
	buffer     []teltonika.OutRecord // blank spot: kept until back in coverage
	spot       string                // queue mode: the spot this truck holds (key in spots)
	waiting    bool                  // queue mode: waiting for a spot
}

func imeiOf(n int) string { return "35" + fmt.Sprintf("%013d", n) }

// Units lists the fake devices of a run: DT first, then EX, LV and SUPPORT.
func Units(o Options) []Unit {
	o = withDefaults(o)
	out := []Unit{}
	add := func(kind UnitKind, count int) {
		for i := range count {
			name := fmt.Sprintf("%s-SIM %02d", kind, i+1)
			out = append(out, Unit{Imei: imeiOf(imeiBase[kind] + i), Kind: kind, Name: name})
		}
	}
	add(DT, o.Devices-o.Excavators-o.LightVehicles-o.Supports)
	add(EX, o.Excavators)
	add(LV, o.LightVehicles)
	add(SUPPORT, o.Supports)
	return out
}

func newDevice(n int, kind UnitKind) *fakeDevice {
	d := &fakeDevice{
		imei:      imeiOf(n),
		kind:      kind,
		here:      loopRoute[(n*5)%len(loopRoute)],
		ign:       true,
		odometer:  125000000 + float64(n)*7300000,
		fuel:      450 - float64(n%6)*60,
		fuelUsed:  8000,
		ibutton:   float64(0x0100_0000_0000 + n*0x1234_5678),
		cargoTemp: 50,
	}
	if simLoop && kind != EX {
		d.path, d.routeKey, d.seg = loopPath, "loop", (n*5)%len(loopRoute)
	}
	return d
}

// setSite puts an EX at the front loading of r (nil = no work: parked, engine off). Each EX
// stands a little apart so several on one front do not overlap.
func (d *fakeDevice) setSite(r *routeInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := ""
	if r != nil {
		key = fmt.Sprintf("ex|%s|%v", r.id, r.l)
	}
	if key == d.routeKey {
		return
	}
	d.routeKey = key
	if r == nil {
		d.working, d.ign, d.speed = false, false, 0
		d.logf("tidak ada front loading, parkir di tempat")
		return
	}
	n, _ := strconv.Atoi(d.imei[len(d.imei)-3:])
	deg := (n * 137) % 360 // stable per unit
	d.here = latLng{r.l[0] + 0.0002*math.Cos(rad(float64(deg))), r.l[1] + 0.0002*math.Sin(rad(float64(deg)))}
	d.working, d.ign, d.heading = true, true, deg
	d.logf("bekerja di front loading rute %q", r.name)
}

func (d *fakeDevice) logf(format string, args ...any) {
	logf("[%s] %s", d.imei, fmt.Sprintf(format, args...))
}

func (d *fakeDevice) next(i int) int {
	if i+1 < len(d.path.points) {
		return i + 1
	}
	return d.path.loopFrom
}

func (d *fakeDevice) position() latLng {
	if d.path == nil {
		return d.here
	}
	a := d.path.points[d.seg]
	b := d.path.points[d.next(d.seg)]
	return latLng{a[0] + (b[0]-a[0])*d.frac, a[1] + (b[1]-a[1])*d.frac}
}

// setRoute is called on every database read. nil = not on a route → park.
func (d *fakeDevice) setRoute(ctx context.Context, r *routeInfo) {
	d.mu.Lock()
	d.finish = r != nil && r.finish
	key := ""
	if r != nil {
		key = fmt.Sprintf("%s|%v|%v", r.id, r.l, r.u)
	} else if simLoop {
		key = "loop"
	}
	if key == d.routeKey || (r != nil && time.Now().Before(d.retryAt)) {
		d.mu.Unlock()
		return
	}
	d.routeKey = key
	here := d.position()
	d.here = here
	d.speed = 0
	if r == nil {
		if simLoop {
			d.path, d.ign = loopPath, true
			d.logf("tidak ada rute, berputar di loop bawaan")
		} else {
			d.path, d.ign = nil, false
			d.logf("tidak ada rute, parkir di tempat")
		}
		d.seg, d.frac, d.depotTicks = 0, 0, 0
		d.mu.Unlock()
		return
	}
	// While the path is computed the truck waits (no straight-line shortcut).
	d.path = nil
	if d.fails == 0 {
		d.logf("mengikuti rute %q, menghitung jalur melalui jalan…", r.name)
	}
	d.mu.Unlock()

	p, err := routePath(ctx, key, r.name, here, r.l, r.u)

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.routeKey != key {
		return // route changed meanwhile
	}
	if err != nil {
		// Back off longer after each failure (20 s, 40, 80, … max 5 min) to spare the road server.
		d.fails++
		d.retryAt = time.Now().Add(time.Duration(math.Min(300e3, 20e3*math.Pow(2, float64(d.fails-1)))) * time.Millisecond)
		if err.Error() != d.lastError {
			d.logf("gagal menghitung jalur (%v), parkir sementara, dicoba kembali otomatis", err)
		}
		d.lastError = err.Error()
		d.routeKey = ""
		return
	}
	d.fails, d.lastError = 0, ""
	d.path, d.ign = p, true
	toLoad := 0.0
	for i := 1; i <= p.loopFrom; i++ {
		toLoad += distM(p.points[i-1], p.points[i])
	}
	d.logf("menuju front loading (%.1f km melalui jalan)", toLoad/1000)
	d.seg, d.frac, d.depotTicks = 0, 0, 0
}

// releaseSpot frees the spot this truck holds (queue mode).
func (d *fakeDevice) releaseSpot() {
	if d.spot != "" {
		spots.CompareAndDelete(d.spot, d.imei)
		d.spot = ""
	}
}

// mustQueue: queue mode, the next stop is a spot held by another truck and it is close.
func (d *fakeDevice) mustQueue() bool {
	if !queueMode || d.kind != DT || d.path == nil {
		return false
	}
	next := d.next(d.seg)
	if !d.path.depots[next] {
		return false
	}
	key := spotKey(d.path.points[next])
	if holder, ok := spots.Load(key); !ok || holder == d.imei {
		return false
	}
	return distM(d.position(), d.path.points[next]) <= queueGapM
}

// covered: with BlankSpot, whether the device has signal where it is now.
func (d *fakeDevice) covered() bool {
	if !blankSpot || d.path == nil || d.path.unload < 0 {
		return true
	}
	return distM(d.position(), d.path.points[d.path.loopFrom]) <= coverageM
}

// stepExcavator: an EX at front loading stands still with the engine on and swings.
func (d *fakeDevice) stepExcavator(dt float64) {
	d.speed = 0
	if !d.working {
		d.ign = false
		return
	}
	d.ign = true
	d.heading = (d.heading + int(rnd(-90, 90)) + 360) % 360
	burn := dt * 0.07 // ~25 L/h
	d.fuel = math.Max(40, d.fuel-burn)
	d.fuelUsed += burn
	if d.fuel <= 40 {
		d.fuel = 500 // refuel (fuel truck)
	}
}

func (d *fakeDevice) step(dt float64) *greenEvent {
	stopS := 180.0 // ~3 minutes loading/unloading
	switch d.kind {
	case LV:
		stopS = 30 // a short check at the site
	case SUPPORT:
		stopS = 20
	}
	depotTicks := int(math.Ceil(stopS / float64(interval)))
	if queueMode && d.kind == DT {
		depotTicks *= 2 // longer loading so queues happen with a few trucks
	}
	d.tick++
	door := 0.0
	if d.door != 0 {
		door = 3
	} else {
		door = -0.5
	}
	d.cargoTemp = math.Max(20, math.Min(120, d.cargoTemp+(rand.Float64()-0.5)*4+door))
	// Engine cut active: engine off, truck stops.
	if d.dout1 != 0 {
		d.ign, d.speed = false, 0
		return nil
	}
	if d.kind == EX {
		d.stepExcavator(dt)
		return nil
	}
	if d.path == nil {
		d.releaseSpot()
		d.ign, d.speed, d.door = false, 0, 0
		return nil
	}
	if d.depotTicks > 0 {
		// At a demo site: still, engine on, tipper open.
		d.depotTicks--
		if d.depotTicks == 0 {
			d.releaseSpot()
		}
		if d.depotTicks == 0 && d.finish && d.seg == d.path.unload {
			// Last unloading of a stopped shift: leave the dumping area (so the departure is
			// recorded and the ritase counts as finished), then park.
			at := d.path.points[d.seg]
			d.exitFrom = &at
			d.logf("dumping terakhir selesai, keluar dari area dumping")
		}
		if d.depotTicks > 2 && d.kind == DT {
			d.door = 1 // tipper
		} else {
			d.door = 0
		}
		d.speed = 0
		return nil
	}
	if d.exitFrom != nil && distM(d.position(), *d.exitFrom) > exitDumpM {
		// Out of the dumping area after the last ritase: park here (routeKey kept, so the same
		// route is not started again on the next sync).
		d.here, d.path, d.exitFrom, d.ign, d.door = d.position(), nil, nil, false, 0
		d.logf("ritase terakhir selesai, parkir di luar area dumping")
		d.speed = 0
		return nil
	}
	if d.parkTicks > 0 {
		d.parkTicks--
		// While parked: door sometimes opens, and now and then fuel drops (simulated theft).
		if d.parkTicks == 10 {
			d.door = 0
			if rand.Float64() < 0.6 {
				d.door = 1
			}
		}
		if d.parkTicks == 4 {
			d.door = 0
		}
		if d.parkTicks == 7 && rand.Float64() < 0.3 {
			d.fuel = math.Max(40, d.fuel-80)
		}
		if d.parkTicks == 0 {
			d.ign, d.trip = true, 0
		}
		d.speed = 0
		return nil
	}
	if d.mustQueue() {
		if !d.waiting {
			d.logf("antre, spot sedang dipakai unit lain")
		}
		d.waiting, d.speed = true, 0
		return nil
	}
	d.waiting = false
	if rand.Float64() < 0.005 {
		d.ign, d.parkTicks, d.speed = false, 12, 0
		return nil
	}

	target := rnd(15, 65)
	switch {
	case d.kind == SUPPORT:
		target = rnd(8, 20) // grader / water truck: slow, no overspeed
	case d.kind == LV:
		target = rnd(30, 60)
		if rand.Float64() < 0.05 {
			target = rnd(82, 95)
		}
	case rand.Float64() < 0.08:
		target = rnd(82, 95)
	}
	var ev *greenEvent
	prev := d.speed
	d.speed = math.Max(0, math.Min(100, d.speed+(target-d.speed)*0.35))
	if d.speed-prev > 18 {
		ev = &greenEvent{1, rnd(0.35, 0.6)}
	}
	if prev-d.speed > 18 {
		ev = &greenEvent{2, rnd(0.4, 0.8)}
	}
	if ev == nil && rand.Float64() < 0.03 {
		ev = &greenEvent{3, rnd(0.3, 0.5)}
	}

	move := d.speed / 3.6 * dt
	d.odometer += move
	d.trip += move
	for move > 0 {
		a := d.path.points[d.seg]
		b := d.path.points[d.next(d.seg)]
		l := math.Max(1, distM(a, b))
		left := l * (1 - d.frac)
		if move < left {
			d.frac += move / l
			move = 0
		} else {
			move -= left
			d.seg = d.next(d.seg)
			d.frac = 0
			if d.path.depots[d.seg] {
				d.depotTicks = depotTicks // stop right at the site
				move = 0
				if queueMode && d.kind == DT {
					d.spot = spotKey(d.path.points[d.seg])
					spots.Store(d.spot, d.imei)
				}
			} else if queueMode && d.mustQueue() {
				move = 0 // reached the end of the queue
			}
		}
	}
	burn := d.speed * dt * 0.0012
	d.fuel = math.Max(40, d.fuel-burn)
	d.fuelUsed += burn
	if d.fuel <= 40 {
		d.fuel = 500 // refuel
	}
	return ev
}

func (d *fakeDevice) record(ev *greenEvent) teltonika.OutRecord {
	a, b := d.here, d.here
	if d.path != nil {
		a, b = d.path.points[d.seg], d.path.points[d.next(d.seg)]
	}
	p := d.position()
	lat := p[0] + rnd(-0.00003, 0.00003)
	lng := p[1] + rnd(-0.00003, 0.00003)
	speed := int(math.Round(d.speed))
	moving := speed > 2
	rpm := 0.0
	if d.ign {
		rpm = 780
		if moving {
			rpm = 900 + float64(speed)*32
		} else if d.kind == EX {
			rpm = rnd(1500, 1900) // digging / swinging
		}
	}
	ext := rnd(12300, 12600)
	coolant, fuelRate := 450.0, 0.0
	if d.ign {
		ext, coolant, fuelRate = rnd(13600, 14100), rnd(870, 935), rnd(8, 45)
	}
	pedal := 0.0
	if moving {
		pedal = rnd(10, 45)
	}
	entries := []teltonika.OutIo{
		io(239, b2f(d.ign)), io(240, b2f(moving)), io(80, b2f(moving)), io(21, rnd(3, 5.4)),
		io(200, 0), io(69, 1), io(181, rnd(9, 16)), io(182, rnd(6, 12)),
		io(66, ext), io(67, rnd(4050, 4150)), io(68, 0), io(113, 96),
		io(24, float64(speed)), io(16, d.odometer), io(199, d.trip), io(1, b2f(d.ign)), io(179, float64(d.dout1)),
		io(241, 51010), io(205, float64(21000+(d.seg*37)%900)), io(206, 1432),
		io(81, float64(speed)), io(82, pedal), io(84, d.fuel), io(89, d.fuel/500*100),
		io(85, rpm), io(87, d.odometer), io(115, coolant), io(110, fuelRate),
		io(83, d.fuelUsed),
		io(2, float64(d.door)), io(3, 0), io(180, 0), io(9, d.fuel*10), io(72, d.cargoTemp),
		io(78, d.ibutton),
	}
	eventIo, priority := 0, 0
	switch {
	case ev != nil:
		entries = append(entries, io(253, float64(ev.typ)), io(254, ev.g*100))
		eventIo, priority = 253, 1
	case speed > 80:
		entries = append(entries, io(255, float64(speed)))
		eventIo, priority = 255, 1
	case d.parkTicks == 11 || (d.ign && d.tick == 1):
		eventIo = 239
	}

	angle := 0
	if d.kind == EX {
		angle = d.heading
	} else if a == b {
		if d.lastRec != nil {
			angle = d.lastRec.Angle
		}
	} else {
		angle = bearing(a, b)
	}
	return teltonika.OutRecord{
		Time: time.Now().UnixMilli(), Priority: priority, Lat: lat, Lng: lng,
		Alt: int(math.Round(rnd(8, 25))), Angle: angle, Sats: int(math.Round(rnd(9, 15))),
		Speed: speed, EventIo: eventIo, Io: entries,
	}
}

func (d *fakeDevice) reply(cmd string) string {
	c := strings.ToLower(strings.TrimSpace(cmd))
	p := d.lastRec
	uptime := d.tick * interval
	switch {
	case c == "getstatus":
		return "Data Link: 1 GPRS: 1 Phone: 0 SIM: 0 OP: 51010 Signal: 5 NewSMS: 0 Roaming: 0 SMSFull: 0 LAC: 1432 Cell ID: 21037 NetType: 1 FwUpd:-1"
	case c == "getver":
		return fmt.Sprintf("Ver:03.01.00.Rev.00 GPS:AXN_5.10 Hw:FMC150 Mod:SIM IMEI:%s Init: 2026-9-27 Uptime: %d MAC: 000000000000 SPC:1(0) AXL:1 OBD:0 BL:1.10 BT:4", d.imei, uptime)
	case c == "getgps" && p != nil:
		return fmt.Sprintf("GPS:1 Sat:%d Lat:%.6f Long:%.6f Alt:%d Speed:%d Dir:%d Time: %s", p.Sats, p.Lat, p.Lng, p.Alt, p.Speed, p.Angle, time.Now().UTC().Format("15:04:05"))
	case c == "getinfo":
		sats := 0
		if p != nil {
			sats = p.Sats
		}
		return fmt.Sprintf("RTC:%s UpTime:%ds PWR:PwrVoltage RST:0 GPS:1 SAT:%d TTFF:22 REC:0 MD:0 DB:0", time.Now().UTC().Format(time.RFC3339), uptime, sats)
	case strings.HasPrefix(c, "setdigout"):
		bits := "00"
		if f := strings.Fields(c); len(f) > 1 {
			bits = f[1]
		}
		if bits[0] == '0' || bits[0] == '1' {
			d.dout1 = int(bits[0] - '0')
		}
		second := "0"
		if len(bits) > 1 {
			second = bits[1:2]
		}
		return fmt.Sprintf("DOUT1:%d DOUT2:%s", d.dout1, second)
	}
	return fmt.Sprintf("SIM: perintah %q diterima (simulator)", cmd)
}

func (d *fakeDevice) send(c net.Conn) error {
	d.mu.Lock()
	ev := d.step(float64(interval))
	parkedEvery := max(1, int(math.Round(60/float64(interval))))
	if d.path == nil && !d.ign && d.tick%parkedEvery != 1 {
		d.mu.Unlock()
		return nil // parked: send rarely, like a real device
	}
	rec := d.record(ev)
	d.lastRec = &rec
	if !d.covered() {
		// No signal: keep it in memory like the device does, send when back in coverage.
		if len(d.buffer) == 0 {
			d.logf("masuk blank spot, data disimpan di alat")
		}
		d.buffer = append(d.buffer, rec)
		d.mu.Unlock()
		return nil
	}
	batch := append(d.buffer, rec)
	if len(d.buffer) > 0 {
		d.logf("masuk area sinyal, kirim %d record tertunda", len(d.buffer))
	}
	d.buffer = nil
	d.mu.Unlock()
	for len(batch) > 0 {
		n := min(len(batch), maxBatch)
		if _, err := c.Write(teltonika.BuildAvl8E(batch[:n])); err != nil {
			return err
		}
		batch = batch[n:]
	}
	return nil
}

func (d *fakeDevice) run(ctx context.Context) {
	for ctx.Err() == nil {
		d.session(ctx)
		d.logf("terputus, menyambung ulang dalam 5 detik")
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (d *fakeDevice) session(ctx context.Context) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
	if err != nil {
		d.logf("%v", err)
		return
	}
	defer c.Close()
	id := []byte(d.imei)
	head := make([]byte, 2)
	binary.BigEndian.PutUint16(head, uint16(len(id)))
	if _, err := c.Write(append(head, id...)); err != nil {
		return
	}
	d.logf("terhubung ke %s:%s", host, port)
	ack := make([]byte, 1)
	if _, err := c.Read(ack); err != nil || ack[0] != 0x01 {
		d.logf("ditolak server")
		return
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(time.Duration(interval) * time.Second)
		defer t.Stop()
		_ = d.send(c)
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				_ = c.Close()
				return
			case <-t.C:
				if d.send(c) != nil {
					return
				}
			}
		}
	}()

	buf := []byte{}
	chunk := make([]byte, 4096)
	for {
		n, err := c.Read(chunk)
		if err != nil {
			return
		}
		buf = append(buf, chunk[:n]...)
		for len(buf) >= 4 {
			if binary.BigEndian.Uint32(buf[:4]) != 0 {
				buf = buf[4:] // ACK with the record count
				continue
			}
			frame, rest, ok, ferr := teltonika.TakeFrame(buf)
			if ferr != nil {
				return
			}
			if !ok {
				break
			}
			buf = append([]byte{}, rest...)
			if teltonika.CodecID(frame) == teltonika.Codec12 {
				_, text, err := teltonika.ParseCodec12(frame)
				if err != nil {
					continue
				}
				d.mu.Lock()
				answer := d.reply(text)
				d.mu.Unlock()
				d.logf("perintah %q -> %.60s", text, answer)
				go func() {
					time.Sleep(400 * time.Millisecond)
					_, _ = c.Write(teltonika.BuildCodec12(answer, teltonika.Codec12Response))
				}()
			}
		}
	}
}

// finishingTrip mirrors repository.FinishingTrip (sh = the closed shift): the last ritase is not
// finished yet (still hauling, or still at dumping), or the truck was returning to / loading at
// front loading when the shift closed and loads once more, hauls and dumps.
const finishingTrip = `(EXISTS (SELECT 1 FROM trips t WHERE t.assignment_id = a.id AND t.leg = 'go'
	AND t.depart_at = (SELECT max(t2.depart_at) FROM trips t2 WHERE t2.assignment_id = a.id)
	AND (t.arrive_at IS NULL OR EXISTS (SELECT 1 FROM site_visits sv WHERE sv.assignment_id = a.id
		AND sv.site_id = t.unloading_site_id AND sv.arrive_at >= t.depart_at AND sv.depart_at IS NULL)))
	OR (sh.closed_at > now() - interval '1 hour'
	AND EXISTS (SELECT 1 FROM trips t WHERE t.assignment_id = a.id AND t.leg = 'back' AND t.depart_at <= sh.closed_at
		AND t.depart_at = (SELECT max(t2.depart_at) FROM trips t2 WHERE t2.assignment_id = a.id AND t2.depart_at <= sh.closed_at))
	AND NOT EXISTS (SELECT 1 FROM trips t WHERE t.assignment_id = a.id AND t.depart_at > sh.closed_at)
	AND NOT EXISTS (SELECT 1 FROM assignments a2 JOIN shifts s2 ON s2.id = a2.shift_id AND s2.status = 'active'
		AND s2.deleted_at IS NULL WHERE a2.imei = a.imei AND a2.end_at IS NULL)))`

// syncRoutes reads each simulated truck's route from the active shift assignments. The other
// units follow the routes in use: EX at front loading, LV and SUPPORT on the haul road.
func syncRoutes(ctx context.Context, db *sql.DB, fleet []*fakeDevice) {
	imeis := []string{}
	for _, d := range fleet {
		if d.kind == DT {
			imeis = append(imeis, d.imei)
		}
	}
	warned := false
	for {
		rows, err := database.NewQueryBuilder("assignments a").
			// Active shift, or a stopped one whose last ritase is not finished yet (finish it).
			Select("DISTINCT ON (a.imei) a.imei", "r.id", "r.name", "l.lat", "l.lng", "u.lat", "u.lng", "sh.status = 'closed'").
			InnerJoin("shifts sh", `sh.id = a.shift_id AND sh.deleted_at IS NULL AND (sh.status = 'active' OR (sh.status = 'closed'
				AND sh.closed_at > now() - interval '6 hours' AND `+finishingTrip+`))`).
			InnerJoin("routes r", "r.id = a.route_id").
			InnerJoin("sites l", "l.id = r.loading_site_id").
			InnerJoin("sites u", "u.id = r.unloading_site_id").
			WhereNull("a.end_at").
			Where("a.imei = ANY($1::text[])", pq.Array(imeis)).
			OrderBy("a.imei").
			OrderBy("sh.status = 'closed'"). // an active assignment wins over finishing an old one
			OrderBy("COALESCE(a.start_at, sh.started_at) DESC").
			ExecuteContext(ctx, db)
		if err != nil {
			if !warned {
				logf("Gagal baca rute dari database (%v), unit tetap di jalur sekarang", err)
			}
			warned = true
		} else {
			warned = false
			by := map[string]*routeInfo{}
			for rows.Next() {
				var imei string
				var r routeInfo
				if rows.Scan(&imei, &r.id, &r.name, &r.l[0], &r.l[1], &r.u[0], &r.u[1], &r.finish) == nil {
					by[imei] = &r
				}
			}
			_ = rows.Close()
			for _, d := range fleet {
				if d.kind == DT {
					go d.setRoute(ctx, by[d.imei])
				}
			}
			syncOthers(ctx, db, fleet)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Second):
		}
	}
}

// syncOthers places the EX, LV and SUPPORT units. An EX works at the front loading of its fleet
// (Fleet → excavator), else of a route in use. LV and SUPPORT drive the routes in use (one route
// each, in turn). No route in use: they park (LV / SUPPORT loop with Loop).
func syncOthers(ctx context.Context, db *sql.DB, fleet []*fakeDevice) {
	imeis := []string{}
	for _, d := range fleet {
		if d.kind != DT {
			imeis = append(imeis, d.imei)
		}
	}
	if len(imeis) == 0 {
		return
	}
	// scan reads route rows, each optionally prefixed by an imei.
	scan := func(rows *sql.Rows, withImei bool) ([]string, []*routeInfo) {
		var keys []string
		var out []*routeInfo
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var imei string
			var r routeInfo
			dest := []any{&r.id, &r.name, &r.l[0], &r.l[1], &r.u[0], &r.u[1]}
			if withImei {
				dest = append([]any{&imei}, dest...)
			}
			if rows.Scan(dest...) == nil {
				keys, out = append(keys, imei), append(out, &r)
			}
		}
		return keys, out
	}
	rows, err := database.NewQueryBuilder("routes r").
		Select("r.id", "r.name", "l.lat", "l.lng", "u.lat", "u.lng").
		InnerJoin("sites l", "l.id = r.loading_site_id").
		InnerJoin("sites u", "u.id = r.unloading_site_id").
		WhereNull("r.deleted_at").
		Where(`EXISTS (SELECT 1 FROM assignments a JOIN shifts sh ON sh.id = a.shift_id AND sh.status = 'active'
			AND sh.deleted_at IS NULL WHERE a.route_id = r.id AND a.end_at IS NULL)`).
		OrderBy("r.name").
		OrderBy("r.id").
		ExecuteContext(ctx, db)
	if err != nil {
		return
	}
	_, inUse := scan(rows, false)
	rows, err = database.NewQueryBuilder("fleet_loaders fl").
		Select("DISTINCT ON (fl.imei) fl.imei", "r.id", "r.name", "l.lat", "l.lng", "u.lat", "u.lng").
		InnerJoin("routes r", "r.id = fl.route_id AND r.deleted_at IS NULL").
		InnerJoin("sites l", "l.id = r.loading_site_id").
		InnerJoin("sites u", "u.id = r.unloading_site_id").
		WhereNull("fl.end_at").
		Where("fl.imei = ANY($1::text[])", pq.Array(imeis)).
		OrderBy("fl.imei").
		OrderBy("fl.start_at DESC").
		ExecuteContext(ctx, db)
	if err != nil {
		return
	}
	keys, list := scan(rows, true)
	loaders := map[string]*routeInfo{}
	for i, k := range keys {
		loaders[k] = list[i]
	}

	nth := func(i int) *routeInfo {
		if len(inUse) == 0 {
			return nil
		}
		return inUse[i%len(inUse)]
	}
	count := map[UnitKind]int{}
	for _, d := range fleet {
		i := count[d.kind]
		count[d.kind]++
		switch d.kind {
		case EX:
			r := loaders[d.imei]
			if r == nil {
				r = nth(i)
			}
			d.setSite(r)
		case LV:
			go d.setRoute(ctx, nth(i))
		case SUPPORT:
			go d.setRoute(ctx, nth(i+1))
		}
	}
}

// Options for one simulator run.
type Options struct {
	Devices     int    // number of fake units, all kinds (default 2)
	// Of Devices: excavators, light vehicles and support units (grader / water truck); the
	// rest are dump trucks. See Mix for a default composition.
	Excavators    int
	LightVehicles int
	Supports      int
	Host        string // device TCP host (default 127.0.0.1)
	Port        string // device TCP port (default 3101)
	Interval    int    // seconds between records (default 3)
	Loop        bool   // unassigned trucks loop around central Jakarta instead of parking
	BlankSpot   bool   // signal only near the loading site: records sent late, in bulk
	Queue       bool   // trucks queue before a busy loading / dumping spot
	DataDir     string // road graph cache
	OverpassURL string
	// DB: routes of active shift assignments are read from here every 20 s.
	// nil = trucks never get a route (they park, or loop with Loop).
	DB *sql.DB
	// RoadStore keeps computed road paths (nil = files under DataDir/roads).
	RoadStore routing.Store
	Logf      func(format string, args ...any)
}

// Mix is the default composition of n units: 1 EX per 5, 1 LV and 1 SUPPORT per 10
// (10 units = 6 DT, 2 EX, 1 LV, 1 SUPPORT).
func Mix(n int) (excavators, lightVehicles, supports int) {
	return n / 5, n / 10, n / 10
}

// withDefaults fills the device count and keeps the other kinds within it.
func withDefaults(o Options) Options {
	if o.Devices <= 0 {
		o.Devices = 2
	}
	left := o.Devices
	for _, n := range []*int{&o.Excavators, &o.LightVehicles, &o.Supports} {
		*n = max(0, min(*n, left))
		left -= *n
	}
	return o
}

// ErrRunning is returned by Run while another run is active.
var ErrRunning = errors.New("simulator already running")

var running atomic.Bool

// Running reports whether a run is active in this process.
func Running() bool { return running.Load() }

// Run connects the fake trucks and blocks until ctx is cancelled. Only one run at a time.
func Run(ctx context.Context, o Options) error {
	if !running.CompareAndSwap(false, true) {
		return ErrRunning
	}
	defer running.Store(false)

	simLoop, blankSpot, queueMode, interval, host, port = o.Loop, o.BlankSpot, o.Queue, 3, "127.0.0.1", "3101"
	spots.Clear()
	if o.Interval > 0 {
		interval = o.Interval
	}
	if o.Host != "" {
		host = o.Host
	}
	if o.Port != "" {
		port = o.Port
	}
	if o.Logf != nil {
		logf = o.Logf
	}
	dataDir := o.DataDir
	if dataDir == "" {
		dataDir = "data"
	}
	graph = routing.NewRoadGraph(o.OverpassURL, dataDir)
	if o.RoadStore != nil {
		graph.WithStore(o.RoadStore)
	}
	var wg sync.WaitGroup
	units := Units(o)
	fleet := make([]*fakeDevice, len(units))
	for i, u := range units {
		n, _ := strconv.Atoi(u.Imei[2:])
		fleet[i] = newDevice(n, u.Kind)
	}
	// Continue where each truck last was (a server restart must not teleport the trucks
	// and cut their ritase in half).
	if o.DB != nil {
		resumePositions(ctx, o.DB, fleet)
	}
	for i := range fleet {
		wg.Add(1)
		go func(d *fakeDevice) {
			defer wg.Done()
			d.run(ctx)
		}(fleet[i])
	}
	if o.DB != nil {
		go syncRoutes(ctx, o.DB, fleet)
	} else {
		logf("Database tidak dipasang: rute dari dashboard tidak dibaca")
	}
	wg.Wait()
	return nil
}

// resumePositions puts each truck at its last recorded position (and odometer/fuel when known).
func resumePositions(ctx context.Context, db *sql.DB, fleet []*fakeDevice) {
	imeis := make([]string, len(fleet))
	by := map[string]*fakeDevice{}
	for i, d := range fleet {
		imeis[i] = d.imei
		by[d.imei] = d
	}
	rows, err := database.NewQueryBuilder("records r").
		Select("DISTINCT ON (r.imei) r.imei", "r.lat", "r.lng", "r.io->>'16'", "r.io->>'84'").
		Where("r.imei = ANY($1::text[])", pq.Array(imeis)).
		Where("r.valid").
		Where("r.time > now() - interval '7 days'").
		OrderBy("r.imei").
		OrderBy("r.time DESC").
		ExecuteContext(ctx, db)
	if err != nil {
		logf("Posisi terakhir unit tidak terbaca (%v), mulai dari posisi bawaan", err)
		return
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var imei string
		var lat, lng float64
		var odo, fuel sql.NullString
		if rows.Scan(&imei, &lat, &lng, &odo, &fuel) != nil {
			continue
		}
		d := by[imei]
		if d == nil {
			continue
		}
		d.here = latLng{lat, lng}
		if v, err := strconv.ParseFloat(odo.String, 64); odo.Valid && err == nil && v > 0 {
			d.odometer = v
		}
		if v, err := strconv.ParseFloat(fuel.String, 64); fuel.Valid && err == nil && v > 0 {
			d.fuel = v
		}
		if simLoop {
			d.path, d.routeKey = nil, "" // loop restarts from here on the first route sync
		}
		n++
	}
	if n > 0 {
		logf("%d unit lanjut dari posisi terakhirnya", n)
	}
}
