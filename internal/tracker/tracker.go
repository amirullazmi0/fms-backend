// Package tracker is the live side of the backend: FMC150 devices connect over TCP
// (Teltonika Codec 8/8E, commands over Codec 12), every record is stored then ACKed,
// and the in-memory fleet state is pushed to dashboards over SSE.
// Port of the device/state half of fmctest/server/index.ts.
package tracker

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/sse"
	"go.uber.org/zap"
)

const (
	HistoryMax     = 3000
	eventsMax      = 300
	logMax         = 3000
	commandTimeout = 20 * time.Second
)

var (
	ErrOffline       = errors.New("Alat lagi offline")
	ErrNoReply       = errors.New("Alat nggak bales dalam 20 detik")
	ErrDeviceUnknown = errors.New("Alat belum pernah konek")
)

// Demo truck info for simulator devices (IMEI 35000000000000x).
var demoTrucks = []dto.DeviceMeta{
	{Name: strp("Truk Simulasi 0"), Plate: strp("B 9010 SIM"), Vehicle: strp("Hino Ranger FM 260")},
	{Name: strp("Truk Simulasi 1"), Plate: strp("B 9121 SIM"), Vehicle: strp("Mitsubishi Fuso Fighter")},
	{Name: strp("Truk Simulasi 2"), Plate: strp("B 9232 SIM"), Vehicle: strp("Isuzu Giga FVR")},
	{Name: strp("Truk Simulasi 3"), Plate: strp("D 9343 SIM"), Vehicle: strp("Hino Dutro 130 HD")},
}

func strp(s string) *string { return &s }
func i64p(v int64) *int64   { return &v }

type deviceState struct {
	dto.Device
	history []dto.HistoryPoint
}

type commandResult struct {
	text string
	err  error
}

type commandJob struct {
	cmd   string
	reply chan commandResult
}

type Tracker struct {
	cfg     *config.Config
	devices repository.DeviceRepository
	events  repository.EventRepository
	hub     *sse.Hub

	startedAt int64

	mu      sync.Mutex
	state   map[string]*deviceState
	order   []string // first-seen order, like the demo's Map
	recent  []dto.FleetEvent
	conns   map[string]*deviceConn
	pending map[string][]*commandJob
	logs    []dto.LogEntry
	logSeq  int64
	counts  dto.DBCounts
	tunnel  *dto.TunnelInfo
}

func New(cfg *config.Config, devices repository.DeviceRepository, events repository.EventRepository, hub *sse.Hub) *Tracker {
	return &Tracker{
		cfg:       cfg,
		devices:   devices,
		events:    events,
		hub:       hub,
		startedAt: time.Now().UnixMilli(),
		state:     map[string]*deviceState{},
		conns:     map[string]*deviceConn{},
		pending:   map[string][]*commandJob{},
	}
}

// Hub is the SSE hub dashboards subscribe to.
func (t *Tracker) Hub() *sse.Hub { return t.hub }

// Broadcast sends a stream message to every dashboard.
func (t *Tracker) Broadcast(msg map[string]any) { t.hub.Broadcast(msg) }

// Changed tells dashboards a collection changed: "sites", "shifts" or "trips".
func (t *Tracker) Changed(what string) { t.hub.Broadcast(map[string]any{"type": what}) }

// Notice pushes a general notification (e.g. a shift past its end time).
func (t *Tracker) Notice(level, title, body, to string) {
	msg := map[string]any{"type": "notice", "level": level, "title": title, "body": body}
	if to != "" {
		msg["to"] = to
	}
	t.hub.Broadcast(msg)
}

