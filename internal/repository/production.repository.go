package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// ProductionRepository covers production targets and the data behind the hourly, freshness and
// cycle-stage reports.
type ProductionRepository interface {
	GetTargets(ctx context.Context) ([]dto.ProductionTarget, error)
	CreateTarget(ctx context.Context, body dto.UpsertTargetRequest, createdBy string) (string, error)
	UpdateTarget(ctx context.Context, id string, body dto.UpsertTargetRequest, updatedBy string) (bool, error)
	DeleteTarget(ctx context.Context, id, deletedBy string) (bool, error)

	// AssignmentWindows: trucks on duty (started shifts) during [from, to].
	AssignmentWindows(ctx context.Context, from, to int64, routeID, shiftID string) ([]dto.AssignmentWindow, error)
	// Freshness: data delay per truck over the last `withinMs`.
	Freshness(ctx context.Context, withinMs, lateMs int64) (map[string]dto.Freshness, error)
	// Stages: average stage times per route in the period.
	Stages(ctx context.Context, f dto.TripFilter) ([]dto.StageRow, error)
	// StatusIntervals: unit status logs overlapping [from, to), clipped to it (open log = until now).
	StatusIntervals(ctx context.Context, from, to int64) ([]dto.StatusInterval, error)
	// AvailabilityUnits: units (not deleted) for the availability report.
	AvailabilityUnits(ctx context.Context) ([]dto.AvailabilityUnit, error)
	// Heatmap / Shocks: kecepatan per sel jalan dan titik guncangan (P3-2).
	Heatmap(ctx context.Context, from, to int64, routeID, leg string, limit int) ([]dto.HeatCell, bool, error)
	Shocks(ctx context.Context, from, to int64, routeID string, limit int) ([]dto.ShockPoint, error)
	// BreakdownLogs: log status breakdown dalam periode (P3-5).
	BreakdownLogs(ctx context.Context, from, to int64) ([]dto.BreakdownLog, error)
}

type productionRepository struct {
	db *sql.DB
}

func NewProductionRepository(db *sql.DB) ProductionRepository {
	return &productionRepository{db: db}
}

