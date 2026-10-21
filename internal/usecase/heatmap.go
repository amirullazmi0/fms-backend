package usecase

import (
	"context"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/pkg/apperror"
)

// ---------- Heat map kecepatan (P3-2) ----------

const (
	maxHeatmapDays = 31
	maxHeatCells   = 20000
	maxShocks      = 2000
)

// Heatmap: speed per road cell during finished ritase (loaded = hauling, empty = return), outside
// the front loading / dumping radius, plus accelerometer shock points (jalan rusak).
func (u *productionUsecase) Heatmap(ctx context.Context, q dto.HeatmapQuery) (*dto.HeatmapReport, error) {
	f := u.filterOf(dto.ReportQuery{From: q.From, To: q.To})
	if f.To-f.From > maxHeatmapDays*24*hourMs {
		return nil, apperror.BadRequest("Periode heat map maksimal 31 hari")
	}
	leg := map[string]string{"loaded": "go", "empty": "back"}[q.Load]
	cells, truncated, err := u.repo.Heatmap(ctx, f.From, f.To, q.Route, leg, maxHeatCells)
	if err != nil {
		return nil, err
	}
	shocks, err := u.repo.Shocks(ctx, f.From, f.To, q.Route, maxShocks)
	if err != nil {
		return nil, err
	}
	for i := range cells {
		cells[i].AvgSpeed = round1(cells[i].AvgSpeed)
	}
	return &dto.HeatmapReport{From: f.From, To: f.To, CellDeg: repository.HeatCellDeg, Cells: cells, Shocks: shocks, Truncated: truncated}, nil
}
