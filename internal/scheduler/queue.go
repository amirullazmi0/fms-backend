package scheduler

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/analytics"
	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
)

// Queue alert (P1-5): too many units queuing too long at one site.
//
// A unit queues at a site when
//   - its last GPS position is in a queue zone of the site (or, for a site without queue zone,
//     it stands still within ApproachM of the radius but not inside it), or
//   - its operator pressed "Antre Loading" / "Antre Dumping" on the MDT for a route through it.
//
// Data from the pit and the barge often arrives late (no signal there), so the alert is only
// as live as the data: until there is LTE at the barge or radio (DMR) positions, it fires when
// the data comes in.
const (
	queueCooldown  = 15 * time.Minute
	gpsFreshMs     = 5 * 60_000  // older positions say nothing about queuing now
	defaultCycleMs = 25 * 60_000 // notes: 1 ritase 22–27 minutes
)

// WithMdt lets the queue alert use the status the operator set on the MDT.
func (s *Scheduler) WithMdt(m repository.MdtRepository) *Scheduler {
	s.mdt = m
	return s
}

type queuer struct {
	imei, label string
	since       int64
}

// queuingByGps: whether the unit is queuing at site now (GPS only).
func (s *Scheduler) queuingByGps(imei string, site dto.Site, now int64) bool {
	pos, speed, ok := s.tracker.Snapshot(imei)
	if !ok || now-pos.Time > gpsFreshMs {
		return false
	}
	hasQueueZone := false
	for _, z := range site.Zones {
		if z.Kind != "queue" {
			continue
		}
		hasQueueZone = true
		if analytics.DistanceM(pos.Lat, pos.Lng, z.Lat, z.Lng) <= float64(z.Radius) {
			return true
		}
	}
	if hasQueueZone {
		return false
	}
	d := analytics.DistanceM(pos.Lat, pos.Lng, site.Lat, site.Lng)
	return speed < analytics.StillKmh && d > float64(site.Radius) && d <= float64(site.Radius)+analytics.ApproachM
}

// cycleMs: average cycle of the route over the last 12 hours (default when no ritase yet).
func (s *Scheduler) cycleMs(ctx context.Context, routeID string, now int64) float64 {
	rep, err := s.trips.Report(ctx, "route", dto.TripFilter{From: now - 12*3600_000, To: now, RouteID: routeID}, "UTC")
	if err != nil || rep.Total.AvgDurationMs <= 0 {
		return defaultCycleMs
	}
	ct := rep.Total.AvgDurationMs + rep.Total.BackAvgMs
	if rep.Total.BackAvgMs <= 0 {
		ct = 2 * rep.Total.AvgDurationMs
	}
	return ct
}

func (s *Scheduler) checkQueues(ctx context.Context, now int64) {
	routes, _, err := s.sites.GetRoutes(ctx, &dto.QueryGlobal{})
	if err != nil {
		return
	}
	var statuses map[string]dto.UnitStatusLog
	if s.mdt != nil {
		if statuses, err = s.mdt.CurrentStatuses(ctx); err != nil {
			statuses = nil
		}
	}

	// Units on duty per site, with the route and the queue code that applies there.
	type duty struct{ imei, routeID, code string }
	bySite := map[string][]duty{}
	siteIDs := []string{}
	for _, r := range routes {
		for _, d := range r.OnDuty {
			if d.Phase != "active" {
				continue
			}
			for _, x := range []struct{ site, code string }{{r.LoadingID, "Q_LOAD"}, {r.UnloadingID, "Q_DUMP"}} {
				if _, ok := bySite[x.site]; !ok {
					siteIDs = append(siteIDs, x.site)
				}
				bySite[x.site] = append(bySite[x.site], duty{d.Imei, r.ID, x.code})
			}
		}
	}
	if len(siteIDs) == 0 {
		return
	}
	sites, err := s.sites.SitesByIDs(ctx, siteIDs, false)
	if err != nil {
		return
	}

	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.queueSince == nil {
		s.queueSince = map[string]int64{}
		s.queueAlerted = map[string]int64{}
	}
	seen := map[string]bool{}
	for _, site := range sites {
		limitMs := int64(site.QueueAlertMin) * 60_000
		var long []queuer
		routeOf := map[string]string{}
		for _, d := range bySite[site.ID] {
			since := int64(0)
			if st, ok := statuses[d.imei]; ok && st.DelayCode != nil && *st.DelayCode == d.code {
				since = st.StartAt
			}
			key := site.ID + "|" + d.imei
			if s.queuingByGps(d.imei, site, now) {
				seen[key] = true
				if _, ok := s.queueSince[key]; !ok {
					s.queueSince[key] = now
				}
				if gps := s.queueSince[key]; since == 0 || gps < since {
					since = gps
				}
			}
			if since > 0 && now-since >= limitMs {
				label, _, _ := s.tracker.UnitInfo(d.imei)
				long = append(long, queuer{imei: d.imei, label: label, since: since})
				routeOf[d.imei] = d.routeID
			}
		}
		if len(long) < max(site.QueueAlertTrucks, 1) || now-s.queueAlerted[site.ID] < queueCooldown.Milliseconds() {
			continue
		}
		s.queueAlerted[site.ID] = now
		sort.Slice(long, func(i, j int) bool { return long[i].since < long[j].since })

		// Production lost ≈ Σ(minutes queued ÷ average cycle) × truck factor.
		lost := 0.0
		names := make([]string, 0, len(long))
		cycles := map[string]float64{}
		for _, q := range long {
			r := routeOf[q.imei]
			if _, ok := cycles[r]; !ok {
				cycles[r] = s.cycleMs(ctx, r, now)
			}
			lost += float64(now-q.since) / cycles[r] * s.tracker.PayloadTon(q.imei)
			names = append(names, fmt.Sprintf("%s %d mnt", q.label, (now-q.since)/60_000))
		}
		lost = math.Round(lost*10) / 10
		text := fmt.Sprintf("%d unit antre lebih dari %d menit di %s: %s. Perkiraan produksi hilang ±%.1f ton.",
			len(long), site.QueueAlertMin, site.Name, strings.Join(names, ", "), lost)
		s.tracker.Notice("warn", "Antrean di "+site.Name, text, "/track")
		s.tracker.Log("conn", "", text, "warn")
		for _, q := range long {
			s.tracker.Alert(q.imei, "warn", "queue", fmt.Sprintf("Antre %d menit di %s", (now-q.since)/60_000, site.Name))
		}
	}
	// A unit that left the queue starts counting again next time.
	for key := range s.queueSince {
		if !seen[key] {
			delete(s.queueSince, key)
		}
	}
}
