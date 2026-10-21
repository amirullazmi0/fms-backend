package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
)

// Auto dispatch (P3-9): bila fitur "auto_dispatch" menyala, saran pindah DT dari panel dispatcher
// dijalankan otomatis, tetapi hanya bila saran itu stabil (muncul terus minimal StableMin menit)
// dan fleet asal maupun tujuan tidak dipindah dalam CooldownMin menit terakhir.
const (
	FeatureAutoDispatch = "auto_dispatch"
	autoDispatchEvery   = time.Minute
)

type autoDispatchConfig struct {
	StableMin   int `json:"stableMin"`
	CooldownMin int `json:"cooldownMin"`
}

type AutoDispatcher struct {
	dispatch DispatchUsecase
	shifts   ShiftUsecase
	features repository.FeatureRepository
	tracker  *tracker.Tracker

	seen  map[string]int64 // imei|to → first time the move was suggested
	moved map[string]int64 // route → last automatic move touching it
}

func NewAutoDispatcher(d DispatchUsecase, s ShiftUsecase, f repository.FeatureRepository, t *tracker.Tracker) *AutoDispatcher {
	return &AutoDispatcher{dispatch: d, shifts: s, features: f, tracker: t, seen: map[string]int64{}, moved: map[string]int64{}}
}

// Run checks every minute until ctx is done.
func (a *AutoDispatcher) Run(ctx context.Context) {
	t := time.NewTicker(autoDispatchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.tick(ctx)
		}
	}
}

func (a *AutoDispatcher) config(ctx context.Context) (autoDispatchConfig, bool) {
	cfg := autoDispatchConfig{StableMin: 10, CooldownMin: 30}
	f, err := a.features.Get(ctx, FeatureAutoDispatch)
	if err != nil || f == nil || !f.Enabled {
		return cfg, false
	}
	if len(f.Config) > 0 {
		_ = json.Unmarshal(f.Config, &cfg)
	}
	cfg.StableMin, cfg.CooldownMin = max(1, cfg.StableMin), max(0, cfg.CooldownMin)
	return cfg, true
}

func (a *AutoDispatcher) tick(ctx context.Context) {
	cfg, on := a.config(ctx)
	if !on {
		clear(a.seen)
		return
	}
	board, err := a.dispatch.Board(ctx)
	if err != nil {
		logger.Warn("auto dispatch: " + err.Error())
		return
	}
	now := time.Now().UnixMilli()
	for _, m := range a.ready(board.Moves, now, cfg) {
		if _, err := a.shifts.MoveUnit(ctx, m.Imei, m.ToRouteID); err != nil {
			a.tracker.Log("conn", m.Imei, "Auto dispatch gagal: "+err.Error(), "warn")
			continue
		}
		a.moved[m.FromRouteID], a.moved[m.ToRouteID] = now, now
		delete(a.seen, m.Imei+"|"+m.ToRouteID)
		text := fmt.Sprintf("%s dipindah otomatis dari fleet %s ke %s untuk menyeimbangkan match factor.", m.Label, m.FromRouteName, m.ToRouteName)
		a.tracker.Notice("info", "Auto dispatch", text, "/dispatch")
		a.tracker.Changed("dispatch")
	}
}

// cooling: the route had an automatic move less than cool ago.
func (a *AutoDispatcher) cooling(route string, now, cool int64) bool {
	at, ok := a.moved[route]
	return ok && now-at < cool
}

// ready: the suggestions stable long enough whose fleets are out of cooldown; forgets the rest.
func (a *AutoDispatcher) ready(moves []dto.FleetMove, now int64, cfg autoDispatchConfig) []dto.FleetMove {
	current := map[string]bool{}
	out := []dto.FleetMove{}
	stable := int64(cfg.StableMin) * 60_000
	cool := int64(cfg.CooldownMin) * 60_000
	for _, m := range moves {
		key := m.Imei + "|" + m.ToRouteID
		current[key] = true
		if _, ok := a.seen[key]; !ok {
			a.seen[key] = now
		}
		if now-a.seen[key] < stable {
			continue
		}
		if a.cooling(m.FromRouteID, now, cool) || a.cooling(m.ToRouteID, now, cool) {
			continue
		}
		out = append(out, m)
	}
	for k := range a.seen {
		if !current[k] {
			delete(a.seen, k) // suggestion gone: start over if it comes back
		}
	}
	return out
}