func (r *productionRepository) GetTargets(ctx context.Context) ([]dto.ProductionTarget, error) {
	rows, err := database.NewQueryBuilder("production_targets t").
		Select("t.id", "t.route_id", "COALESCE(r.name, '-')", "t.rit_per_hour", "t.ton_per_hour", "t.rit_per_shift", "t.ton_per_shift",
			"to_char(t.valid_from, 'YYYY-MM-DD')", "t.note", "t.updated_at").
		LeftJoin("routes r", "r.id = t.route_id").
		WhereNull("t.deleted_at").
		OrderBy("r.name").
		OrderBy("t.valid_from DESC").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.ProductionTarget{}
	for rows.Next() {
		var t dto.ProductionTarget
		var rh, th, rs, ts sql.NullFloat64
		var note sql.NullString
		var updated time.Time
		if err := rows.Scan(&t.ID, &t.RouteID, &t.RouteName, &rh, &th, &rs, &ts, &t.ValidFrom, &note, &updated); err != nil {
			return nil, err
		}
		t.RitPerHour, t.TonPerHour, t.RitPerShift, t.TonPerShift = database.NullFloat(rh), database.NullFloat(th), database.NullFloat(rs), database.NullFloat(ts)
		t.Note = database.NullString(note)
		t.UpdatedAt = database.Ms(updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *productionRepository) CreateTarget(ctx context.Context, b dto.UpsertTargetRequest, createdBy string) (string, error) {
	var id string
	err := database.NewInsertBuilder("production_targets").
		Set("route_id", b.RouteID).
		Set("rit_per_hour", b.RitPerHour).
		Set("ton_per_hour", b.TonPerHour).
		Set("rit_per_shift", b.RitPerShift).
		Set("ton_per_shift", b.TonPerShift).
		Set("valid_from", b.ValidFrom).
		Set("note", noteOf(b.Note)).
		SetCreatedBy(createdBy).
		QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *productionRepository) UpdateTarget(ctx context.Context, id string, b dto.UpsertTargetRequest, updatedBy string) (bool, error) {
	return affected(database.NewUpdateBuilder("production_targets").
		Set("route_id", b.RouteID).
		Set("rit_per_hour", b.RitPerHour).
		Set("ton_per_hour", b.TonPerHour).
		Set("rit_per_shift", b.RitPerShift).
		Set("ton_per_shift", b.TonPerShift).
		Set("valid_from", b.ValidFrom).
		Set("note", noteOf(b.Note)).
		SetUpdatedBy(updatedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db))
}

func (r *productionRepository) DeleteTarget(ctx context.Context, id, deletedBy string) (bool, error) {
	return affected(database.NewDeleteBuilder("production_targets").
		SetDeletedBy(deletedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db))
}

func (r *productionRepository) AssignmentWindows(ctx context.Context, from, to int64, routeID, shiftID string) ([]dto.AssignmentWindow, error) {
	qb := database.NewQueryBuilder("assignments a").
		Select("a.imei", "a.route_id", "GREATEST(COALESCE(a.start_at, s.started_at), s.started_at)",
			"CASE WHEN a.end_at IS NULL AND s.closed_at IS NULL THEN NULL ELSE LEAST(COALESCE(a.end_at, s.closed_at), COALESCE(s.closed_at, a.end_at)) END").
		InnerJoin("shifts s", "s.id = a.shift_id AND s.deleted_at IS NULL AND s.started_at IS NOT NULL").
		Where("COALESCE(a.start_at, s.started_at) <= $1", database.FromMs(to)).
		Where("COALESCE(LEAST(a.end_at, s.closed_at), a.end_at, s.closed_at, now()) >= $2", database.FromMs(from))
	n := 2
	if routeID != "" {
		n++
		qb.Where(fmt.Sprintf("a.route_id = $%d", n), routeID)
	}
	if shiftID != "" {
		n++
		qb.Where(fmt.Sprintf("a.shift_id = $%d", n), shiftID)
	}
	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.AssignmentWindow{}
	for rows.Next() {
		var w dto.AssignmentWindow
		var start time.Time
		var end sql.NullTime
		if err := rows.Scan(&w.Imei, &w.RouteID, &start, &end); err != nil {
			return nil, err
		}
		w.Start, w.End = database.Ms(start), database.NullMs(end)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *productionRepository) Freshness(ctx context.Context, withinMs, lateMs int64) (map[string]dto.Freshness, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT imei,
			COALESCE(AVG(EXTRACT(EPOCH FROM received_at - time) * 1000), 0)::float,
			COALESCE(MAX(EXTRACT(EPOCH FROM received_at - time) * 1000), 0)::float,
			COUNT(*) FILTER (WHERE received_at - time > make_interval(secs => $2))::int,
			COUNT(*)::int
		FROM records WHERE received_at > now() - make_interval(secs => $1)
		GROUP BY imei`, float64(withinMs)/1000, float64(lateMs)/1000)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]dto.Freshness{}
	for rows.Next() {
		var f dto.Freshness
		if err := rows.Scan(&f.Imei, &f.AvgDelayMs, &f.MaxDelayMs, &f.LateRecords, &f.Records); err != nil {
			return nil, err
		}
		out[f.Imei] = f
	}
	return out, rows.Err()
}

// stageFilter: the trip filter on a table aliased `alias` with time column `timeCol`.
func stageFilter(qb *database.QueryBuilder, f dto.TripFilter, alias, timeCol string) {
	n := 0
	next := func() int { n++; return n }
	qb.Where(fmt.Sprintf("%s.%s >= $%d", alias, timeCol, next()), database.FromMs(f.From))
	qb.Where(fmt.Sprintf("%s.%s <= $%d", alias, timeCol, next()), database.FromMs(f.To))
	for col, v := range map[string]string{"route_id": f.RouteID, "driver_id": f.DriverID, "imei": f.Imei, "shift_id": f.ShiftID} {
		if v != "" {
			qb.Where(fmt.Sprintf("%s.%s = $%d", alias, col, next()), v)
		}
	}
}

func (r *productionRepository) Stages(ctx context.Context, f dto.TripFilter) ([]dto.StageRow, error) {
	by := map[string]*dto.StageRow{}
	order := []string{}
	row := func(id, name string) *dto.StageRow {
		x, ok := by[id]
		if !ok {
			x = &dto.StageRow{RouteID: id, RouteName: name}
			by[id] = x
			order = append(order, id)
		}
		if x.RouteName == "" {
			x.RouteName = name
		}
		return x
	}

	vq := database.NewQueryBuilder("site_visits v").
		Select("v.route_id::text", "COALESCE(MAX(r.name), '')", "v.role::text", "COUNT(*)::int",
			"COALESCE(AVG(v.queue_ms), 0)::float", "AVG(v.spot_ms)::float", "COALESCE(AVG(COALESCE(v.work_ms, v.dwell_ms)), 0)::float",
			"COALESCE(AVG(v.dwell_ms), 0)::float").
		LeftJoin("routes r", "r.id = v.route_id").
		Where("v.status = 'done'").
		Where("v.route_id IS NOT NULL").
		GroupBy("v.route_id", "v.role")
	stageFilter(vq, f, "v", "arrive_at")
	rows, err := vq.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name, role string
		var s dto.StageStats
		var spot sql.NullFloat64
		if err := rows.Scan(&id, &name, &role, &s.Visits, &s.AvgQueueMs, &spot, &s.AvgWorkMs, &s.AvgDwellMs); err != nil {
			_ = rows.Close()
			return nil, err
		}
		s.AvgSpotMs = database.NullFloat(spot)
		x := row(id, name)
		if role == "loading" {
			x.Loading = s
		} else {
			x.Unloading = s
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	tq := database.NewQueryBuilder("trips t").
		Select("t.route_id::text", "MAX(t.route_name)", "t.leg::text", "COUNT(*)::int", "COALESCE(AVG(t.duration_ms), 0)::float").
		Where("t.status = 'done'").
		Where("t.route_id IS NOT NULL").
		GroupBy("t.route_id", "t.leg")
	stageFilter(tq, f, "t", "depart_at")
	trows, err := tq.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = trows.Close() }()
	for trows.Next() {
		var id, name, leg string
		var n int
		var avg float64
		if err := trows.Scan(&id, &name, &leg, &n, &avg); err != nil {
			return nil, err
		}
		x := row(id, name)
		if leg == "go" {
			x.Hauling, x.HaulingN = avg, n
		} else {
			x.Returning, x.ReturningN = avg, n
		}
	}
	if err := trows.Err(); err != nil {
		return nil, err
	}

	out := make([]dto.StageRow, 0, len(order))
	for _, id := range order {
		x := by[id]
		// A trip runs from leaving one site to arriving at the other, so the cycle adds both stays.
		site := func(s dto.StageStats) float64 {
			v := s.AvgQueueMs + s.AvgWorkMs
			if s.AvgSpotMs != nil {
				v += *s.AvgSpotMs
			}
			return v
		}
		x.CycleMs = site(x.Loading) + x.Hauling + site(x.Unloading) + x.Returning
		if x.CycleMs > 0 {
			x.RitPerHour = float64(int(60/(x.CycleMs/60_000)*100+0.5)) / 100
		}
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].RouteName) < strings.ToLower(out[j].RouteName) })
	return out, nil
}

func (r *productionRepository) StatusIntervals(ctx context.Context, from, to int64) ([]dto.StatusInterval, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT l.imei,
			COALESCE(dc.category::text, CASE l.status WHEN 'working' THEN 'working' WHEN 'breakdown' THEN 'breakdown' ELSE 'standby_unplanned' END),
			(EXTRACT(EPOCH FROM GREATEST(l.start_at, $1)) * 1000)::bigint,
			(EXTRACT(EPOCH FROM LEAST(COALESCE(l.end_at, now()), $2)) * 1000)::bigint
		FROM unit_status_logs l LEFT JOIN delay_codes dc ON dc.code = l.delay_code
		WHERE l.start_at < $2 AND COALESCE(l.end_at, now()) > $1
		ORDER BY l.imei, l.start_at`, database.FromMs(from), database.FromMs(to))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.StatusInterval{}
	for rows.Next() {
		var s dto.StatusInterval
		if err := rows.Scan(&s.Imei, &s.Category, &s.Start, &s.End); err != nil {
			return nil, err
		}
		if s.End > s.Start {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

func (r *productionRepository) AvailabilityUnits(ctx context.Context) ([]dto.AvailabilityUnit, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT imei, COALESCE(NULLIF(plate, ''), NULLIF(name, ''), imei), unit_type::text
		FROM devices WHERE deleted_at IS NULL ORDER BY 2`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.AvailabilityUnit{}
	for rows.Next() {
		var u dto.AvailabilityUnit
		if err := rows.Scan(&u.Imei, &u.Label, &u.UnitType); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
