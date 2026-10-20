package usecase

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

func TestMatchFactor(t *testing.T) {
	// 4 DT, muat 3 mnt, CT 20 mnt: MF = 4×3 / 20 = 0,6 (undermatch), ideal 6,67 DT.
	mf, class, ideal := matchFactor(4, 1, 3*60_000, 20*60_000)
	if mf == nil || *mf != 0.6 || class != "under" || ideal == nil || *ideal != 6.67 {
		t.Fatalf("got %v %q %v", mf, class, ideal)
	}
	if mf, class, _ := matchFactor(7, 1, 3*60_000, 20*60_000); *mf != 1.05 || class != "match" {
		t.Fatalf("7 DT: %v %q", *mf, class)
	}
	if mf, class, _ := matchFactor(9, 1, 3*60_000, 20*60_000); *mf != 1.35 || class != "over" {
		t.Fatalf("9 DT: %v %q", *mf, class)
	}
	if mf, _, ideal := matchFactor(0, 1, 3*60_000, 20*60_000); mf != nil || ideal == nil {
		t.Fatalf("0 DT: %v %v", mf, ideal)
	}
}

func TestCoveredMs(t *testing.T) {
	stays := []dto.StayInterval{{Start: 10, End: 30}, {Start: 20, End: 40}, {Start: 50, End: 60}, {Start: 95, End: 200}}
	if got := coveredMs(stays, 0, 100); got != 30+10+5 {
		t.Fatalf("covered = %d", got)
	}
	if got := coveredMs(nil, 0, 100); got != 0 {
		t.Fatalf("empty = %d", got)
	}
}

func TestSuggestMoves(t *testing.T) {
	loader := &dto.FleetLoader{Imei: "1"}
	trucks := func(n int) []dto.FleetTruck {
		out := make([]dto.FleetTruck, n)
		for i := range out {
			out[i] = dto.FleetTruck{Imei: string(rune('a' + i)), Label: "DT"}
		}
		return out
	}
	fleet := func(id string, n int) dto.Fleet {
		f := dto.Fleet{RouteID: id, RouteName: id, Loader: loader, Trucks: trucks(n), Active: n, LoadTimeMs: 3 * 60_000, CycleMs: 20 * 60_000}
		f.MatchFactor, f.Match, f.IdealTrucks = matchFactor(n, 1, f.LoadTimeMs, f.CycleMs)
		return f
	}
	// A: 4 DT (MF 0,6, excavator menunggu), B: 10 DT (MF 1,5, DT antre) → pindahkan 1 DT B → A.
	moves := suggestMoves([]dto.Fleet{fleet("A", 4), fleet("B", 10)})
	if len(moves) != 1 || moves[0].FromRouteID != "B" || moves[0].ToRouteID != "A" || *moves[0].ToMfAfter != 0.75 {
		t.Fatalf("moves = %+v", moves)
	}
	// Both balanced: no suggestion.
	if moves := suggestMoves([]dto.Fleet{fleet("A", 7), fleet("B", 7)}); len(moves) != 0 {
		t.Fatalf("balanced moves = %+v", moves)
	}
}
