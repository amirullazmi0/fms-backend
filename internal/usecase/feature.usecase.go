package usecase

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/model"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/routing"
	"github.com/amirullazmi0/fms-backend/pkg/simulator"
)

// FeatureUsecase: feature switches (fms-administrator → Fitur) and the backend's own
// truck simulator (feature "simulation").
type FeatureUsecase interface {
	List(ctx context.Context) ([]dto.FeatureResponse, error)
	Update(ctx context.Context, actor *model.Actor, key string, req *dto.UpdateFeatureRequest) (*dto.FeatureResponse, error)

	SimulationStatus(ctx context.Context) (*dto.SimulationStatus, error)
	StartSimulation(ctx context.Context, actor *model.Actor, req *dto.StartSimulationRequest) (*dto.SimulationStatus, error)
	StopSimulation(ctx context.Context, actor *model.Actor) (*dto.SimulationStatus, error)
	// ResumeSimulation restarts the simulator after a server restart when it was running.
	ResumeSimulation(ctx context.Context)
}

type featureUsecase struct {
	repo    repository.FeatureRepository
	db      *sql.DB
	cfg     *config.Config
	tracker *tracker.Tracker
	roads   routing.Store   // road data shared with the API (database)
	root    context.Context // server lifetime: the simulator stops with the server

	mu        sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	run       dto.SimulationConfig
	startedAt int64
	startedBy string
}

func NewFeatureUsecase(root context.Context, repo repository.FeatureRepository, db *sql.DB, cfg *config.Config, t *tracker.Tracker, roads routing.Store) FeatureUsecase {
	return &featureUsecase{repo: repo, db: db, cfg: cfg, tracker: t, roads: roads, root: root}
}

func (u *featureUsecase) List(ctx context.Context) ([]dto.FeatureResponse, error) {
	return u.repo.List(ctx)
}

func (u *featureUsecase) Update(ctx context.Context, actor *model.Actor, key string, req *dto.UpdateFeatureRequest) (*dto.FeatureResponse, error) {
	var config []byte
	if len(req.Config) > 0 && !bytes.Equal(bytes.TrimSpace(req.Config), []byte("null")) {
		var obj map[string]any
		if err := json.Unmarshal(req.Config, &obj); err != nil {
			return nil, apperror.BadRequest("Config harus objek JSON")
		}
		if key == model.FeatureSimulation {
			if err := validSimulation(req.Config); err != nil {
				return nil, err
			}
			_, cur, err := u.simulationFeature(ctx)
			if err != nil {
				return nil, err
			}
			obj["running"] = cur.Running
			if config, err = json.Marshal(obj); err != nil {
				return nil, err
			}
		} else {
			config = req.Config
		}
	}
	if req.Enabled == nil && config == nil {
		return nil, apperror.BadRequest("Tidak ada yang diubah")
	}
	ok, err := u.repo.Update(ctx, key, req.Enabled, config, actor.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperror.NotFound("Fitur tidak ditemukan")
	}
	// Switching the simulation off stops a running simulator.
	if key == model.FeatureSimulation && req.Enabled != nil && !*req.Enabled {
		u.setRunning(ctx, false, actor.ID)
		u.stop(actor.ID)
	}
	u.tracker.Changed("features")
	return u.repo.Get(ctx, key)
}

func validSimulation(raw json.RawMessage) error {
	var c dto.SimulationConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return apperror.BadRequest("Config simulasi tidak valid")
	}
	if c.Devices < 1 || c.Devices > 50 {
		return apperror.BadRequest("Jumlah unit simulasi 1–50")
	}
	if c.Interval < 1 || c.Interval > 60 {
		return apperror.BadRequest("Interval kirim data 1–60 detik")
	}
	others := 0
	for _, n := range []*int{c.Excavators, c.LightVehicles, c.Supports} {
		if n != nil {
			if *n < 0 {
				return apperror.BadRequest("Jumlah EX / LV / SUPPORT tidak boleh negatif")
			}
			others += *n
		}
	}
	if others > c.Devices {
		return apperror.BadRequest("Jumlah EX + LV + SUPPORT melebihi jumlah unit")
	}
	return nil
}

