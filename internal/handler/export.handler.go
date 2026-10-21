package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// Ekspor Excel laporan (P3-8). Satu file .xlsx per jenis laporan, angka tetap angka (bisa dihitung
// di Excel), waktu dalam zona laporan.
type ExportHandler struct {
	shift      usecase.ShiftUsecase
	production usecase.ProductionUsecase
	loc        *time.Location
}

func NewExportHandler(shift usecase.ShiftUsecase, production usecase.ProductionUsecase, loc *time.Location) *ExportHandler {
	return &ExportHandler{shift: shift, production: production, loc: loc}
}

type exportQuery struct {
	dto.ReportQuery
	Type string `form:"type" validate:"required,oneof=ritase hourly availability operators breakdowns"`
}

const maxExportTrips = 100_000

type sheet struct {
	name   string
	header []string
	rows   [][]any
}

// Export godoc
// @Summary Ekspor laporan ke Excel (.xlsx): ritase, hourly, availability, operators, breakdowns
// @Tags reports
// @Security BearerAuth
// @Param type query string true "ritase | hourly | availability | operators | breakdowns"
// @Param from query int false "Start (ms)"
// @Param to query int false "End (ms)"
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Router /api/reports/export [get]
func (h *ExportHandler) Export(c *gin.Context) {
	var q exportQuery
	if !bindQuery(c, &q) {
		return
	}
	ctx := c.Request.Context()
	t := func(ms int64) any { return time.UnixMilli(ms).In(h.loc).Format("2006-01-02 15:04:05") }
	tp := func(ms *int64) any {
		if ms == nil {
			return ""
		}
		return t(*ms)
	}
	minutes := func(ms float64) any { return float64(int(ms/60_000*10+0.5)) / 10 }
	str := func(s *string) any {
		if s == nil {
			return ""
		}
		return *s
	}
	num := func(v *float64) any {
		if v == nil {
			return ""
		}
		return *v
	}

	var sheets []sheet
	switch q.Type {
	case "ritase":
		s := sheet{name: "Ritase", header: []string{"Berangkat", "Tiba", "Tanggal shift", "Shift", "Rute", "Arah", "Unit", "Operator",
			"Front loading", "Dumping", "Durasi (mnt)", "Jarak (km)", "Berhenti", "Kecepatan maks", "Kecepatan rata-rata",
			"Lama di front (mnt)", "Lama di dumping (mnt)", "Tonase (t)", "BBM terpakai", "Status"}}
		for offset := 0; offset < maxExportTrips; offset += 2000 {
			q.Limit, q.Offset = 2000, offset
			list, total, err := h.shift.ReportTrips(ctx, q.ReportQuery)
			if err != nil {
				respondError(c, err, "Failed to export trips")
				return
			}
			for _, r := range list {
				leg := "Hauling"
				if r.Leg != "go" {
					leg = "Return"
				}
				var unload any = ""
				if r.UnloadDwellMs != nil {
					unload = minutes(float64(*r.UnloadDwellMs))
				}
				s.rows = append(s.rows, []any{t(r.DepartAt), tp(r.ArriveAt), str(r.ShiftDate), str(r.ShiftName), r.RouteName, leg,
					str(r.Truck), str(r.DriverName), r.LoadingName, r.UnloadingName, minutes(float64(r.DurationMs)),
					float64(int(r.Distance/100+0.5)) / 10, r.Stops, r.MaxSpeed, r.AvgSpeed, minutes(float64(r.LoadDwellMs)), unload,
					num(r.PayloadTon), num(r.FuelUsed), r.Status})
			}
			if offset+len(list) >= total || len(list) == 0 {
				break
			}
		}
		sheets = append(sheets, s)
	case "hourly":
		r, err := h.production.Hourly(ctx, q.ReportQuery)
		if err != nil {
			respondError(c, err, "Failed to export hourly")
			return
		}
		s := sheet{name: "Produksi per jam", header: []string{"Tanggal", "Jam", "Ritase", "Tonase (t)", "Target ritase", "Target tonase", "Pencapaian (%)", "Unit", "Status"}}
		for _, x := range r.Rows {
			s.rows = append(s.rows, []any{x.Date, x.Label, x.Rit, x.Ton, x.TargetRit, x.TargetTon, num(x.Achievement), x.Units, x.Status})
		}
		sheets = append(sheets, s)
	case "availability":
		q2 := dto.AvailabilityQuery{From: q.From, To: q.To, Bucket: "day"}
		r, err := h.production.Availability(ctx, q2)
		if err != nil {
			respondError(c, err, "Failed to export availability")
			return
		}
		header := []string{"Tanggal", "Unit / jenis", "Jenis unit", "Total (jam)", "Working (jam)", "Standby terencana (jam)",
			"Standby tak terencana (jam)", "Breakdown (jam)", "Tanpa status (jam)", "PA (%)", "UA (%)", "MA (%)", "EU (%)"}
		rowsOf := func(list []dto.AvailabilityRow) [][]any {
			out := [][]any{}
			hr := func(ms float64) any { return float64(int(ms/3_600_000*100+0.5)) / 100 }
			for _, x := range list {
				out = append(out, []any{x.Period, x.Label, x.UnitType, hr(x.TotalMs), hr(x.WorkingMs), hr(x.StandbyPlannedMs),
					hr(x.StandbyUnplannedMs), hr(x.BreakdownMs), hr(x.NoStatusMs), num(x.PA), num(x.UA), num(x.MA), num(x.EU)})
			}
			return out
		}
		sheets = append(sheets, sheet{name: "Per unit", header: header, rows: rowsOf(r.Units)},
			sheet{name: "Per jenis unit", header: header, rows: rowsOf(r.Types)},
			sheet{name: "Semua unit", header: header, rows: rowsOf(r.Total)})
	case "operators":
		r, err := h.production.OperatorReport(ctx, q.ReportQuery)
		if err != nil {
			respondError(c, err, "Failed to export operators")
			return
		}
		s := sheet{name: "Rapor operator", header: []string{"Operator", "Skor", "Penugasan", "Ritase", "Cycle time (mnt)", "Dumping (mnt)",
			"Idle (mnt)", "Overspeed", "Mengemudi kasar", "Telat mulai (kali)", "Telat mulai (mnt)", "Berhenti awal (kali)", "Berhenti awal (mnt)"}}
		for _, o := range r.Operators {
			var cycle, dump any = "", ""
			if o.CycleMs != nil {
				cycle = minutes(*o.CycleMs)
			}
			if o.DumpMs != nil {
				dump = minutes(*o.DumpMs)
			}
			s.rows = append(s.rows, []any{o.DriverName, o.Score, o.Shifts, o.Ritase, cycle, dump, minutes(float64(o.IdleMs)), o.Overspeed, o.Harsh,
				o.LateStarts, minutes(float64(o.LateStartMs)), o.EarlyEnds, minutes(float64(o.EarlyEndMs))})
		}
		idle := sheet{name: "Idle per unit", header: []string{"Unit", "Idle (mnt)", "% waktu bertugas", "BBM terbuang (L)", "Sumber BBM"}}
		for _, x := range r.Idle {
			src := "Perkiraan"
			if x.FromCan {
				src = "CAN"
			}
			idle.rows = append(idle.rows, []any{x.Unit, minutes(float64(x.IdleMs)), x.ShareOfJob, x.FuelL, src})
		}
		sheets = append(sheets, s, idle)
	case "breakdowns":
		r, err := h.production.Breakdowns(ctx, q.ReportQuery)
		if err != nil {
			respondError(c, err, "Failed to export breakdowns")
			return
		}
		s := sheet{name: "Log breakdown", header: []string{"Mulai", "Selesai", "Unit", "Jenis", "Operator", "Catatan", "Durasi perbaikan (mnt)"}}
		for _, e := range r.Episodes {
			kinds := ""
			for i, k := range e.Codes {
				if i > 0 {
					kinds += " → "
				}
				kinds += k
			}
			s.rows = append(s.rows, []any{t(e.StartAt), tp(e.EndAt), e.Unit, kinds, str(e.DriverName), str(e.Note), minutes(float64(e.DurationMs))})
		}
		units := sheet{name: "Per unit", header: []string{"Unit", "Kejadian", "Total BD (mnt)", "MTTR (mnt)", "Masih BD"}}
		for _, u := range r.Units {
			var mttr any = ""
			if u.MttrMs != nil {
				mttr = minutes(float64(*u.MttrMs))
			}
			open := "Tidak"
			if u.OpenNow {
				open = "Ya"
			}
			units.rows = append(units.rows, []any{u.Unit, u.Count, minutes(float64(u.TotalMs)), mttr, open})
		}
		sheets = append(sheets, s, units)
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFF4CF"}}})
	for i, s := range sheets {
		if i == 0 {
			_ = f.SetSheetName("Sheet1", s.name)
		} else if _, err := f.NewSheet(s.name); err != nil {
			response.Error(c, http.StatusInternalServerError, "Gagal membuat file Excel", nil)
			return
		}
		sw, err := f.NewStreamWriter(s.name)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "Gagal membuat file Excel", nil)
			return
		}
		head := make([]any, len(s.header))
		for j, x := range s.header {
			head[j] = excelize.Cell{StyleID: bold, Value: x}
		}
		_ = sw.SetColWidth(1, len(s.header), 16)
		_ = sw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
		_ = sw.SetRow("A1", head)
		for r, row := range s.rows {
			cell, _ := excelize.CoordinatesToCellName(1, r+2)
			_ = sw.SetRow(cell, row)
		}
		if err := sw.Flush(); err != nil {
			response.Error(c, http.StatusInternalServerError, "Gagal membuat file Excel", nil)
			return
		}
	}
	name := fmt.Sprintf("fms-%s-%s.xlsx", q.Type, time.Now().In(h.loc).Format("20060102-1504"))
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Status(http.StatusOK)
	if err := f.Write(c.Writer); err != nil {
		_ = c.Error(err)
	}
}