// Load restores devices, their recent history and events from the database.
func (t *Tracker) Load(ctx context.Context) error {
	counts, err := t.devices.Counts(ctx)
	if err != nil {
		return err
	}
	rows, err := t.devices.ListDevices(ctx)
	if err != nil {
		return err
	}
	recs := make(map[string][]dto.AvlRecord, len(rows))
	for _, row := range rows {
		r, err := t.devices.LastRecords(ctx, row.Imei, HistoryMax)
		if err != nil {
			return err
		}
		recs[row.Imei] = r
	}
	evs, err := t.events.LastEvents(ctx, eventsMax)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.counts = counts
	for _, row := range rows {
		d := t.getDevice(row.Imei)
		d.Name, d.Plate, d.Vehicle = row.Name, row.Plate, row.Vehicle
		d.FirstSeen = i64p(row.FirstSeen)
		d.LastSeen = row.LastSeen
		d.ConnectedAt = row.LastConnectedAt
		d.Remote = row.LastRemote
		for _, rec := range recs[row.Imei] {
			t.ingest(row.Imei, rec, false)
		}
		d.Records = row.Records
	}
	t.recent = append(t.recent, evs...)
	logger.Info("Tracker state loaded", zap.Int("devices", counts.Devices), zap.Int("records", counts.Records), zap.Int("events", counts.Events))
	return nil
}

// getDevice returns the state of imei, creating it. Caller holds t.mu.
func (t *Tracker) getDevice(imei string) *deviceState {
	d, ok := t.state[imei]
	if !ok {
		d = &deviceState{Device: dto.Device{Imei: imei, Io: dto.IoMap{}, IoTime: map[string]int64{}}}
		t.state[imei] = d
		t.order = append(t.order, imei)
	}
	return d
}

// summary copies a device for output (maps copied so JSON encoding runs without the lock).
func summary(d *deviceState) dto.Device {
	out := d.Device
	out.Io = make(dto.IoMap, len(d.Io))
	for k, v := range d.Io {
		out.Io[k] = v
	}
	out.IoTime = make(map[string]int64, len(d.IoTime))
	for k, v := range d.IoTime {
		out.IoTime[k] = v
	}
	if d.Last != nil {
		last := *d.Last
		out.Last = &last
	}
	if d.Pos != nil {
		pos := *d.Pos
		out.Pos = &pos
	}
	return out
}

func (t *Tracker) broadcastDevice(d *deviceState) {
	t.hub.Broadcast(map[string]any{"type": "device", "device": summary(d)})
}

// ingest applies a record to the device state. live = new from the device;
// live false = reload from the database at startup (events already stored).
// Caller holds t.mu.
func (t *Tracker) ingest(imei string, rec dto.AvlRecord, live bool) {
	d := t.getDevice(imei)
	prevIo := make(dto.IoMap, len(d.Io))
	for k, v := range d.Io {
		prevIo[k] = v
	}
	if live {
		d.Records++
	}
	if d.Last == nil || rec.Time >= d.Last.Time {
		head := rec.AvlHead
		d.Last = &head
		for id, v := range rec.Io {
			d.Io[id] = v
			d.IoTime[id] = rec.Time
		}
		if rec.Valid {
			d.Pos = &dto.DevicePos{Lat: rec.Lat, Lng: rec.Lng, Angle: rec.Angle, Time: rec.Time}
		}
	}

	point := dto.HistoryPoint{T: rec.Time, Lat: rec.Lat, Lng: rec.Lng, V: rec.Valid, Speed: rec.Speed, Angle: rec.Angle, Sats: rec.Sats, Ign: rec.Io["239"], Ext: rec.Io["66"]}
	h := append(d.history, point)
	if n := len(h); n > 1 && h[n-2].T > point.T {
		sort.SliceStable(h, func(i, j int) bool { return h[i].T < h[j].T })
	}
	if len(h) > HistoryMax {
		h = append([]dto.HistoryPoint(nil), h[len(h)-HistoryMax:]...)
	}
	d.history = h

	if live {
		t.detectEvents(d, prevIo, rec)
		t.hub.Broadcast(map[string]any{"type": "point", "imei": imei, "point": point})
		t.broadcastDevice(d)
	}
}

