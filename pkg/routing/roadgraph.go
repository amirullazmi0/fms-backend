package routing

// Road path computed locally from OpenStreetMap road data (Overpass API). Used when
// OSRM is unreachable, and by the simulator so trucks follow real roads.
//
// How: fetch every road in a box around both points, build a graph (junction = node,
// road piece = edge, one-ways respected), then find the fastest path with A*.
// Paths are cached under <dataDir>/roads.

import (
	"container/heap"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	roadCacheVersion = 2
	areaTTL          = 7 * 24 * time.Hour // road data per area kept a week
	failRetry        = 2 * time.Minute    // after a failure, wait before asking again (free Overpass rate-limits)
	maxSpeedKmh      = 70.0
)

// Rough speed per road type (km/h) to pick the fastest path.
var roadSpeed = map[string]float64{
	"motorway": 70, "trunk": 55, "primary": 45, "secondary": 40, "tertiary": 35, "unclassified": 30,
	"residential": 25, "living_street": 15, "service": 12,
	"motorway_link": 45, "trunk_link": 40, "primary_link": 35, "secondary_link": 30, "tertiary_link": 28,
}

var minorRoads = map[string]bool{"residential": true, "living_street": true, "service": true, "unclassified": true}

// LatLng is a point.
type LatLng struct {
	Lat, Lng float64
}

// RoadResult is a path starting at `from` and ending at `to`.
type RoadResult struct {
	Coords    [][2]float64 `json:"coords"` // [lat, lng]
	Distance  float64      `json:"distance"`
	DurationS float64      `json:"durationS"` // regular vehicle estimate
}

func rad(d float64) float64 { return d * math.Pi / 180 }

func dist(a, b LatLng) float64 {
	x := math.Pow(math.Sin(rad(b.Lat-a.Lat)/2), 2) + math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Pow(math.Sin(rad(b.Lng-a.Lng)/2), 2)
	return 2 * 6371000 * math.Asin(math.Sqrt(x))
}

type osmElement struct {
	Type  string            `json:"type"`
	ID    int64             `json:"id"`
	Lat   float64           `json:"lat"`
	Lon   float64           `json:"lon"`
	Nodes []int64           `json:"nodes"`
	Tags  map[string]string `json:"tags"`
}

type memoEntry struct {
	done chan struct{}
	val  any
	err  error
	at   time.Time
	gen  int64 // cacheGen when computed; stale after Invalidate
}

// RoadGraph computes paths over OSM roads.
type RoadGraph struct {
	overpass []string
	cacheDir string
	store    Store // nil = files under cacheDir
	client   *http.Client

	overpassMu sync.Mutex // one Overpass request at a time (be polite to the free server)
	mu         sync.Mutex
	areas      map[string]*memoEntry
	paths      map[string]*memoEntry
}

func NewRoadGraph(overpassURL, dataDir string) *RoadGraph {
	urls := []string{"https://overpass-api.de/api/interpreter", "https://overpass.kumi.systems/api/interpreter"}
	if overpassURL != "" {
		urls = []string{overpassURL}
	}
	return &RoadGraph{
		overpass: urls,
		cacheDir: filepath.Join(dataDir, "roads"),
		client:   &http.Client{Timeout: 90 * time.Second},
		areas:    map[string]*memoEntry{},
		paths:    map[string]*memoEntry{},
	}
}

// memo runs fn once per key; failures are forgotten after failRetry.
func (g *RoadGraph) memo(m map[string]*memoEntry, key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	e, ok := m[key]
	gen := cacheGen.Load()
	if ok && e.err != nil && time.Since(e.at) > failRetry {
		ok = false
	}
	if ok && e.gen != gen {
		select {
		case <-e.done: // finished before the data was deleted: compute again
			ok = false
		default:
		}
	}
	if !ok {
		e = &memoEntry{done: make(chan struct{}), gen: gen}
		m[key] = e
		g.mu.Unlock()
		e.val, e.err = fn()
		e.at = time.Now()
		close(e.done)
		return e.val, e.err
	}
	g.mu.Unlock()
	<-e.done
	return e.val, e.err
}

func shortHash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

// areaRoads: road data per area. The box snaps to a ±2 km grid so nearby routes share
// data, kept in memory + on disk for a week, so Overpass is asked once.
func (g *RoadGraph) areaRoads(ctx context.Context, bbox [4]float64, includeMinor bool) ([]osmElement, error) {
	const grid = 0.02
	snapped := [4]float64{
		math.Floor(bbox[0]/grid) * grid, math.Floor(bbox[1]/grid) * grid,
		math.Ceil(bbox[2]/grid) * grid, math.Ceil(bbox[3]/grid) * grid,
	}
	parts := make([]string, 4)
	for i, x := range snapped {
		parts[i] = trimFloat(x, 4)
		snapped[i], _ = parseFloat(parts[i])
	}
	kind := "major"
	if includeMinor {
		kind = "all"
	}
	key := strings.Join(parts, ",") + "|" + kind
	v, err := g.memo(g.areas, key, func() (any, error) {
		if els, ok := g.loadArea(ctx, key, snapped, kind); ok {
			return els, nil
		}
		els, err := g.fetchRoads(ctx, snapped, includeMinor)
		if err != nil {
			return nil, err
		}
		g.saveArea(ctx, key, snapped, kind, els)
		return els, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]osmElement), nil
}

