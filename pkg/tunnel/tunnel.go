// Package tunnel opens a public TCP address so FMC150 trackers can connect over the
// internet (4G). Port of fmctest/server/tunnel.ts + the health check in dev.ts.
//
// Provider (env TUNNEL):
//
//	auto (default) : ngrok when NGROK_AUTHTOKEN is set and works, otherwise bore.pub
//	ngrok          : ngrok only (the account needs card verification for TCP addresses)
//	bore           : bore.pub only (free, no account; the binary is downloaded on first use)
//	0              : no tunnel
//
// ngrok runs in-process (ngrok-go SDK): its listener is served by the tracker directly,
// so no ngrok.exe is needed. bore forwards bore.pub to the local DEVICE_PORT. Only the
// Teltonika protocol is exposed; the dashboard and API stay local.
package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

// HealthID is a fake "IMEI" used to check the tunnel still reaches this server. The
// tracker answers it with 0x00 and does not log it.
const HealthID = "FMC-HEALTH"

const (
	openTimeout   = 20 * time.Second
	probeEvery    = time.Minute
	probeTimeout  = 8 * time.Second
	maxProbeFails = 5
)

// Info is the public address devices are configured with (setparam 2004/2005).
type Info struct {
	Host     string
	Port     int
	Provider string // ngrok | bore
}

// Options configure a Manager.
type Options struct {
	Mode       string // auto | ngrok | bore | 0
	Authtoken  string // NGROK_AUTHTOKEN
	BoreHost   string // default bore.pub
	DevicePort int    // local Teltonika port (bore forwards here)
	DataDir    string // where the bore binary is cached
	// Serve handles device connections arriving on the ngrok listener.
	Serve func(ctx context.Context, ln net.Listener) error
	// OnChange is called with the current address (nil = no tunnel).
	OnChange func(*Info)
	Logf     func(level, format string, args ...any)
}

// active is one open tunnel.
type active struct {
	info  *Info
	close func()
	done  <-chan struct{} // closed when the tunnel died by itself
}

type Manager struct {
	opt Options
	mu  sync.Mutex
	cur *Info
}

func New(opt Options) *Manager {
	opt.Mode = strings.ToLower(strings.TrimSpace(opt.Mode))
	if opt.Mode == "" {
		opt.Mode = "auto"
	}
	if opt.BoreHost == "" {
		opt.BoreHost = "bore.pub"
	}
	if opt.Logf == nil {
		opt.Logf = func(string, string, ...any) {}
	}
	return &Manager{opt: opt}
}

// Enabled reports whether a tunnel is wanted at all.
func (m *Manager) Enabled() bool { return m.opt.Mode != "0" && m.opt.Mode != "off" }

func (m *Manager) set(info *Info) {
	m.mu.Lock()
	m.cur = info
	m.mu.Unlock()
	if m.opt.OnChange != nil {
		m.opt.OnChange(info)
	}
}

// Run keeps a tunnel open until ctx is done: open, probe every minute, rebuild after
// 5 failed probes or when the tunnel drops.
func (m *Manager) Run(ctx context.Context) {
	if !m.Enabled() {
		return
	}
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		t, err := m.open(ctx)
		if err != nil {
			m.opt.Logf("warn", "Tunnel gagal dibuka (%v), dicoba kembali dalam %s", err, backoff)
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 5*time.Minute)
			continue
		}
		backoff = 5 * time.Second
		m.set(t.info)
		m.opt.Logf("info", "Tunnel aktif: %s:%d (%s) ← isi ini di Domain/Port alat", t.info.Host, t.info.Port, t.info.Provider)
		m.watch(ctx, t)
		t.close()
		m.set(nil)
	}
}

// watch probes the public address until the tunnel dies, fails 5 probes in a row, or ctx ends.
func (m *Manager) watch(ctx context.Context, t *active) {
	ticker := time.NewTicker(probeEvery)
	defer ticker.Stop()
	fails := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.done:
			m.opt.Logf("warn", "Tunnel %s terputus, membuat ulang…", t.info.Provider)
			return
		case <-ticker.C:
			ok, known := Probe(t.info.Host, t.info.Port, probeTimeout)
			switch {
			case !known: // DNS blocked here: cannot tell, do not count it as dead
			case ok:
				fails = 0
			default:
				fails++
				m.opt.Logf("warn", "Tunnel tidak terhubung (%d/%d)", fails, maxProbeFails)
				if fails >= maxProbeFails {
					m.opt.Logf("warn", "Tunnel dibuat ulang setelah %d kali gagal diperiksa", fails)
					return
				}
			}
		}
	}
}

