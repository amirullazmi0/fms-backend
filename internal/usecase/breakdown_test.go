package usecase

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

func TestBreakdownReport(t *testing.T) {
	s := func(v string) *string { return &v }
	i := func(v int64) *int64 { return &v }
	const h = hourMs
	logs := []dto.BreakdownLog{
		// DT-01: BD mesin 1 j lalu langsung BD hidrolik 2 j = 1 perbaikan 3 j.
		{Imei: "A", Unit: "DT-01", CodeName: s("BD Mesin"), StartAt: 0, EndAt: i(1 * h)},
		{Imei: "A", Unit: "DT-01", CodeName: s("BD Hidrolik"), StartAt: 1 * h, EndAt: i(3 * h)},
		// DT-01 lagi: BD ban 1 j (setelah sempat working).
		{Imei: "A", Unit: "DT-01", CodeName: s("BD Ban"), StartAt: 5 * h, EndAt: i(6 * h)},
		// DT-02: masih breakdown sejak jam 8.
		{Imei: "B", Unit: "DT-02", CodeName: s("BD Mesin"), StartAt: 8 * h},
	}
	rep := breakdownReport(logs, 10*h)
	if len(rep.Episodes) != 3 || rep.Open != 1 {
		t.Fatalf("episodes = %+v", rep.Episodes)
	}
	var a dto.BreakdownUnit
	for _, u := range rep.Units {
		if u.Imei == "A" {
			a = u
		}
	}
	if a.Count != 2 || a.TotalMs != 4*h || a.MttrMs == nil || *a.MttrMs != 2*h {
		t.Fatalf("unit A = %+v", a)
	}
	if rep.MttrMs == nil || *rep.MttrMs != 2*h { // open BD not in MTTR
		t.Fatalf("mttr = %v", rep.MttrMs)
	}
	last := rep.Episodes[len(rep.Episodes)-1] // oldest
	if len(last.Codes) != 2 || last.Codes[1] != "BD Hidrolik" {
		t.Fatalf("merged codes = %v", last.Codes)
	}
}
