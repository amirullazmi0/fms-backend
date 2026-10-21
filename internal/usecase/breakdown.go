package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
)

// ---------- Log breakdown & durasi perbaikan (P3-5) ----------

const maxBreakdownDays = 400

// Breakdowns: breakdown episodes in the period with repair duration, MTTR per unit and per BD type.
func (u *productionUsecase) Breakdowns(ctx context.Context, q dto.ReportQuery) (*dto.BreakdownReport, error) {
	f := u.filterOf(dto.ReportQuery{From: q.From, To: q.To})
	if f.To-f.From > maxBreakdownDays*24*hourMs {
		return nil, apperror.BadRequest("Periode maksimal 400 hari")
	}
	logs, err := u.repo.BreakdownLogs(ctx, f.From, f.To)
	if err != nil {
		return nil, err
	}
	rep := breakdownReport(logs, time.Now().UnixMilli())
	rep.From, rep.To = f.From, f.To
	return rep, nil
}

func avgMs(total int64, n int) *int64 {
	if n == 0 {
		return nil
	}
	v := total / int64(n)
	return &v
}

// breakdownReport: merges consecutive BD logs per unit (logs ordered by unit, start) into episodes.
func breakdownReport(logs []dto.BreakdownLog, now int64) *dto.BreakdownReport {
	eps := []dto.Breakdown{}
	for _, l := range logs {
		name := "Breakdown"
		if l.CodeName != nil {
			name = *l.CodeName
		}
		if n := len(eps); n > 0 {
			e := &eps[n-1]
			if e.Imei == l.Imei && e.EndAt != nil && *e.EndAt == l.StartAt {
				e.EndAt = l.EndAt
				if e.Codes[len(e.Codes)-1] != name {
					e.Codes = append(e.Codes, name)
				}
				if l.Note != nil {
					e.Note = l.Note
				}
				continue
			}
		}
		eps = append(eps, dto.Breakdown{Imei: l.Imei, Unit: l.Unit, Codes: []string{name}, DriverName: l.DriverName,
			Note: l.Note, StartAt: l.StartAt, EndAt: l.EndAt})
	}

	rep := &dto.BreakdownReport{Units: []dto.BreakdownUnit{}, Codes: []dto.BreakdownCode{}}
	units := map[string]*dto.BreakdownUnit{}
	unitClosed := map[string][2]int64{} // imei → {closed count, closed total}
	codes := map[string]*dto.BreakdownCode{}
	var allClosed, allClosedMs int64
	for i := range eps {
		e := &eps[i]
		end := now
		if e.EndAt != nil {
			end = *e.EndAt
		}
		e.DurationMs = end - e.StartAt
		un := units[e.Imei]
		if un == nil {
			un = &dto.BreakdownUnit{Imei: e.Imei, Unit: e.Unit}
			units[e.Imei] = un
		}
		un.Count++
		un.TotalMs += e.DurationMs
		if e.EndAt == nil {
			un.OpenNow = true
			rep.Open++
		} else {
			c := unitClosed[e.Imei]
			unitClosed[e.Imei] = [2]int64{c[0] + 1, c[1] + e.DurationMs}
			allClosed++
			allClosedMs += e.DurationMs
		}
		code := codes[e.Codes[0]]
		if code == nil {
			code = &dto.BreakdownCode{Name: e.Codes[0]}
			codes[e.Codes[0]] = code
		}
		code.Count++
		code.TotalMs += e.DurationMs
	}
	for imei, un := range units {
		c := unitClosed[imei]
		un.MttrMs = avgMs(c[1], int(c[0]))
		rep.Units = append(rep.Units, *un)
	}
	sort.Slice(rep.Units, func(i, j int) bool { return rep.Units[i].TotalMs > rep.Units[j].TotalMs })
	for _, c := range codes {
		rep.Codes = append(rep.Codes, *c)
	}
	sort.Slice(rep.Codes, func(i, j int) bool { return rep.Codes[i].TotalMs > rep.Codes[j].TotalMs })
	sort.Slice(eps, func(i, j int) bool { return eps[i].StartAt > eps[j].StartAt })
	rep.Episodes = eps
	rep.MttrMs = avgMs(allClosedMs, int(allClosed))
	return rep
}