func (g *RoadGraph) fetchRoads(ctx context.Context, bbox [4]float64, includeMinor bool) ([]osmElement, error) {
	kinds := []string{}
	for k := range roadSpeed {
		if includeMinor || !minorRoads[k] {
			kinds = append(kinds, k)
		}
	}
	sort.Strings(kinds)
	box := fmt.Sprintf("%s,%s,%s,%s", trimFloat(bbox[0], 5), trimFloat(bbox[1], 5), trimFloat(bbox[2], 5), trimFloat(bbox[3], 5))
	q := fmt.Sprintf(`[out:json][timeout:60];way[highway~"^(%s)$"][access!~"^(no|private)$"](%s);out body;>;out skel qt;`, strings.Join(kinds, "|"), box)

	g.overpassMu.Lock()
	defer g.overpassMu.Unlock()

	// Free Overpass servers are often busy (429/504): retry a few times, alternating servers.
	var last error
	for i := 0; i < 4; i++ {
		els, err := g.overpassOnce(ctx, g.overpass[i%len(g.overpass)], q)
		if err == nil {
			return els, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(4*(i+1)) * time.Second):
		}
	}
	return nil, last
}

func (g *RoadGraph) overpassOnce(ctx context.Context, endpoint, q string) ([]osmElement, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("data="+url.QueryEscape(q)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "fms-backend")
	res, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		busy := ""
		if res.StatusCode == 429 || res.StatusCode == 504 {
			busy = " (lagi sibuk)"
		}
		return nil, fmt.Errorf("Overpass balas %d%s", res.StatusCode, busy)
	}
	var body struct {
		Elements []osmElement `json:"elements"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Elements, nil
}

// ---------- Graph + A* ----------

type edge struct {
	to        int64
	cost, len float64
}

type graph struct {
	pos   map[int64]LatLng
	edges map[int64][]edge
}

func buildGraph(elements []osmElement, ignoreOneway bool) *graph {
	g := &graph{pos: map[int64]LatLng{}, edges: map[int64][]edge{}}
	for _, e := range elements {
		if e.Type == "node" {
			g.pos[e.ID] = LatLng{e.Lat, e.Lon}
		}
	}
	add := func(a, b int64, speed float64) {
		pa, ok1 := g.pos[a]
		pb, ok2 := g.pos[b]
		if !ok1 || !ok2 {
			return
		}
		l := dist(pa, pb)
		g.edges[a] = append(g.edges[a], edge{to: b, len: l, cost: l / (speed / 3.6)})
	}
	for _, e := range elements {
		if e.Type != "way" {
			continue
		}
		hw := e.Tags["highway"]
		speed, ok := roadSpeed[hw]
		if !ok {
			speed = 20
		}
		ow := e.Tags["oneway"]
		oneway := !ignoreOneway && (ow == "yes" || ow == "1" || ow == "true" || hw == "motorway" || e.Tags["junction"] == "roundabout")
		rev := ow == "-1" && !ignoreOneway
		for i := 1; i < len(e.Nodes); i++ {
			a, b := e.Nodes[i-1], e.Nodes[i]
			if !rev {
				add(a, b, speed)
			}
			if rev || !oneway {
				add(b, a, speed)
			}
		}
	}
	return g
}

// mainNetwork is the largest connected road network (direction ignored). Points in
// closed areas (car parks, compounds) may sit on a road piece cut off from the main
// roads; start/end nodes are only searched in this network.
func mainNetwork(g *graph) map[int64]bool {
	adj := map[int64][]int64{}
	keys := []int64{}
	link := func(a, b int64) {
		if _, ok := adj[a]; !ok {
			keys = append(keys, a)
		}
		adj[a] = append(adj[a], b)
	}
	for a, list := range g.edges {
		for _, e := range list {
			link(a, e.to)
			link(e.to, a)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	seen := map[int64]bool{}
	var best []int64
	for _, start := range keys {
		if seen[start] {
			continue
		}
		comp := []int64{}
		stack := []int64{start}
		seen[start] = true
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			comp = append(comp, id)
			for _, n := range adj[id] {
				if !seen[n] {
					seen[n] = true
					stack = append(stack, n)
				}
			}
		}
		if len(comp) > len(best) {
			best = comp
		}
	}
	out := map[int64]bool{}
	for _, id := range best {
		out[id] = true
	}
	return out
}

func nearest(g *graph, p LatLng, allowed map[int64]bool) (int64, bool) {
	var best int64
	bestD := math.Inf(1)
	found := false
	for id := range allowed {
		q, ok := g.pos[id]
		if !ok {
			continue
		}
		if d := dist(p, q); d < bestD || (d == bestD && id < best) {
			bestD, best, found = d, id, true
		}
	}
	return best, found
}

type pqItem struct {
	f  float64
	id int64
}
type minHeap []pqItem

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].f < h[j].f }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(pqItem)) }
func (h *minHeap) Pop() any          { old := *h; n := len(old); x := old[n-1]; *h = old[:n-1]; return x }

func astar(g *graph, start, goal int64) []int64 {
	goalPos := g.pos[goal]
	h := func(id int64) float64 { return dist(g.pos[id], goalPos) / (maxSpeedKmh / 3.6) }
	cost := map[int64]float64{start: 0}
	prev := map[int64]int64{}
	open := &minHeap{{h(start), start}}
	for open.Len() > 0 {
		it := heap.Pop(open).(pqItem)
		id := it.id
		if id == goal {
			out := []int64{goal}
			cur := goal
			for {
				p, ok := prev[cur]
				if !ok {
					break
				}
				out = append(out, p)
				cur = p
			}
			for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
				out[i], out[j] = out[j], out[i]
			}
			return out
		}
		base := cost[id]
		for _, e := range g.edges[id] {
			c := base + e.cost
			if old, ok := cost[e.to]; !ok || c < old {
				cost[e.to] = c
				prev[e.to] = id
				heap.Push(open, pqItem{c + h(e.to), e.to})
			}
		}
	}
	return nil
}

// RoadPath returns the fastest road path from -> to.
func (g *RoadGraph) RoadPath(ctx context.Context, from, to LatLng) (*RoadResult, error) {
	key := shortHash(fmt.Sprintf("%d|%s,%s|%s,%s", roadCacheVersion, trimFloat(from.Lat, 5), trimFloat(from.Lng, 5), trimFloat(to.Lat, 5), trimFloat(to.Lng, 5)))
	// Shared work: one caller giving up must not fail (and cache the failure for) the others.
	shared := context.WithoutCancel(ctx)
	v, err := g.memo(g.paths, key, func() (any, error) { return g.compute(shared, from, to, key) })
	if err != nil {
		return nil, err
	}
	return v.(*RoadResult), nil
}

func (g *RoadGraph) compute(ctx context.Context, from, to LatLng, key string) (*RoadResult, error) {
	if r, ok := g.loadPath(ctx, key, from, to); ok {
		return r, nil
	}

	straight := dist(from, to)
	if straight < 30 {
		return &RoadResult{Coords: [][2]float64{{from.Lat, from.Lng}, {to.Lat, to.Lng}}, Distance: straight, DurationS: straight / 5}, nil
	}
	if straight > 80000 {
		return nil, errors.New("Jarak lebih dari 80 km, kejauhan buat dihitung lokal")
	}

	// Box around both points + margin. Long distances skip minor roads to keep the data small.
	includeMinor := straight < 15000
	pad := math.Max(1500, straight*0.25)
	dLat := pad / 111320
	dLng := pad / (111320 * math.Cos(rad((from.Lat+to.Lat)/2)))
	bbox := [4]float64{
		math.Min(from.Lat, to.Lat) - dLat, math.Min(from.Lng, to.Lng) - dLng,
		math.Max(from.Lat, to.Lat) + dLat, math.Max(from.Lng, to.Lng) + dLng,
	}
	for i := range bbox {
		bbox[i], _ = parseFloat(trimFloat(bbox[i], 5))
	}

	els, err := g.areaRoads(ctx, bbox, includeMinor)
	if err != nil {
		return nil, err
	}
	gr := buildGraph(els, false)
	net := mainNetwork(gr)
	a, okA := nearest(gr, from, net)
	b, okB := nearest(gr, to, net)
	if !okA || !okB {
		return nil, errors.New("Nggak ada jalan di sekitar titik")
	}
	ids := astar(gr, a, b)
	if ids == nil {
		// Stuck on one-ways (OSM data near a site is sometimes incomplete): try once ignoring them.
		gr = buildGraph(els, true)
		ids = astar(gr, a, b)
	}
	if ids == nil {
		return nil, errors.New("Nggak ketemu jalan yang nyambung antar dua titik ini")
	}

	coords := [][2]float64{{from.Lat, from.Lng}}
	distance := dist(from, gr.pos[ids[0]])
	durationS := distance / (15 / 3.6) // from the point to the nearest road, slowly
	for i, id := range ids {
		p := gr.pos[id]
		coords = append(coords, [2]float64{p.Lat, p.Lng})
		if i > 0 {
			for _, e := range gr.edges[ids[i-1]] {
				if e.to == id {
					distance += e.len
					durationS += e.cost
					break
				}
			}
		}
	}
	tail := dist(gr.pos[ids[len(ids)-1]], to)
	coords = append(coords, [2]float64{to.Lat, to.Lng})
	distance += tail
	durationS += tail / (15 / 3.6)

	result := &RoadResult{Coords: coords, Distance: distance, DurationS: durationS}
	g.savePath(ctx, key, from, to, result)
	return result, nil
}