// simulationMix: the EX / LV / SUPPORT counts of a run (default composition when not set).
func simulationMix(c dto.SimulationConfig) (ex, lv, sup int) {
	ex, lv, sup = simulator.Mix(c.Devices)
	if c.Excavators != nil {
		ex = *c.Excavators
	}
	if c.LightVehicles != nil {
		lv = *c.LightVehicles
	}
	if c.Supports != nil {
		sup = *c.Supports
	}
	return ex, lv, sup
}

// registerUnits sets the unit type of the fake devices (and a name while they have none), so
// EX / LV / SUPPORT show as such on the dashboard.
func (u *featureUsecase) registerUnits(ctx context.Context, units []simulator.Unit, by string) {
	cur := map[string]dto.Device{}
	for _, d := range u.tracker.Devices() {
		cur[d.Imei] = d
	}
	for _, x := range units {
		d, ok := cur[x.Imei]
		kind := string(x.Kind)
		req := dto.UpdateDeviceRequest{}
		if !ok || d.UnitType != kind {
			req.UnitType = &kind
		}
		if !ok || d.Name == nil {
			name := x.Name
			req.Name = &name
		}
		if req.UnitType == nil && req.Name == nil {
			continue
		}
		if _, err := u.tracker.UpdateMeta(ctx, x.Imei, req, userOf(by)); err != nil {
			logger.Warn("simulator: gagal set jenis unit " + x.Imei + ": " + err.Error())
		}
	}
}

// simulationFeature: the feature row and its config (with defaults).
func (u *featureUsecase) simulationFeature(ctx context.Context) (*dto.FeatureResponse, dto.SimulationConfig, error) {
	c := dto.SimulationConfig{Devices: 2, Interval: 3}
	f, err := u.repo.Get(ctx, model.FeatureSimulation)
	if err != nil || f == nil {
		return f, c, err
	}
	_ = json.Unmarshal(f.Config, &c)
	if c.Devices < 1 {
		c.Devices = 2
	}
	if c.Interval < 1 {
		c.Interval = 3
	}
	return f, c, nil
}

func (u *featureUsecase) SimulationStatus(ctx context.Context) (*dto.SimulationStatus, error) {
	f, c, err := u.simulationFeature(ctx)
	if err != nil {
		return nil, err
	}
	return u.status(f != nil && f.Enabled, c), nil
}

func (u *featureUsecase) status(enabled bool, c dto.SimulationConfig) *dto.SimulationStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := &dto.SimulationStatus{Enabled: enabled, Devices: c.Devices, Interval: c.Interval}
	if u.cancel != nil {
		at, by := u.startedAt, u.startedBy
		s.Running, s.Devices, s.Interval, s.StartedAt, s.StartedBy = true, u.run.Devices, u.run.Interval, &at, &by
	}
	return s
}

