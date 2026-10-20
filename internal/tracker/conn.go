package tracker

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/teltonika"
	"github.com/amirullazmi0/fms-backend/pkg/tunnel"
)

var imeiPattern = regexp.MustCompile(`^\d{15}$`)

// idleTimeout drops a silent connection; the device reconnects and resends un-ACKed data.
const idleTimeout = 30 * time.Minute

// lateAfterMs: a record older than this when stored was buffered on the device (no signal).
const lateAfterMs = 2 * 60_000

type deviceConn struct {
	conn    net.Conn
	writeMu sync.Mutex
	once    sync.Once
}

func (c *deviceConn) write(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := c.conn.Write(b)
	return err
}

func (c *deviceConn) writeCodec12(cmd string) error {
	return c.write(teltonika.BuildCodec12(cmd, teltonika.Codec12Command))
}

func (c *deviceConn) close() {
	c.once.Do(func() { _ = c.conn.Close() })
}

func isoTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// Serve accepts FMC150 TCP connections on DEVICE_PORT until ctx is done.
func (t *Tracker) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", ":"+t.cfg.Device.Port)
	if err != nil {
		return err
	}
	t.Log("conn", "", "Port alat (Teltonika TCP) dengerin di :"+t.cfg.Device.Port, "info")
	return t.ServeListener(ctx, ln)
}

// ServeListener handles device connections from any listener (local port or the
// in-process ngrok tunnel) until ctx is done or the listener closes.
func (t *Tracker) ServeListener(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go t.handle(ctx, c)
	}
}

func (t *Tracker) handle(ctx context.Context, raw net.Conn) {
	c := &deviceConn{conn: raw}
	defer c.close()
	remote := raw.RemoteAddr().String()
	if tc, ok := raw.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(time.Minute)
	}

	// Handshake: 2-byte length + IMEI as ASCII. Reply 01 = accepted, 00 = rejected.
	_ = raw.SetReadDeadline(time.Now().Add(time.Minute))
	head := make([]byte, 2)
	if _, err := io.ReadFull(raw, head); err != nil {
		return
	}
	n := int(binary.BigEndian.Uint16(head))
	if n == 0 || n > 64 {
		t.Log("conn", "", fmt.Sprintf("Tolak %s: bukan protokol Teltonika", remote), "warn")
		return
	}
	idBuf := make([]byte, n)
	if _, err := io.ReadFull(raw, idBuf); err != nil {
		return
	}
	imei := string(idBuf)
	if imei == tunnel.HealthID {
		_ = c.write([]byte{0x00}) // tunnel health check: answer quietly
		return
	}
	if !imeiPattern.MatchString(imei) {
		t.Log("conn", "", fmt.Sprintf(`Tolak %s: IMEI nggak valid "%s"`, remote, imei), "warn")
		_ = c.write([]byte{0x00})
		return
	}

	if err := t.accept(ctx, imei, remote, c); err != nil {
		t.Log("error", imei, "Gagal daftarin alat: "+err.Error(), "error")
		return
	}
	defer t.disconnect(imei, c)

	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		_ = raw.SetReadDeadline(time.Now().Add(idleTimeout))
		n, err := raw.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			for {
				frame, rest, ok, ferr := teltonika.TakeFrame(buf)
				if ferr != nil {
					t.Log("error", imei, "Paket rusak: "+ferr.Error(), "error")
					return
				}
				if !ok {
					break
				}
				if err := t.handleFrame(ctx, imei, c, frame); err != nil {
					// Drop the connection: the device reconnects and resends un-ACKed records.
					t.Log("error", imei, "Gagal proses data alat: "+err.Error(), "error")
					return
				}
				buf = append(buf[:0], rest...)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				t.Log("error", imei, fmt.Sprintf("Socket %s: %v", remote, err), "error")
			}
			return
		}
	}
}

// accept registers a connected device and answers the handshake.
func (t *Tracker) accept(ctx context.Context, imei, remote string, c *deviceConn) error {
	isNew, err := t.devices.EnsureDevice(ctx, imei)
	if err != nil {
		return err
	}
	if err := t.devices.MarkConnected(ctx, imei, remote); err != nil {
		return err
	}
	var demo *dto.DeviceMeta
	if isNew && strings.HasPrefix(imei, "3500000000000") {
		m := demoTrucks[int(imei[len(imei)-1]-'0')%len(demoTrucks)]
		if err := t.devices.SetDeviceMeta(ctx, imei, m, ""); err == nil {
			demo = &m
		}
	}
	if err := c.write([]byte{0x01}); err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	t.mu.Lock()
	if old, ok := t.conns[imei]; ok && old != c {
		old.close()
	}
	t.conns[imei] = c
	d := t.getDevice(imei)
	d.Online = true
	d.Remote = strp(remote)
	d.ConnectedAt = i64p(now)
	d.LastSeen = i64p(now)
	if isNew {
		d.FirstSeen = i64p(now)
		t.counts.Devices++
		t.pushEvent(dto.FleetEvent{Time: now, Imei: imei, Level: "info", Kind: "new", Text: "Alat baru terdaftar"})
		if demo != nil {
			d.Name, d.Plate, d.Vehicle = demo.Name, demo.Plate, demo.Vehicle
		}
	}
	t.pushEvent(dto.FleetEvent{Time: now, Imei: imei, Level: "info", Kind: "online", Text: fmt.Sprintf("Koneksi dibuka (%s)", remote)})
	t.broadcastDevice(d)
	t.mu.Unlock()

	t.Log("conn", imei, fmt.Sprintf("Konek dari %s, IMEI diterima (bales 01)", remote), "info")
	if isNew {
		t.Log("conn", imei, "Alat baru, ditambahin ke database", "info")
	}
	return nil
}

