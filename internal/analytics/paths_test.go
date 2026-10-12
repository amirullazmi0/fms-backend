package analytics

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

// line from a to b in n steps, shifted sideways by off degrees of longitude.
func line(a, b [2]float64, n int, off float64) [][2]float64 {
	out := make([][2]float64, 0, n+1)
	for i := 0; i <= n; i++ {
		f := float64(i) / float64(n)
		out = append(out, [2]float64{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f + off})
	}
	return out
}

func TestUsualOfPicksTheCommonRoad(t *testing.T) {
	a, b := [2]float64{-6.20, 106.80}, [2]float64{-6.17, 106.80}
	via := [2]float64{-6.185, 106.83} // detour ~3 km east
	detour := append(line(a, via, 30, 0), line(via, b, 30, 0)[1:]...)
	trips := []tracedTrip{
		{trip: dto.RouteTrip{End: 3, DurationMs: 600e3, Distance: 3300}, coords: line(a, b, 60, 0)},
		{trip: dto.RouteTrip{End: 2, DurationMs: 660e3, Distance: 3400}, coords: line(a, b, 45, 0.0001)}, // ~11 m beside
		{trip: dto.RouteTrip{End: 4, DurationMs: 900e3, Distance: 6500}, coords: detour},                // latest, but the odd one
	}
	u := usualOf(trips)
	if u == nil {
		t.Fatal("expected a usual path")
	}
	if u.Trips != 3 || u.Compared != 3 {
		t.Fatalf("trips/compared = %d/%d, want 3/3", u.Trips, u.Compared)
	}
	if u.Follow != 2 {
		t.Fatalf("follow = %d, want 2 (the two straight traces)", u.Follow)
	}
	if u.Distance == 6500 {
		t.Fatal("picked the detour instead of the common road")
	}
	if u.LastEnd != 4 {
		t.Fatalf("lastEnd = %d, want 4", u.LastEnd)
	}
	if usualOf(nil) != nil {
		t.Fatal("no trips should give nil")
	}
}
