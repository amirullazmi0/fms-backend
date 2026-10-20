package analytics

import "github.com/amirullazmi0/fms-backend/internal/dto"

// Stages of a stay at a loading / unloading site (P1-6), from the site's zones:
//
//	queue = in a queue zone (also the queue just before entering the site radius),
//	spot  = in a spot zone: spotting + loading / dumping,
//	work  = the rest of the time inside the site radius (manoeuvring).
//
// A site without zones cannot tell queueing from loading inside its radius. Then queue is only
// the time the truck stood still right before entering the radius, spot is unknown (nil) and
// work is the whole stay.
const (
	StillKmh       = 3          // slower than this = standing in the queue
	ApproachM      = 300        // without zones: a queue waits at most this far outside the radius
	maxStageGapMs  = 5 * 60_000 // a longer gap between records is not counted to any stage
	queueLookbackM = 2000       // stop looking back for the queue past this distance
)

type Stages struct {
	QueueMs int64
	SpotMs  *int64
	WorkMs  int64
}

func inZone(r *dto.AvlRecord, zones []dto.SiteZone, kind string) bool {
	for _, z := range zones {
		if z.Kind == kind && DistanceM(r.Lat, r.Lng, z.Lat, z.Lng) <= float64(z.Radius) {
			return true
		}
	}
	return false
}

// queueBefore: how long the truck queued right before arriving at the site.
func queueBefore(valid []dto.AvlRecord, site *dto.Site, arrive int64, hasQueueZone bool) int64 {
	// last record before the arrival
	i := len(valid) - 1
	for i >= 0 && valid[i].Time >= arrive {
		i--
	}
	start := arrive
	next := arrive
	for ; i >= 0; i-- {
		r := &valid[i]
		if next-r.Time > maxStageGapMs {
			break
		}
		d := DistanceM(r.Lat, r.Lng, site.Lat, site.Lng)
		if d > queueLookbackM {
			break
		}
		queuing := false
		if hasQueueZone {
			queuing = inZone(r, site.Zones, "queue")
		} else {
			queuing = r.Speed < StillKmh && d <= float64(site.Radius)+ApproachM
		}
		if !queuing {
			break
		}
		start, next = r.Time, r.Time
	}
	return arrive - start
}

// SplitVisit splits one stay (from FindVisits) into queue / spot / work time.
func SplitVisit(records []dto.AvlRecord, site dto.Site, v dto.SiteVisit) Stages {
	valid := make([]dto.AvlRecord, 0, len(records))
	for _, r := range records {
		if r.Valid {
			valid = append(valid, r)
		}
	}
	hasQueue, hasSpot := false, false
	for _, z := range site.Zones {
		hasQueue = hasQueue || z.Kind == "queue"
		hasSpot = hasSpot || z.Kind == "spot"
	}
	out := Stages{QueueMs: queueBefore(valid, &site, v.Arrive, hasQueue)}
	end := v.Arrive + v.DwellMs
	if !hasQueue && !hasSpot {
		out.WorkMs = v.DwellMs
		return out
	}

	var spot int64
	for i := range valid {
		r := &valid[i]
		if r.Time < v.Arrive || r.Time >= end {
			continue
		}
		until := end
		if i+1 < len(valid) && valid[i+1].Time < end {
			until = valid[i+1].Time
		}
		dt := until - r.Time
		if dt > maxStageGapMs {
			dt = 0
		}
		switch {
		case hasSpot && inZone(r, site.Zones, "spot"):
			spot += dt
		case hasQueue && inZone(r, site.Zones, "queue"):
			out.QueueMs += dt
		default:
			out.WorkMs += dt
		}
	}
	if hasSpot {
		out.SpotMs = &spot
	}
	return out
}
