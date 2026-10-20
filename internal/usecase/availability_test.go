package usecase

import (
	"testing"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/dto"
)

func TestFinishIndicators(t *testing.T) {
	// 24 jam: 2 jam BD, 15 jam working, 4 jam standby, 3 jam tanpa status.
	r := dto.AvailabilityRow{TotalMs: 24 * hourMs, BreakdownMs: 2 * hourMs, WorkingMs: 15 * hourMs, StandbyUnplannedMs: 4 * hourMs}
	finish(&r)
	check := func(name string, got *float64, want float64) {
		if got == nil || *got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("PA", r.PA, 91.7) // 22/24
	check("UA", r.UA, 68.2) // 15/22
	check("MA", r.MA, 88.2) // 15/17
	check("EU", r.EU, 62.5) // 15/24
	if r.NoStatusMs != 3*hourMs {
		t.Errorf("NoStatusMs = %v", r.NoStatusMs)
	}
}

func TestPeriodsSplitByDayAndMonth(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	u := &productionUsecase{report: &config.ReportConfig{Location: loc}}
	from := time.Date(2026, 10, 30, 12, 0, 0, 0, loc).UnixMilli()
	to := time.Date(2026, 11, 2, 6, 0, 0, 0, loc).UnixMilli()
	days := u.periods(from, to, "day")
	if len(days) != 4 || days[0].key != "2026-10-30" || days[3].key != "2026-11-02" || days[0].start != from || days[3].end != to {
		t.Fatalf("days = %+v", days)
	}
	months := u.periods(from, to, "month")
	if len(months) != 2 || months[0].key != "2026-10" || months[1].key != "2026-11" {
		t.Fatalf("months = %+v", months)
	}
	if total := u.periods(from, to, "total"); len(total) != 1 || total[0].end-total[0].start != to-from {
		t.Fatalf("total = %+v", total)
	}
}
