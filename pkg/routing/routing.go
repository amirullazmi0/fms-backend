// Package routing finds the road path between a loading and an unloading site, with
// distance and time estimates. It uses OSRM (OpenStreetMap routing; the public demo
// server by default, OSRM_URL to change, ROUTING=0 to disable). When OSRM is
// unreachable, the path is computed locally from OSM road data (one path, no
// alternatives).
//
// OSRM uses a car profile. Trucks are slower: truck estimate = car time × TRUCK_FACTOR.
package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

const (
	cacheTTL = 24 * time.Hour
	failTTL  = time.Minute // failed: don't retry on every request
)

type cached struct {
	at    time.Time
	value *dto.RoadRoute
	err   string
}

type Router struct {
	enabled     bool
	osrmURL     string
	truckFactor float64
	graph       *RoadGraph
	client      *http.Client

	mu       sync.Mutex
	cache    map[string]cached
	inflight map[string]chan struct{}
}

func New(enabled bool, osrmURL string, truckFactor float64, graph *RoadGraph) *Router {
	return &Router{
		enabled:     enabled,
		osrmURL:     osrmURL,
		truckFactor: truckFactor,
		graph:       graph,
		client:      &http.Client{Timeout: 12 * time.Second},
		cache:       map[string]cached{},
		inflight:    map[string]chan struct{}{},
	}
}

func (r *Router) TruckFactor() float64 { return r.truckFactor }

// RoadRoute returns the road path(s) from -> to.
func (r *Router) RoadRoute(ctx context.Context, from, to LatLng) (*dto.RoadRoute, error) {
	var osrmErr error
	if !r.enabled {
		osrmErr = errors.New("OSRM dimatiin (ROUTING=0)")
	} else {
		v, err := r.osrmRoute(ctx, from, to)
		if err == nil {
			return v, nil
		}
		osrmErr = err
	}
	p, err := r.graph.RoadPath(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("%v; hitung lokal juga gagal: %v", osrmErr, err)
	}
	return &dto.RoadRoute{
		Paths:       []dto.RoadPath{{Distance: p.Distance, DurationS: p.DurationS, TruckDurationS: p.DurationS * r.truckFactor, Coords: p.Coords}},
		TruckFactor: r.truckFactor,
		Source:      "data jalan OpenStreetMap (dihitung lokal)",
		FetchedAt:   time.Now().UnixMilli(),
	}, nil
}

type osrmReply struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Routes  []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry struct {
			Coordinates [][2]float64 `json:"coordinates"` // [lng, lat]
		} `json:"geometry"`
	} `json:"routes"`
}

func (r *Router) osrmRoute(ctx context.Context, from, to LatLng) (*dto.RoadRoute, error) {
	key := fmt.Sprintf("%.6f,%.6f;%.6f,%.6f", from.Lat, from.Lng, to.Lat, to.Lng)
	for {
		r.mu.Lock()
		if hit, ok := r.cache[key]; ok {
			ttl := failTTL
			if hit.value != nil {
				ttl = cacheTTL
			}
			if time.Since(hit.at) < ttl {
				r.mu.Unlock()
				if hit.value != nil {
					return hit.value, nil
				}
				return nil, errors.New(hit.err)
			}
		}
		if wait, ok := r.inflight[key]; ok {
			r.mu.Unlock()
			<-wait
			continue
		}
		done := make(chan struct{})
		r.inflight[key] = done
		r.mu.Unlock()

		value, err := r.fetchOSRM(context.WithoutCancel(ctx), from, to)
		r.mu.Lock()
		entry := cached{at: time.Now(), value: value}
		if err != nil {
			entry.err = err.Error()
		}
		r.cache[key] = entry
		delete(r.inflight, key)
		close(done)
		r.mu.Unlock()
		return value, err
	}
}

func (r *Router) fetchOSRM(ctx context.Context, from, to LatLng) (*dto.RoadRoute, error) {
	url := fmt.Sprintf("%s/route/v1/driving/%s,%s;%s,%s?overview=full&geometries=geojson&alternatives=3&steps=false",
		r.osrmURL, ff(from.Lng), ff(from.Lat), ff(to.Lng), ff(to.Lat))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fms-backend")
	res, err := r.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return nil, errors.New("Server routing nggak jawab (timeout)")
		}
		return nil, err
	}
	defer res.Body.Close()
	var body osrmReply
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != http.StatusOK || body.Code != "Ok" || len(body.Routes) == 0 {
		if body.Code == "NoRoute" {
			return nil, errors.New("Nggak ketemu jalan yang nyambung antar dua titik ini")
		}
		if body.Message != "" {
			return nil, errors.New(body.Message)
		}
		return nil, fmt.Errorf("Server routing balas %d", res.StatusCode)
	}
	paths := make([]dto.RoadPath, len(body.Routes))
	for i, rt := range body.Routes {
		coords := make([][2]float64, len(rt.Geometry.Coordinates))
		for k, c := range rt.Geometry.Coordinates {
			coords[k] = [2]float64{c[1], c[0]}
		}
		paths[i] = dto.RoadPath{Distance: rt.Distance, DurationS: rt.Duration, TruckDurationS: rt.Duration * r.truckFactor, Coords: coords}
	}
	return &dto.RoadRoute{Paths: paths, TruckFactor: r.truckFactor, Source: "OSRM (OpenStreetMap)", FetchedAt: time.Now().UnixMilli()}, nil
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// ff formats a coordinate like JS Number.toString().
func ff(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// trimFloat is Number(x.toFixed(d)) as text.
func trimFloat(v float64, d int) string {
	s := strconv.FormatFloat(v, 'f', d, 64)
	f, _ := strconv.ParseFloat(s, 64)
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
