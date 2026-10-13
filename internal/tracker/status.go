package tracker

import (
	"net"
	"strconv"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/tunnel"
)

// Status is the server card on the Live page.
func (t *Tracker) Status() dto.ServerStatus {
	port, _ := strconv.Atoi(t.cfg.App.Port)
	devicePort, _ := strconv.Atoi(t.cfg.Device.Port)
	t.mu.Lock()
	defer t.mu.Unlock()
	var tunnel *dto.TunnelInfo
	if t.tunnel != nil {
		cp := *t.tunnel
		tunnel = &cp
	}
	return dto.ServerStatus{
		StartedAt:     t.startedAt,
		Port:          port,
		LanIps:        lanIps(),
		Tunnel:        tunnel,
		TunnelEnabled: t.cfg.Device.Tunnel,
		DevicePort:    devicePort,
		DB:            t.counts,
		Connected:     len(t.conns),
		Devices:       len(t.state),
	}
}

func lanIps() []string {
	out := []string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

// SetTunnel records the public device address (from the tunnel manager or the
// TUNNEL_HOST/TUNNEL_PORT override). nil = no tunnel.
func (t *Tracker) SetTunnel(info *tunnel.Info) {
	if info == nil {
		t.setTunnel(nil)
		return
	}
	t.setTunnel(&dto.TunnelInfo{Host: info.Host, Port: info.Port, Provider: info.Provider})
}

func (t *Tracker) setTunnel(v *dto.TunnelInfo) {
	t.mu.Lock()
	same := (v == nil && t.tunnel == nil) || (v != nil && t.tunnel != nil && *v == *t.tunnel)
	t.tunnel = v
	t.mu.Unlock()
	_ = same // the tunnel manager logs address changes; /api/status carries the value
}