func (m *Manager) open(ctx context.Context) (*active, error) {
	if m.opt.Mode == "ngrok" || (m.opt.Mode != "bore" && m.opt.Authtoken != "") {
		t, err := m.openNgrok(ctx)
		if err == nil || m.opt.Mode == "ngrok" {
			return t, err
		}
		m.opt.Logf("warn", "ngrok gagal (%v). Pindah ke %s (gratis, tanpa akun)…", err, m.opt.BoreHost)
	}
	return m.openBore(ctx)
}

func (m *Manager) openNgrok(ctx context.Context) (*active, error) {
	if m.opt.Authtoken == "" {
		return nil, errors.New("NGROK_AUTHTOKEN di .env kosong")
	}
	type result struct {
		tun ngrok.Tunnel
		err error
	}
	ch := make(chan result, 1)
	sessCtx, cancel := context.WithCancel(ctx)
	go func() {
		tun, err := ngrok.Listen(sessCtx, config.TCPEndpoint(), ngrok.WithAuthtoken(m.opt.Authtoken))
		ch <- result{tun, err}
	}()
	var r result
	// Capped at 20 s: with the network down ngrok can wait a long time without an error.
	select {
	case r = <-ch:
	case <-time.After(openTimeout):
		cancel()
		return nil, errors.New("timeout 20 detik, jaringan belum siap?")
	}
	if r.err != nil {
		cancel()
		msg := strings.SplitN(r.err.Error(), "\n", 2)[0]
		if strings.Contains(strings.ToLower(msg), "card") || strings.Contains(msg, "8013") {
			msg += " — ngrok meminta verifikasi kartu untuk alamat TCP: https://dashboard.ngrok.com/settings#id-verification"
		}
		return nil, errors.New(msg)
	}
	host, port, err := parseTCPURL(r.tun.URL())
	if err != nil {
		_ = r.tun.Close()
		cancel()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if m.opt.Serve != nil {
			_ = m.opt.Serve(sessCtx, r.tun)
		}
	}()
	return &active{
		info: &Info{Host: host, Port: port, Provider: "ngrok"},
		close: func() {
			cancel()
			_ = r.tun.Close()
			_ = r.tun.Session().Close()
		},
		done: done,
	}, nil
}

// parseTCPURL splits "tcp://0.tcp.ap.ngrok.io:12345".
func parseTCPURL(u string) (string, int, error) {
	hostport := strings.TrimPrefix(u, "tcp://")
	host, p, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", 0, fmt.Errorf("alamat ngrok aneh %q: %w", u, err)
	}
	port, err := strconv.Atoi(p)
	return host, port, err
}

// Probe dials the public address, sends HealthID and waits for this server's 0x00.
// known is false when the host cannot be resolved here (office DNS often blocks
// ngrok/bore domains while the device, on the carrier's DNS, still finds them).
func Probe(host string, port int, timeout time.Duration) (ok, known bool) {
	ip := resolve(host)
	if ip == "" {
		return false, false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), timeout)
	if err != nil {
		return false, true
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	msg := make([]byte, 2+len(HealthID))
	binary.BigEndian.PutUint16(msg, uint16(len(HealthID)))
	copy(msg[2:], HealthID)
	if _, err := conn.Write(msg); err != nil {
		return false, true
	}
	b := make([]byte, 1)
	if _, err := conn.Read(b); err != nil {
		return false, true
	}
	return b[0] == 0x00, true
}

// resolve looks the host up on public DNS first, then the system resolver.
func resolve(host string) string {
	if net.ParseIP(host) != nil {
		return host
	}
	for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, server)
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		ips, err := r.LookupIPAddr(ctx, host)
		cancel()
		if err == nil {
			for _, ip := range ips {
				if ip.IP.To4() != nil {
					return ip.IP.String()
				}
			}
		}
	}
	ips, err := net.LookupIP(host)
	if err == nil && len(ips) > 0 {
		return ips[0].String()
	}
	return ""
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