// pushEvent records an event. Caller holds t.mu.
func (t *Tracker) pushEvent(ev dto.FleetEvent) {
	go func() {
		if err := t.events.SaveEvent(context.Background(), ev); err != nil {
			t.Log("error", "", "Database: "+err.Error(), "error")
		}
	}()
	t.counts.Events++
	t.recent = append(t.recent, ev)
	if len(t.recent) > eventsMax {
		t.recent = append([]dto.FleetEvent(nil), t.recent[len(t.recent)-eventsMax:]...)
	}
	t.hub.Broadcast(map[string]any{"type": "event", "event": ev})
}

func (t *Tracker) pushCommand(line dto.CommandLine) {
	go func() {
		if err := t.events.SaveCommand(context.Background(), line); err != nil {
			t.Log("error", "", "Database: "+err.Error(), "error")
		}
	}()
	t.hub.Broadcast(map[string]any{"type": "command", "imei": line.Imei, "dir": line.Dir, "text": line.Text, "time": line.Time})
}

// quietKinds are not echoed to the server log (too chatty).
var quietKinds = map[string]bool{"raw": true, "data": true, "ack": true}

// Log adds a device log line (the dashboard log view) and streams it.
func (t *Tracker) Log(kind, imei, text, level string) {
	t.mu.Lock()
	t.logSeq++
	entry := dto.LogEntry{ID: t.logSeq, Time: time.Now().UnixMilli(), Kind: kind, Level: level, Text: text}
	if imei != "" {
		entry.Imei = strp(imei)
	}
	t.logs = append(t.logs, entry)
	if len(t.logs) > logMax {
		t.logs = append([]dto.LogEntry(nil), t.logs[len(t.logs)-logMax:]...)
	}
	t.mu.Unlock()
	t.hub.Broadcast(map[string]any{"type": "log", "entry": entry})

	if quietKinds[kind] && level == "info" {
		return
	}
	fields := []zap.Field{zap.String("kind", kind)}
	if imei != "" {
		fields = append(fields, zap.String("imei", imei))
	}
	switch level {
	case "error":
		logger.Error(text, fields...)
	case "warn":
		logger.Warn(text, fields...)
	default:
		logger.Info(text, fields...)
	}
}

// ---------- Read API ----------

func (t *Tracker) Devices() []dto.Device {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]dto.Device, 0, len(t.order))
	for _, imei := range t.order {
		if d, ok := t.state[imei]; ok {
			out = append(out, summary(d))
		}
	}
	return out
}

func (t *Tracker) Device(imei string, limit int) (*dto.DeviceDetail, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	d, ok := t.state[imei]
	if !ok {
		return nil, false
	}
	if limit <= 0 || limit > HistoryMax {
		limit = HistoryMax
	}
	h := d.history
	if len(h) > limit {
		h = h[len(h)-limit:]
	}
	return &dto.DeviceDetail{Device: summary(d), History: append([]dto.HistoryPoint{}, h...)}, true
}

func (t *Tracker) Events(limit int) []dto.FleetEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	ev := t.recent
	if len(ev) > limit {
		ev = ev[len(ev)-limit:]
	}
	return append([]dto.FleetEvent{}, ev...)
}

func (t *Tracker) Logs(limit int) []dto.LogEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	if limit <= 0 {
		limit = 1000
	}
	l := t.logs
	if len(l) > limit {
		l = l[len(l)-limit:]
	}
	return append([]dto.LogEntry{}, l...)
}

// ---------- Write API ----------

