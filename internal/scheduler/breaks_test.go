package scheduler

import (
	"testing"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

func TestBreakWindow(t *testing.T) {
	loc := time.FixedZone("WIB", 7*3600)
	at := func(d, h, m int) int64 { return time.Date(2026, 10, d, h, m, 0, 0, loc).UnixMilli() }

	// Shift pagi 06:00–18:00, Ishoma 12:00–13:00: same day.
	s, e, ok := breakWindow(dto.ShiftBreak{Start: "12:00", End: "13:00", Code: "ISHOMA"}, at(21, 6, 0), loc)
	if !ok || s != at(21, 12, 0) || e != at(21, 13, 0) {
		t.Fatalf("pagi: %v %v %v", s, e, ok)
	}
	// Shift malam 18:00–06:00, istirahat 00:00–01:00: next day.
	s, e, ok = breakWindow(dto.ShiftBreak{Start: "00:00", End: "01:00", Code: "ISHOMA"}, at(21, 18, 0), loc)
	if !ok || s != at(22, 0, 0) || e != at(22, 1, 0) {
		t.Fatalf("malam: %v %v %v", s, e, ok)
	}
	// Break across midnight: 23:30–00:30.
	s, e, ok = breakWindow(dto.ShiftBreak{Start: "23:30", End: "00:30", Code: "ISHOMA"}, at(21, 18, 0), loc)
	if !ok || s != at(21, 23, 30) || e != at(22, 0, 30) {
		t.Fatalf("lewat tengah malam: %v %v %v", s, e, ok)
	}
}
