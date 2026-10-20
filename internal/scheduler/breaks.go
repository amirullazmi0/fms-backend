package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

// Scheduled breaks (P2-3): while a break of a running shift lasts, its units are on planned
// standby with the break's delay code (e.g. ISHOMA), set automatically (source auto). At the end
// they return to their previous status, unless the operator already changed it.
const (
	breaksEvery = 30_000
	// breakGraceMs: the break status is only set this long after the break started (server down
	// longer = the operator decides).
	breakGraceMs = 10 * 60_000
)

// WithLocation sets the time zone of the break times (the report time zone).
func (s *Scheduler) WithLocation(loc *time.Location) *Scheduler {
	s.loc = loc
	return s
}

// breakWindow: the break as absolute times within the shift. The break starts on the first
// occurrence of its HH:MM at or after the planned start; an end before the start = next day.
func breakWindow(b dto.ShiftBreak, plannedStart int64, loc *time.Location) (int64, int64, bool) {
	sh, err1 := time.Parse("15:04", b.Start)
	eh, err2 := time.Parse("15:04", b.End)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	ps := time.UnixMilli(plannedStart).In(loc)
	start := time.Date(ps.Year(), ps.Month(), ps.Day(), sh.Hour(), sh.Minute(), 0, 0, loc)
	if start.UnixMilli() < plannedStart {
		start = start.AddDate(0, 0, 1)
	}
	end := time.Date(start.Year(), start.Month(), start.Day(), eh.Hour(), eh.Minute(), 0, 0, loc)
	if !end.After(start) {
		end = end.AddDate(0, 0, 1)
	}
	return start.UnixMilli(), end.UnixMilli(), true
}

func (s *Scheduler) checkBreaks(ctx context.Context, now int64) {
	if s.mdt == nil || s.loc == nil || now-s.lastBreaks < breaksEvery {
		return
	}
	s.lastBreaks = now
	active, err := s.shifts.ActiveShifts(ctx)
	if err != nil {
		return
	}
	changed := false
	for _, sh := range active {
		for _, b := range sh.Breaks {
			start, end, ok := breakWindow(b, sh.PlannedStart, s.loc)
			if !ok || end > sh.PlannedEnd+breakGraceMs {
				continue
			}
			var units []string
			var err error
			switch {
			case now >= start && now < end && now < start+breakGraceMs:
				units, err = s.mdt.StartBreak(ctx, sh.ID, b.Code, start)
				if err == nil && len(units) > 0 {
					s.log(fmt.Sprintf("Istirahat %s %s–%s (shift %s): %d unit standby terencana", b.Code, b.Start, b.End, sh.Name, len(units)))
				}
			case now >= end:
				units, err = s.mdt.EndBreak(ctx, sh.ID, b.Code, start, end)
				if err == nil && len(units) > 0 {
					s.log(fmt.Sprintf("Istirahat %s selesai (shift %s): %d unit kembali ke status sebelumnya", b.Code, sh.Name, len(units)))
				}
			}
			if err != nil {
				s.log("Gagal mengatur status istirahat: " + err.Error())
				continue
			}
			for _, imei := range units {
				s.tracker.Broadcast(map[string]any{"type": "unit_status", "imei": imei})
				changed = true
			}
		}
	}
	if changed {
		s.tracker.Changed("unit_status")
	}
}

