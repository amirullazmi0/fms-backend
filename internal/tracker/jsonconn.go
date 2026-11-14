package tracker

// Jalur JSON: tracker pihak ketiga yang tidak bicara Teltonika mengirim objek JSON lewat
// TCP di port yang sama (DEVICE_PORT). Koneksi dideteksi dari byte pertama: '{' atau '['
// = JSON, selain itu = handshake IMEI Teltonika.
//
// Format yang diterima (satu atau beberapa objek per koneksi, boleh dipisah newline):
//
//	{"data":{"epoch":1794556916,"device_id":50010,"latitude":-6.19,"longitude":106.76,
//	         "altitude":62.1,"speed":0.06,"io1":0,"io2":1,"io3":1,"io4":1,
//	         "flag":"1","rs232":"","rs485":""}}
//
// "data" boleh berupa array, dan objek tanpa pembungkus "data" juga diterima.
// Server membalas satu baris JSON per objek: {"status":"ok","saved":n} atau {"status":"error",...}.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"

	"strconv"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/validator"
)

var jsonIDPattern = validator.DeviceIDPattern

// jsonOfflineAfter: alat JSON biasanya buka-tutup koneksi tiap kirim, jadi status online
// tidak mengikuti socket. Alat dianggap offline kalau tidak ada data selama ini.
const jsonOfflineAfter = 3 * time.Minute

// JSONTrackerModel: tracker_model yang diisi otomatis untuk alat yang masuk lewat jalur JSON.
const JSONTrackerModel = "JSON"

type jsonRecord struct {
	Epoch     json.Number `json:"epoch"`
	DeviceID  any         `json:"device_id"`
	Latitude  float64     `json:"latitude"`
	Longitude float64     `json:"longitude"`
	Altitude  float64     `json:"altitude"`
	Speed     float64     `json:"speed"`
	Angle     *float64    `json:"angle"`
	Sats      *float64    `json:"satellites"`
	Io1       *float64    `json:"io1"`
	Io2       *float64    `json:"io2"`
	Io3       *float64    `json:"io3"`
	Io4       *float64    `json:"io4"`
	Flag      any         `json:"flag"`
	Rs232     string      `json:"rs232"`
	Rs485     string      `json:"rs485"`
}

func isJSONStart(b byte) bool { return b == '{' || b == '[' }

func jsonDeviceID(v any) (string, bool) {
	var id string
	switch x := v.(type) {
	case string:
		id = strings.TrimSpace(x)
	case json.Number:
		id = x.String()
	case float64:
		id = strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return "", false
	}
	return id, jsonIDPattern.MatchString(id)
}

// toAvl maps a JSON record onto the AVL shape the rest of the backend uses.
func (r jsonRecord) toAvl(now int64) (dto.AvlRecord, error) {
	ts := now
	if s := r.Epoch.String(); s != "" {
		f, err := r.Epoch.Float64()
		if err != nil {
			return dto.AvlRecord{}, fmt.Errorf("epoch tidak valid %q", s)
		}
		ts = int64(f)
		if ts < 1e12 { // detik -> milidetik
			ts *= 1000
		}
	}
	if r.Latitude < -90 || r.Latitude > 90 || r.Longitude < -180 || r.Longitude > 180 {
		return dto.AvlRecord{}, fmt.Errorf("koordinat di luar jangkauan (%v, %v)", r.Latitude, r.Longitude)
	}
	rec := dto.AvlRecord{
		AvlHead: dto.AvlHead{
			Time:  ts,
			Lat:   r.Latitude,
			Lng:   r.Longitude,
			Alt:   int(math.Round(r.Altitude)),
			Speed: int(math.Round(r.Speed)),
			Valid: r.Latitude != 0 || r.Longitude != 0,
		},
		Io: dto.IoMap{},
	}
	if r.Angle != nil {
		rec.Angle = int(math.Round(*r.Angle))
	}
	if r.Sats != nil {
		rec.Sats = int(*r.Sats)
	}
	// io1..io4 -> DIN1..DIN4 (AVL ID 1..4).
	for id, v := range map[string]*float64{"1": r.Io1, "2": r.Io2, "3": r.Io3, "4": r.Io4} {
		if v != nil {
			rec.Io[id] = *v
		}
	}
	return rec, nil
}

