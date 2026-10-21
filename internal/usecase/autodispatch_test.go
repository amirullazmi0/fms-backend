package usecase

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

func TestAutoDispatchReady(t *testing.T) {
	a := &AutoDispatcher{seen: map[string]int64{}, moved: map[string]int64{}}
	cfg := autoDispatchConfig{StableMin: 10, CooldownMin: 30}
	const min = 60_000
	m := dto.FleetMove{Imei: "X", FromRouteID: "B", ToRouteID: "A"}

	if got := a.ready([]dto.FleetMove{m}, 0, cfg); len(got) != 0 {
		t.Fatal("new suggestion must wait")
	}
	if got := a.ready([]dto.FleetMove{m}, 9*min, cfg); len(got) != 0 {
		t.Fatal("9 min is not stable yet")
	}
	if got := a.ready([]dto.FleetMove{m}, 10*min, cfg); len(got) != 1 {
		t.Fatal("10 min stable: run it")
	}
	// Suggestion disappears, then comes back: the stability clock restarts.
	a.ready(nil, 11*min, cfg)
	if got := a.ready([]dto.FleetMove{m}, 12*min, cfg); len(got) != 0 {
		t.Fatal("reappeared suggestion must wait again")
	}
	// Cooldown: fleet A was just changed.
	a.moved["A"] = 20 * min
	if got := a.ready([]dto.FleetMove{m}, 25*min, cfg); len(got) != 0 {
		t.Fatal("fleet in cooldown")
	}
	if got := a.ready([]dto.FleetMove{m}, 50*min, cfg); len(got) != 1 {
		t.Fatal("after cooldown: run it")
	}
}
