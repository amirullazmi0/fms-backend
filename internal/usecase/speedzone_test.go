package usecase

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

func TestOverspeedEpisodes(t *testing.T) {
	budi := "Budi"
	p := func(imei, zone string, at int64, speed int) dto.OverspeedPoint {
		return dto.OverspeedPoint{Imei: imei, ZoneID: zone, ZoneName: zone, LimitKmh: 30, Time: at, Speed: speed, DriverName: &budi}
	}
	points := []dto.OverspeedPoint{
		p("A", "turunan", 0, 35), p("A", "turunan", 10_000, 42), p("A", "turunan", 20_000, 38), // 1 episode
		p("A", "turunan", 200_000, 33), // > 60 s later: new episode
		p("B", "turunan", 5_000, 31),   // other unit
	}
	eps := overspeedEpisodes(points)
	if len(eps) != 3 || eps[0].MaxSpeed != 42 || eps[0].EndAt != 20_000 || eps[0].Points != 3 {
		t.Fatalf("episodes = %+v", eps)
	}
	drivers := overspeedDrivers(eps)
	if len(drivers) != 1 || drivers[0].Episodes != 3 || drivers[0].MaxOver != 12 || drivers[0].TotalMs != 20_000 {
		t.Fatalf("drivers = %+v", drivers)
	}
}