func (t *Tracker) disconnect(imei string, c *deviceConn) {
	t.mu.Lock()
	cur, ok := t.conns[imei]
	_, known := t.state[imei]
	if !ok || cur != c || !known {
		t.mu.Unlock()
		return
	}
	delete(t.conns, imei)
	d := t.getDevice(imei)
	d.Online = false
	t.rejectCommands(imei, "Koneksi alat putus")
	t.pushEvent(dto.FleetEvent{Time: time.Now().UnixMilli(), Imei: imei, Level: "info", Kind: "offline", Text: "Koneksi ditutup"})
	t.broadcastDevice(d)
	t.mu.Unlock()
	t.Log("conn", imei, "Koneksi ditutup", "info")
}

func (t *Tracker) sendAck(c *deviceConn, count int) ([]byte, error) {
	ack := make([]byte, 4)
	binary.BigEndian.PutUint32(ack, uint32(count))
	return ack, c.write(ack)
}

// handleFrame processes one packet. A returned error drops the connection.
func (t *Tracker) handleFrame(ctx context.Context, imei string, c *deviceConn, frame []byte) error {
	t.mu.Lock()
	t.getDevice(imei).LastSeen = i64p(time.Now().UnixMilli())
	t.mu.Unlock()
	t.Log("raw", imei, fmt.Sprintf("<< %d byte %s", len(frame), hex.EncodeToString(frame)), "info")

	if !teltonika.CRCOk(frame) {
		t.Log("error", imei, "CRC salah, paket diabaikan (alat bakal kirim ulang)", "error")
		return nil
	}
	codec := teltonika.CodecID(frame)

	if codec == teltonika.Codec12 {
		_, text, err := teltonika.ParseCodec12(frame)
		if err != nil {
			t.Log("error", imei, "Codec 12 rusak: "+err.Error(), "error")
			return nil
		}
		t.Log("cmd", imei, "<< "+text, "info")
		t.mu.Lock()
		if q := t.pending[imei]; len(q) > 0 {
			job := q[0]
			t.pending[imei] = q[1:]
			job.reply <- commandResult{text: text}
		}
		t.mu.Unlock()
		t.pushCommand(dto.CommandLine{Imei: imei, Dir: "in", Text: text, Time: time.Now().UnixMilli()})
		return nil
	}

	if codec != teltonika.Codec8 && codec != teltonika.Codec8E {
		t.Log("error", imei, fmt.Sprintf("Codec 0x%x belum didukung, tetap di-ACK", codec), "warn")
		_, err := t.sendAck(c, int(frame[9]))
		return err
	}

	parsed, err := teltonika.ParseAvl(frame)
	if err != nil {
		t.Log("error", imei, "Gagal decode: "+err.Error(), "error")
		return nil
	}

	// Store first, then ACK. If storing fails there is no ACK, so the device resends.
	fresh, err := t.devices.SaveRecords(ctx, imei, parsed.Records)
	if err != nil {
		t.Log("error", imei, "Gagal nyimpen ke database: "+err.Error(), "error")
		return nil
	}
	sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].Time < fresh[j].Time })
	lines := make([]string, 0, len(fresh))
	t.mu.Lock()
	t.counts.Records += len(fresh)
	for _, rec := range fresh {
		t.ingest(imei, rec, true)
		lines = append(lines, describeRecord(rec, parsed.Codec))
	}
	onLate := t.onLate
	t.mu.Unlock()
	// Records from a blank spot arrive in bulk, long after their GPS time: the ritase of the
	// shift they belong to must be counted again (any shift, however old).
	if onLate != nil && len(fresh) > 0 && fresh[0].Time < time.Now().UnixMilli()-lateAfterMs {
		onLate(imei, fresh[0].Time, fresh[len(fresh)-1].Time)
	}
	for _, l := range lines {
		t.Log("data", imei, l, "info")
	}
	if dup := parsed.Count - len(fresh); dup > 0 {
		t.Log("data", imei, fmt.Sprintf("%d record kiriman ulang dilewati (udah ada di database)", dup), "info")
	}

	ack, err := t.sendAck(c, parsed.Count)
	if err != nil {
		return err
	}
	t.Log("ack", imei, fmt.Sprintf(">> ACK %s (%d record)", hex.EncodeToString(ack), parsed.Count), "info")
	return nil
}
