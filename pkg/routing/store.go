package routing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Store keeps computed road paths and raw OSM road data per area (fms-backend: the
// road_paths / road_areas tables, viewable and deletable in fms-administrator).
type Store interface {
	GetPath(ctx context.Context, key string) (*RoadResult, bool)
	PutPath(ctx context.Context, key string, from, to LatLng, r *RoadResult)
	// GetArea returns the raw elements JSON when stored and younger than maxAge.
	GetArea(ctx context.Context, key string, maxAge time.Duration) ([]byte, bool)
	PutArea(ctx context.Context, key string, bbox [4]float64, kind string, raw []byte, elements int)
}

// cacheGen: bumped when stored road data is deleted, so every RoadGraph drops what it
// keeps in memory and reads the store again.
var cacheGen atomic.Int64

// Invalidate forgets the in-memory road data of every RoadGraph in this process.
func Invalidate() { cacheGen.Add(1) }

// WithStore keeps road data in s instead of files. Old files under dataDir/roads are
// moved into the store the first time they are needed.
func (g *RoadGraph) WithStore(s Store) *RoadGraph {
	g.store = s
	return g
}

func (g *RoadGraph) loadPath(ctx context.Context, key string, from, to LatLng) (*RoadResult, bool) {
	file := filepath.Join(g.cacheDir, key+".json")
	if g.store != nil {
		if r, ok := g.store.GetPath(ctx, key); ok {
			return r, true
		}
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	var r RoadResult
	if json.Unmarshal(b, &r) != nil {
		return nil, false
	}
	if g.store != nil {
		g.store.PutPath(ctx, key, from, to, &r)
		_ = os.Remove(file)
	}
	return &r, true
}

func (g *RoadGraph) savePath(ctx context.Context, key string, from, to LatLng, r *RoadResult) {
	if g.store != nil {
		g.store.PutPath(ctx, key, from, to, r)
		return
	}
	if b, err := json.Marshal(r); err == nil {
		_ = os.MkdirAll(g.cacheDir, 0o755)
		_ = os.WriteFile(filepath.Join(g.cacheDir, key+".json"), b, 0o644)
	}
}

func (g *RoadGraph) loadArea(ctx context.Context, key string, bbox [4]float64, kind string) ([]osmElement, bool) {
	if g.store != nil {
		if raw, ok := g.store.GetArea(ctx, key, areaTTL); ok {
			var els []osmElement
			if json.Unmarshal(raw, &els) == nil {
				return els, true
			}
		}
	}
	file := filepath.Join(g.cacheDir, "area-"+shortHash(key)+".json")
	st, err := os.Stat(file)
	if err != nil || time.Since(st.ModTime()) >= areaTTL {
		return nil, false
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	var els []osmElement
	if json.Unmarshal(raw, &els) != nil {
		return nil, false
	}
	if g.store != nil {
		g.store.PutArea(ctx, key, bbox, kind, raw, len(els))
		_ = os.Remove(file)
	}
	return els, true
}

func (g *RoadGraph) saveArea(ctx context.Context, key string, bbox [4]float64, kind string, els []osmElement) {
	raw, err := json.Marshal(els)
	if err != nil {
		return
	}
	if g.store != nil {
		g.store.PutArea(ctx, key, bbox, kind, raw, len(els))
		return
	}
	_ = os.MkdirAll(g.cacheDir, 0o755)
	_ = os.WriteFile(filepath.Join(g.cacheDir, "area-"+shortHash(key)+".json"), raw, 0o644)
}
