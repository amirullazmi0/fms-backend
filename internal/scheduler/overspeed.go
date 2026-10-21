package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/analytics"
	"github.com/amirullazmi0/fms-backend/internal/repository"
)

// Live overspeed (P3-3): a unit inside an active speed zone faster than its limit raises an
// alert (fleet event + notification), at most once per unit & zone per cooldown.
const (
	overspeedEvery    = 30_000
	overspeedCooldown = 5 * time.Minute
	overspeedFreshMs  = 2 * 60_000
)

// WithSpeedZones enables the live overspeed alert.
func (s *Scheduler) WithSpeedZones(z repository.SpeedZoneRepository) *Scheduler {
	s.zones = z
	s.overspeedAt = map[string]int64{}
	return s
}

func (s *Scheduler) checkOverspeed(ctx context.Context, now int64) {
	if s.zones == nil || now-s.lastOverspeed < overspeedEvery {
		return
	}
	s.lastOverspeed = now
	zones, err := s.zones.List(ctx, true)
	if err != nil || len(zones) == 0 {
		return
	}
	for _, d := range s.tracker.Devices() {
		pos, speed, ok := s.tracker.Snapshot(d.Imei)
		if !ok || now-pos.Time > overspeedFreshMs {
			continue
		}
		for _, z := range zones {
			if speed <= z.LimitKmh || analytics.DistanceM(pos.Lat, pos.Lng, z.Lat, z.Lng) > float64(z.Radius) {
				continue
			}
			key := d.Imei + "|" + z.ID
			if now-s.overspeedAt[key] < overspeedCooldown.Milliseconds() {
				continue
			}
			s.overspeedAt[key] = now
			label, _, _ := s.tracker.UnitInfo(d.Imei)
			text := fmt.Sprintf("%s %d km/jam di zona %s (batas %d km/jam)", label, speed, z.Name, z.LimitKmh)
			s.tracker.Alert(d.Imei, "warn", "overspeed_zone", text)
			s.tracker.Notice("warn", "Overspeed di zona", text, "/track?imei="+d.Imei)
		}
	}
}
