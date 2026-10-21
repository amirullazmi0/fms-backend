package analytics

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

// A route with two stockpiles / jetties: a ritase ends at whichever one the truck reaches.
func TestBuildReportManyUnloadings(t *testing.T) {
	pit := dto.Site{ID: "pit", Name: "Pit", Lat: 0, Lng: 0, Radius: 50}
	jettyA := dto.Site{ID: "a", Name: "Jetty A", Lat: 3000 * degM, Lng: 0, Radius: 50}
	jettyB := dto.Site{ID: "b", Name: "Jetty B", Lat: -3000 * degM, Lng: 0, Radius: 50}
	route := dto.Route{ID: "r", Name: "Pit → Jetty", LoadingID: "pit", UnloadingID: "a", UnloadingIDs: []string{"a", "b"}}

	records := []dto.AvlRecord{}
	at := int64(0)
	stay := func(northM float64, minutes int) {
		for i := 0; i < minutes*2; i++ {
			records = append(records, rec(at, northM, 0))
			at += 30_000
		}
	}
	drive := func(fromM, toM float64) {
		for i := 1; i <= 10; i++ {
			records = append(records, rec(at, fromM+(toM-fromM)*float64(i)/10, 40))
			at += 30_000
		}
	}
	stay(0, 5)
	drive(0, 3000)
	stay(3000, 5)
	drive(3000, 0)
	stay(0, 5)
	drive(0, -3000)
	stay(-3000, 5)
	drive(-3000, 0)
	stay(0, 2)

	rep := BuildReport(records, []dto.Route{route}, []dto.Site{pit, jettyA, jettyB}, 0, at, at+OngoingMs*2)
	got := []string{}
	for _, tr := range rep.Trips {
		if tr.Leg == "go" && tr.Status == "done" && tr.Unloading != nil {
			got = append(got, tr.Unloading.SiteID)
		}
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("hauling done to %v, want [a b]", got)
	}
}