func (u *featureUsecase) StartSimulation(ctx context.Context, actor *model.Actor, req *dto.StartSimulationRequest) (*dto.SimulationStatus, error) {
	f, c, err := u.simulationFeature(ctx)
	if err != nil {
		return nil, err
	}
	if f == nil || !f.Enabled {
		return nil, apperror.Forbidden("Fitur simulasi sedang nonaktif. Aktifkan terlebih dahulu di menu Fitur.")
	}
	if req.Devices != nil {
		c.Devices = *req.Devices
	}
	if req.Interval != nil {
		c.Interval = *req.Interval
	}

	u.mu.Lock()
	if u.cancel != nil || simulator.Running() {
		u.mu.Unlock()
		return nil, apperror.Conflict("Simulasi sudah berjalan")
	}
	runCtx, cancel := context.WithCancel(u.root)
	done := make(chan struct{})
	u.cancel, u.done, u.run = cancel, done, c
	u.startedAt, u.startedBy = time.Now().UnixMilli(), actor.ID
	u.mu.Unlock()

	ex, lv, sup := simulationMix(c)
	opts := simulator.Options{
		Devices:       c.Devices,
		Excavators:    ex,
		LightVehicles: lv,
		Supports:      sup,
		Interval:      c.Interval,
		Loop:        c.Loop,
		BlankSpot:   c.BlankSpot,
		Queue:       c.Queue,
		Host:        "127.0.0.1",
		Port:        u.cfg.Device.Port,
		DataDir:     u.cfg.App.DataDir,
		OverpassURL: u.cfg.Routing.OverpassURL,
		DB:          u.db,
		RoadStore:   u.roads,
		Logf: func(format string, args ...any) {
			logger.Info("simulator: " + fmt.Sprintf(format, args...))
		},
	}
	units := simulator.Units(opts)
	u.registerUnits(ctx, units, actor.ID)
	go func() {
		defer close(done)
		if err := simulator.Run(runCtx, opts); err != nil {
			logger.Warn("simulator: " + err.Error())
		}
		u.mu.Lock()
		if u.done == done {
			u.cancel, u.done = nil, nil
		}
		u.mu.Unlock()
		u.tracker.Changed("simulation")
	}()

	u.setRunning(ctx, true, actor.ID)
	kinds := map[simulator.UnitKind]int{}
	for _, x := range units {
		kinds[x.Kind]++
	}
	mix := fmt.Sprintf("%d DT, %d EX, %d LV, %d SUPPORT", kinds[simulator.DT], kinds[simulator.EX], kinds[simulator.LV], kinds[simulator.SUPPORT])
	logger.Info(fmt.Sprintf("Simulasi berjalan: %d unit (%s), setiap %d detik", len(units), mix, c.Interval))
	u.tracker.Notice("info", "Simulasi berjalan", fmt.Sprintf("%d unit simulasi (%s) terhubung ke server, mengirim data setiap %d detik.", len(units), mix, c.Interval), "")
	u.tracker.Changed("simulation")
	return u.status(true, c), nil
}

func (u *featureUsecase) StopSimulation(ctx context.Context, actor *model.Actor) (*dto.SimulationStatus, error) {
	u.setRunning(ctx, false, actor.ID)
	if !u.stop(actor.ID) {
		return nil, apperror.Conflict("Simulasi sedang tidak berjalan")
	}
	return u.SimulationStatus(ctx)
}

// stop cancels the run and waits (briefly) for the trucks to disconnect.
func (u *featureUsecase) stop(by string) bool {
	u.mu.Lock()
	cancel, done := u.cancel, u.done
	u.cancel, u.done = nil, nil
	u.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	logger.Info("Simulasi dihentikan oleh " + by)
	u.tracker.Notice("warn", "Simulasi dihentikan", "Unit simulasi putus dari server.", "")
	u.tracker.Changed("simulation")
	return true
}

// setRunning stores whether the simulator should be on (survives a server restart).
func (u *featureUsecase) setRunning(ctx context.Context, running bool, by string) {
	f, err := u.repo.Get(ctx, model.FeatureSimulation)
	if err != nil || f == nil {
		return
	}
	obj := map[string]any{}
	_ = json.Unmarshal(f.Config, &obj)
	if v, _ := obj["running"].(bool); v == running {
		return
	}
	obj["running"] = running
	raw, err := json.Marshal(obj)
	if err != nil {
		return
	}
	if _, err := u.repo.Update(ctx, model.FeatureSimulation, nil, raw, userOf(by)); err != nil {
		logger.Warn("simulator: gagal menyimpan status berjalan: " + err.Error())
	}
}

func (u *featureUsecase) ResumeSimulation(ctx context.Context) {
	f, c, err := u.simulationFeature(ctx)
	if err != nil || f == nil || !f.Enabled || !c.Running {
		return
	}
	actor := &model.Actor{ID: systemActor}
	if _, err := u.StartSimulation(ctx, actor, &dto.StartSimulationRequest{}); err != nil {
		logger.Warn("simulator: gagal lanjutin simulasi: " + err.Error())
		return
	}
	logger.Info("Simulasi dilanjutkan otomatis (sebelumnya masih berjalan saat server berhenti)")
}

// systemActor: the simulator resumed by the server itself (no user). It is not a user UUID, so
// updated_by stays unset for its writes (userOf).
const systemActor = "system"

func userOf(by string) string {
	if by == systemActor {
		return ""
	}
	return by
}