// UpdateMeta sets name/plate/vehicle (nil = keep). Registers the device if needed.
func (t *Tracker) UpdateMeta(ctx context.Context, imei string, req dto.UpdateDeviceRequest, userID string) (dto.Device, error) {
	t.mu.Lock()
	d := t.getDevice(imei)
	clean := func(v *string, old *string) *string {
		if v == nil {
			return old
		}
		s := strings.TrimSpace(*v)
		if len(s) > 60 {
			s = s[:60]
		}
		if s == "" {
			return nil
		}
		return &s
	}
	meta := dto.DeviceMeta{Name: clean(req.Name, d.Name), Plate: clean(req.Plate, d.Plate), Vehicle: clean(req.Vehicle, d.Vehicle)}
	t.mu.Unlock()

	isNew, err := t.devices.EnsureDevice(ctx, imei)
	if err != nil {
		return dto.Device{}, err
	}
	if err := t.devices.SetDeviceMeta(ctx, imei, meta, userID); err != nil {
		return dto.Device{}, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	d = t.getDevice(imei)
	if isNew {
		d.FirstSeen = i64p(time.Now().UnixMilli())
		t.counts.Devices++
	}
	d.Name, d.Plate, d.Vehicle = meta.Name, meta.Plate, meta.Vehicle
	t.broadcastDevice(d)
	return summary(d), nil
}

// Delete soft-deletes a device, drops its connection and in-memory state.
func (t *Tracker) Delete(ctx context.Context, imei, userID string) (bool, error) {
	t.mu.Lock()
	_, known := t.state[imei]
	t.mu.Unlock()
	if !known {
		return false, nil
	}
	if _, err := t.devices.DeleteDevice(ctx, imei, userID); err != nil {
		return false, err
	}

	t.mu.Lock()
	if c, ok := t.conns[imei]; ok {
		c.close()
		delete(t.conns, imei)
	}
	t.rejectCommands(imei, "Alat dihapus")
	delete(t.state, imei)
	for i, x := range t.order {
		if x == imei {
			t.order = append(t.order[:i:i], t.order[i+1:]...)
			break
		}
	}
	kept := t.recent[:0]
	for _, ev := range t.recent {
		if ev.Imei != imei {
			kept = append(kept, ev)
		}
	}
	t.recent = kept
	t.counts.Devices--
	t.mu.Unlock()

	t.Log("conn", imei, "Alat dihapus (data record tetap disimpan buat laporan)", "warn")
	t.hub.Broadcast(map[string]any{"type": "removed", "imei": imei})
	return true, nil
}

// SendCommand sends a Codec 12 GPRS command and waits for the device's reply.
func (t *Tracker) SendCommand(ctx context.Context, imei, cmd string) (string, error) {
	t.mu.Lock()
	c, ok := t.conns[imei]
	if !ok {
		t.mu.Unlock()
		return "", ErrOffline
	}
	job := &commandJob{cmd: cmd, reply: make(chan commandResult, 1)}
	t.pending[imei] = append(t.pending[imei], job)
	t.mu.Unlock()

	if err := c.writeCodec12(cmd); err != nil {
		t.dropJob(imei, job)
		return "", err
	}
	t.Log("cmd", imei, ">> "+cmd, "info")
	t.pushCommand(dto.CommandLine{Imei: imei, Dir: "out", Text: cmd, Time: time.Now().UnixMilli()})

	timer := time.NewTimer(commandTimeout)
	defer timer.Stop()
	select {
	case r := <-job.reply:
		return r.text, r.err
	case <-timer.C:
		t.dropJob(imei, job)
		t.Log("cmd", imei, `Perintah "`+cmd+`" nggak dibales dalam 20 detik`, "warn")
		return "", ErrNoReply
	case <-ctx.Done():
		t.dropJob(imei, job)
		return "", ctx.Err()
	}
}

func (t *Tracker) dropJob(imei string, job *commandJob) {
	t.mu.Lock()
	defer t.mu.Unlock()
	q := t.pending[imei]
	for i, j := range q {
		if j == job {
			t.pending[imei] = append(q[:i:i], q[i+1:]...)
			return
		}
	}
}

// rejectCommands fails every waiting command of imei. Caller holds t.mu.
func (t *Tracker) rejectCommands(imei, reason string) {
	for _, job := range t.pending[imei] {
		job.reply <- commandResult{err: errors.New(reason)}
	}
	delete(t.pending, imei)
}
