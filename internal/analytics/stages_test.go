package analytics

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

// ~1 m of latitude
const degM = 1.0 / 110540

func rec(t int64, northM float64, speed int) dto.AvlRecord {
	return dto.AvlRecord{AvlHead: dto.AvlHead{Time: t, Lat: northM * degM, Lng: 0, Speed: speed, Valid: true}}
}

const min = 60_000

func TestSplitVisitNoZones(t *testing.T) {
	site := dto.Site{ID: "s", Lat: 0, Lng: 0, Radius: 30}
	// stands still 100 m north for 10 min, then drives in and stays 5 min, then leaves
	records := []dto.AvlRecord{
		rec(0, 1500, 40),
		rec(1*min, 100, 0), rec(6*min, 100, 0), rec(11*min, 100, 2),
		rec(12*min, 0, 5), rec(15*min, 0, 0), rec(17*min, 0, 0),
		rec(18*min, 500, 30), rec(19*min, 900, 30),
	}
	visits := FindVisits(records, []dto.Site{site})
	if len(visits) != 1 {
		t.Fatalf("visits = %d, want 1", len(visits))
	}
	s := SplitVisit(records, site, visits[0])
	if s.QueueMs != 11*min {
		t.Errorf("queue = %d min, want 11", s.QueueMs/min)
	}
	if s.SpotMs != nil {
		t.Errorf("spot = %d, want nil without spot zone", *s.SpotMs)
	}
	if s.WorkMs != visits[0].DwellMs {
		t.Errorf("work = %d, want whole dwell %d", s.WorkMs, visits[0].DwellMs)
	}
}

func TestSplitVisitZones(t *testing.T) {
	site := dto.Site{ID: "s", Lat: 0, Lng: 0, Radius: 60, Zones: []dto.SiteZone{
		{Kind: "queue", Lat: 80 * degM, Lng: 0, Radius: 30}, // queue line partly outside the radius
		{Kind: "spot", Lat: 0, Lng: 0, Radius: 10},
	}}
	records := []dto.AvlRecord{
		rec(0, 1500, 40),
		rec(1*min, 90, 0), rec(4*min, 90, 0), // queue outside the radius: 1..5 = 4 min
		rec(5*min, 55, 0), rec(8*min, 55, 0), // queue inside the radius: 5..10 = 5 min
		rec(10*min, 30, 5), // manoeuvre: 10..11 = 1 min
		rec(11*min, 0, 0), rec(14*min, 0, 0), // spot: 11..16 = 5 min
		rec(16*min, 40, 10), // manoeuvre out: 16..17 = 1 min
		rec(17*min, 300, 30), rec(18*min, 800, 30),
	}
	visits := FindVisits(records, []dto.Site{site})
	if len(visits) != 1 {
		t.Fatalf("visits = %d, want 1", len(visits))
	}
	s := SplitVisit(records, site, visits[0])
	if s.QueueMs != 9*min {
		t.Errorf("queue = %.1f min, want 9", float64(s.QueueMs)/min)
	}
	if s.SpotMs == nil || *s.SpotMs != 5*min {
		t.Errorf("spot = %v, want 5 min", s.SpotMs)
	}
	if s.WorkMs != 2*min {
		t.Errorf("work = %.1f min, want 2", float64(s.WorkMs)/min)
	}
}
