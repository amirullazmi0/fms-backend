// Package geocode turns coordinates into a road/area name (reverse geocoding) via
// Nominatim (OpenStreetMap). Each location is asked once: results live in memory and
// in the places table. Nominatim allows 1 request per second, hence the queue.
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Store persists the place cache.
type Store interface {
	ListPlaces(ctx context.Context) (map[string]*string, error)
	SavePlace(ctx context.Context, key string, label *string) error
}

type job struct {
	key      string
	lat, lng float64
}

type Geocoder struct {
	enabled  bool
	store    Store
	client   *http.Client
	mu       sync.Mutex
	cache    map[string]*string // nil value = looked up, no name
	queued   map[string]bool
	queue    chan job
	failures int
	logf     func(format string, args ...any)
}

func New(enabled bool, store Store, logf func(format string, args ...any)) *Geocoder {
	return &Geocoder{
		enabled: enabled,
		store:   store,
		client:  &http.Client{Timeout: 8 * time.Second},
		cache:   map[string]*string{},
		queued:  map[string]bool{},
		queue:   make(chan job, 1000),
		logf:    logf,
	}
}

// Load fills the memory cache from the database.
func (g *Geocoder) Load(ctx context.Context) error {
	places, err := g.store.ListPlaces(ctx)
	if err != nil {
		return err
	}
	g.mu.Lock()
	for k, v := range places {
		g.cache[k] = v
	}
	g.mu.Unlock()
	return nil
}

// Run works the queue until ctx is done.
func (g *Geocoder) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-g.queue:
			g.lookup(ctx, j)
			select {
			case <-ctx.Done():
				return
			case <-time.After(1100 * time.Millisecond):
			}
		}
	}
}

// keyOf rounds to ~11 m so nearby points share one result.
func keyOf(lat, lng float64) string {
	return fmt.Sprintf("%.4f,%.4f", lat, lng)
}

// PlaceName returns the cached name, or queues a lookup and returns nil.
func (g *Geocoder) PlaceName(lat, lng float64) *string {
	key := keyOf(lat, lng)
	g.mu.Lock()
	defer g.mu.Unlock()
	if v, ok := g.cache[key]; ok {
		return v
	}
	if g.enabled && !g.queued[key] && g.failures < 5 {
		select {
		case g.queue <- job{key, lat, lng}:
			g.queued[key] = true
		default:
		}
	}
	return nil
}

type nominatimReply struct {
	DisplayName *string           `json:"display_name"`
	Address     map[string]string `json:"address"`
}

func (g *Geocoder) lookup(ctx context.Context, j job) {
	label, err := g.fetch(ctx, j.lat, j.lng)
	g.mu.Lock()
	delete(g.queued, j.key)
	if err != nil {
		g.failures++
		if g.failures == 5 && g.logf != nil {
			g.logf("Geocode dimatiin sementara, Nominatim nggak bisa diakses (%v)", err)
		}
		g.mu.Unlock()
		return
	}
	g.failures = 0
	g.cache[j.key] = label
	g.mu.Unlock()
	if err := g.store.SavePlace(ctx, j.key, label); err != nil && g.logf != nil {
		g.logf("Gagal nyimpen nama tempat: %v", err)
	}
}

func (g *Geocoder) fetch(ctx context.Context, lat, lng float64) (*string, error) {
	url := fmt.Sprintf("https://nominatim.openstreetmap.org/reverse?format=jsonv2&lat=%v&lon=%v&zoom=17&accept-language=id", lat, lng)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fms-backend/1.0 (FMC150 fleet dashboard)")
	res, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var r nominatimReply
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, err
	}
	return label(r), nil
}

func first(a map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := a[k]; v != "" {
			return v
		}
	}
	return ""
}

func label(r nominatimReply) *string {
	a := r.Address
	if a == nil {
		return r.DisplayName
	}
	parts := []string{}
	for _, p := range []string{
		first(a, "road", "pedestrian", "residential", "amenity", "building"),
		first(a, "suburb", "village", "neighbourhood"),
		first(a, "city_district", "municipality", "town", "city", "county"),
	} {
		dup := false
		for _, q := range parts {
			if q == p {
				dup = true
			}
		}
		if p != "" && !dup {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return r.DisplayName
	}
	s := strings.Join(parts, ", ")
	return &s
}