// decodeJSONPayload accepts {"data":{...}}, {"data":[...]}, {...} or [...].
func decodeJSONPayload(raw json.RawMessage) ([]jsonRecord, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil, errors.New("payload kosong")
	}
	if raw[0] == '{' {
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, err
		}
		if len(env.Data) > 0 && string(env.Data) != "null" {
			return decodeJSONPayload(env.Data)
		}
		var one jsonRecord
		if err := unmarshalNumber(raw, &one); err != nil {
			return nil, err
		}
		return []jsonRecord{one}, nil
	}
	var many []jsonRecord
	if err := unmarshalNumber(raw, &many); err != nil {
		return nil, err
	}
	return many, nil
}

func unmarshalNumber(raw []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	return dec.Decode(v)
}

// handleJSON serves a JSON connection. head holds bytes already read from raw.
func (t *Tracker) handleJSON(ctx context.Context, raw net.Conn, c *deviceConn, head []byte) {
	remote := raw.RemoteAddr().String()
	dec := json.NewDecoder(io.MultiReader(strings.NewReader(string(head)), raw))
	reply := func(v map[string]any) {
		b, _ := json.Marshal(v)
		_ = c.write(append(b, '\n'))
	}
	for {
		_ = raw.SetReadDeadline(time.Now().Add(idleTimeout))
		var msg json.RawMessage
		if err := dec.Decode(&msg); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				var ne net.Error
				if !(errors.As(err, &ne) && ne.Timeout()) {
					t.Log("error", "", fmt.Sprintf("JSON dari %s rusak: %v", remote, err), "warn")
					reply(map[string]any{"status": "error", "error": "JSON rusak: " + err.Error()})
				}
			}
			return
		}
		saved, err := t.ingestJSON(ctx, remote, msg)
		if err != nil {
			t.Log("error", "", fmt.Sprintf("Tolak JSON dari %s: %v", remote, err), "warn")
			reply(map[string]any{"status": "error", "error": err.Error()})
			continue
		}
		reply(map[string]any{"status": "ok", "saved": saved})
	}
}

func (t *Tracker) ingestJSON(ctx context.Context, remote string, msg json.RawMessage) (int, error) {
	items, err := decodeJSONPayload(msg)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	byDevice := map[string][]dto.AvlRecord{}
	var order []string
	for _, it := range items {
		id, ok := jsonDeviceID(it.DeviceID)
		if !ok {
			return 0, fmt.Errorf("device_id tidak valid %v", it.DeviceID)
		}
		rec, err := it.toAvl(now)
		if err != nil {
			return 0, err
		}
		if _, seen := byDevice[id]; !seen {
			order = append(order, id)
		}
		byDevice[id] = append(byDevice[id], rec)
	}
	if len(order) > 0 {
		t.Log("raw", order[0], "<< JSON "+compactJSON(msg), "info")
	}

	total := 0
	for _, id := range order {
		if err := t.touchJSONDevice(ctx, id, remote); err != nil {
			return total, fmt.Errorf("gagal daftarin alat %s: %w", id, err)
		}
		recs := byDevice[id]
		fresh, err := t.devices.SaveRecords(ctx, id, recs)
		if err != nil {
			return total, fmt.Errorf("gagal simpan ke database: %w", err)
		}
		t.mu.Lock()
		t.counts.Records += len(fresh)
		for _, rec := range fresh {
			t.ingest(id, rec, true)
		}
		onLate := t.onLate
		t.mu.Unlock()
		if onLate != nil && len(fresh) > 0 && fresh[0].Time < time.Now().UnixMilli()-lateAfterMs {
			onLate(id, fresh[0].Time, fresh[len(fresh)-1].Time)
		}
		for _, rec := range fresh {
			t.Log("data", id, fmt.Sprintf("JSON %s lat %.6f lng %.6f %d km/h", isoTime(rec.Time), rec.Lat, rec.Lng, rec.Speed), "info")
		}
		if dup := len(recs) - len(fresh); dup > 0 {
			t.Log("data", id, fmt.Sprintf("%d record kiriman ulang dilewati (sudah ada di database)", dup), "info")
		}
		total += len(fresh)
	}
	return total, nil
}

