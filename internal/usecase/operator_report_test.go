package usecase

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

func TestOperatorReport(t *testing.T) {
	const h = hourMs
	i := func(v int64) *int64 { return &v }
	f := func(v float64) *float64 { return &v }
	stats := []dto.AssignmentStats{
		// Budi: shift 06–18, mulai bergerak 06:05, berhenti 17:55, rapi.
		{AssignmentID: "1", DriverID: "b", DriverName: "Budi", Imei: "A", PlannedStart: 6 * h, PlannedEnd: 18 * h,
			WindowStart: 6 * h, WindowEnd: 18 * h, IdleS: 3600, FirstMove: i(6*h + 5*60_000), LastMove: i(17*h + 55*60_000),
			Ritase: 30, CycleMs: f(20 * 60_000), DumpMs: f(4 * 60_000)},
		// Andi: telat 40 mnt, pulang 1 j lebih awal, 4 overspeed, idle 5 j dengan CAN 4 L/jam.
		{AssignmentID: "2", DriverID: "a", DriverName: "Andi", Imei: "B", PlannedStart: 6 * h, PlannedEnd: 18 * h,
			WindowStart: 6 * h, WindowEnd: 18 * h, IdleS: 5 * 3600, IdleRateLph: f(4), FirstMove: i(6*h + 40*60_000),
			LastMove: i(17 * h), Overspeed: 4, Ritase: 20},
	}
	rep := operatorReport(stats)
	if len(rep.Operators) != 2 || rep.Operators[0].DriverName != "Budi" || rep.Operators[0].Score != 100 {
		t.Fatalf("operators = %+v", rep.Operators)
	}
	andi := rep.Operators[1]
	// 100 − 12 (overspeed) − 10 (telat + pulang awal) − 10.8 (idle 41,7% → (0,417−0,2)×50) = 67
	if andi.LateStarts != 1 || andi.EarlyEnds != 1 || andi.Score != 67 {
		t.Fatalf("andi = %+v", andi)
	}
	var b dto.IdleUnit
	for _, x := range rep.Idle {
		if x.Imei == "B" {
			b = x
		}
	}
	if b.FuelL != 20 || !b.FromCan || b.ShareOfJob != 41.7 {
		t.Fatalf("idle B = %+v", b)
	}
}
