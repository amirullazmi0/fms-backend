package usecase

import (
	"context"
	"math"
	"sort"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
)

// ---------- Rapor operator (P3-6) & laporan idle (P3-7) ----------

const (
	maxOperatorDays = 62
	// disciplineMs: mulai bergerak lebih lambat / berhenti lebih awal dari ini = tercatat.
	disciplineMs = 15 * 60_000
	// defaultIdleLph: konsumsi BBM saat idle bila CAN fuel rate tidak ada (perkiraan DT, L/jam).
	defaultIdleLph = 3.5
)

// OperatorReport: rapor per operator (ritase, cycle time, lama dumping, idle, overspeed, cara
// mengemudi, disiplin shift) dan laporan idle per unit dengan perkiraan BBM terbuang.
func (u *productionUsecase) OperatorReport(ctx context.Context, q dto.ReportQuery) (*dto.OperatorReport, error) {
	f := u.filterOf(dto.ReportQuery{From: q.From, To: q.To})
	if f.To-f.From > maxOperatorDays*24*hourMs {
		return nil, apperror.BadRequest("Periode maksimal 62 hari")
	}
	stats, err := u.repo.AssignmentStats(ctx, f.From, f.To)
	if err != nil {
		return nil, err
	}
	rep := operatorReport(stats)
	rep.From, rep.To = f.From, f.To
	for i := range rep.Idle {
		rep.Idle[i].Unit, _, _ = u.tracker.UnitInfo(rep.Idle[i].Imei)
	}
	return rep, nil
}

// operatorScore: 100 dikurangi overspeed (3 per kejadian, maks 30), mengemudi kasar (1, maks 15),
// telat mulai / berhenti lebih awal (5 per kali, maks 20), dan porsi idle di atas 20% (maks 15).
func operatorScore(o dto.OperatorScore, jobMs int64) int {
	s := 100.0
	s -= math.Min(30, float64(o.Overspeed)*3)
	s -= math.Min(15, float64(o.Harsh))
	s -= math.Min(20, float64(o.LateStarts+o.EarlyEnds)*5)
	if jobMs > 0 {
		idleShare := float64(o.IdleMs) / float64(jobMs)
		s -= math.Min(15, math.Max(0, idleShare-0.2)*50)
	}
	return int(math.Round(math.Max(0, s)))
}

func operatorReport(stats []dto.AssignmentStats) *dto.OperatorReport {
	type acc struct {
		o                 dto.OperatorScore
		jobMs             int64
		cycleSum, dumpSum float64
		cycleN, dumpN     int
	}
	type idleAcc struct {
		idleS, fuelL, jobMs float64
		fromCan             bool
	}
	ops := map[string]*acc{}
	idle := map[string]*idleAcc{}
	for _, s := range stats {
		a := ops[s.DriverID]
		if a == nil {
			a = &acc{o: dto.OperatorScore{DriverID: s.DriverID, DriverName: s.DriverName}}
			ops[s.DriverID] = a
		}
		job := s.WindowEnd - s.WindowStart
		a.jobMs += job
		a.o.Shifts++
		a.o.Ritase += s.Ritase
		a.o.IdleMs += int64(s.IdleS * 1000)
		a.o.Overspeed += s.Overspeed
		a.o.Harsh += s.Harsh
		if s.CycleMs != nil && *s.CycleMs > 0 {
			a.cycleSum += *s.CycleMs * float64(max(1, s.Ritase))
			a.cycleN += max(1, s.Ritase)
		}
		if s.DumpMs != nil {
			a.dumpSum += *s.DumpMs
			a.dumpN++
		}
		// Disiplin: hanya dibandingkan dengan jadwal bila penugasan memang mencakup jam itu.
		if s.FirstMove != nil {
			ref := max(s.PlannedStart, s.WindowStart)
			if late := *s.FirstMove - ref; late > disciplineMs {
				a.o.LateStarts++
				a.o.LateStartMs += late
			}
		}
		if s.LastMove != nil && s.WindowEnd >= s.PlannedEnd {
			if early := s.PlannedEnd - *s.LastMove; early > disciplineMs {
				a.o.EarlyEnds++
				a.o.EarlyEndMs += early
			}
		}

		ia := idle[s.Imei]
		if ia == nil {
			ia = &idleAcc{}
			idle[s.Imei] = ia
		}
		ia.idleS += s.IdleS
		ia.jobMs += float64(job)
		lph := defaultIdleLph
		if s.IdleRateLph != nil && *s.IdleRateLph > 0 {
			lph, ia.fromCan = *s.IdleRateLph, true
		}
		ia.fuelL += s.IdleS / 3600 * lph
	}

	rep := &dto.OperatorReport{Operators: []dto.OperatorScore{}, Idle: []dto.IdleUnit{}, IdleLph: defaultIdleLph}
	for _, a := range ops {
		if a.cycleN > 0 {
			v := a.cycleSum / float64(a.cycleN)
			a.o.CycleMs = &v
		}
		if a.dumpN > 0 {
			v := a.dumpSum / float64(a.dumpN)
			a.o.DumpMs = &v
		}
		a.o.Score = operatorScore(a.o, a.jobMs)
		rep.Operators = append(rep.Operators, a.o)
	}
	sort.Slice(rep.Operators, func(i, j int) bool {
		if rep.Operators[i].Score != rep.Operators[j].Score {
			return rep.Operators[i].Score > rep.Operators[j].Score
		}
		return rep.Operators[i].Ritase > rep.Operators[j].Ritase
	})
	for imei, ia := range idle {
		share := 0.0
		if ia.jobMs > 0 {
			share = round1(ia.idleS * 1000 / ia.jobMs * 100)
		}
		rep.Idle = append(rep.Idle, dto.IdleUnit{Imei: imei, IdleMs: int64(ia.idleS * 1000), FuelL: round1(ia.fuelL),
			FromCan: ia.fromCan, ShareOfJob: share})
	}
	sort.Slice(rep.Idle, func(i, j int) bool { return rep.Idle[i].IdleMs > rep.Idle[j].IdleMs })
	return rep
}