// touchJSONDevice registers the device, marks it online and (re)arms its offline timer.
func (t *Tracker) touchJSONDevice(ctx context.Context, id, remote string) error {
	isNew, err := t.devices.EnsureDevice(ctx, id)
	if err != nil {
		return err
	}
	if err := t.devices.MarkConnected(ctx, id, remote); err != nil {
		return err
	}
	// Model tracker = "JSON" (jalur masuknya pasti), kecuali sudah diisi manual.
	t.mu.Lock()
	needModel := t.getDevice(id).TrackerModel == nil || *t.getDevice(id).TrackerModel == ""
	t.mu.Unlock()
	if needModel {
		if err := t.devices.SetTrackerModelIfEmpty(ctx, id, JSONTrackerModel); err != nil {
			return err
		}
	}
	now := time.Now().UnixMilli()
	t.mu.Lock()
	d := t.getDevice(id)
	if needModel && (d.TrackerModel == nil || *d.TrackerModel == "") {
		d.TrackerModel = strp(JSONTrackerModel)
	}
	wasOnline := d.Online
	d.Online = true
	d.Remote = strp(remote)
	d.LastSeen = i64p(now)
	if !wasOnline {
		d.ConnectedAt = i64p(now)
	}
	if isNew {
		d.FirstSeen = i64p(now)
		t.counts.Devices++
		t.pushEvent(dto.FleetEvent{Time: now, Imei: id, Level: "info", Kind: "new", Text: "Alat baru terdaftar (JSON)"})
	}
	if !wasOnline {
		t.pushEvent(dto.FleetEvent{Time: now, Imei: id, Level: "info", Kind: "online", Text: fmt.Sprintf("Mulai kirim data JSON (%s)", remote)})
	}
	if tm, ok := t.jsonTimers[id]; ok {
		tm.Stop()
	}
	t.jsonTimers[id] = time.AfterFunc(jsonOfflineAfter, func() { t.jsonOffline(id, now) })
	t.broadcastDevice(d)
	t.mu.Unlock()

	if !wasOnline {
		t.Log("conn", id, fmt.Sprintf("Alat JSON terhubung dari %s", remote), "info")
	}
	if isNew {
		t.Log("conn", id, "Alat baru (JSON), ditambahkan ke database", "info")
	}
	return nil
}

func (t *Tracker) jsonOffline(id string, armedAt int64) {
	t.mu.Lock()
	d, ok := t.state[id]
	if !ok || !d.Online || d.LastSeen == nil || *d.LastSeen != armedAt {
		t.mu.Unlock()
		return
	}
	if _, tcp := t.conns[id]; tcp { // sekarang pakai koneksi Teltonika
		t.mu.Unlock()
		return
	}
	delete(t.jsonTimers, id)
	d.Online = false
	t.pushEvent(dto.FleetEvent{Time: time.Now().UnixMilli(), Imei: id, Level: "info", Kind: "offline", Text: fmt.Sprintf("Tidak ada data JSON selama %d menit", int(jsonOfflineAfter.Minutes()))})
	t.broadcastDevice(d)
	t.mu.Unlock()
	t.Log("conn", id, "Alat JSON dianggap offline (tidak ada data)", "info")
}

func compactJSON(b []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return string(b)
	}
	return buf.String()
}
