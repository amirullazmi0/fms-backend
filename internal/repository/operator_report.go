package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// AssignmentStats: per assignment overlapping [from, to): idle time (ignition on, speed ≤ 2,
// gaps capped at 5 min), first / last movement, overspeed & green-driving events, ritase, cycle
// time and dumping time (P3-6 / P3-7).
func (r *productionRepository) AssignmentStats(ctx context.Context, from, to int64) ([]dto.AssignmentStats, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `WITH win AS (
			SELECT a.id, a.imei, a.driver_id, u.name AS driver_name, s.planned_start, s.planned_end,
				GREATEST(COALESCE(a.start_at, s.started_at), $1) AS f,
				LEAST(COALESCE(LEAST(a.end_at, s.closed_at), a.end_at, s.closed_at, now()), $2) AS t
			FROM assignments a JOIN shifts s ON s.id = a.shift_id AND s.deleted_at IS NULL
			JOIN users u ON u.id = a.driver_id
			WHERE s.started_at IS NOT NULL
		), rec AS (
			SELECT w.id AS aid, r.time, r.speed, COALESCE((r.io->>'239')::float, 0) AS ign, (r.io->>'110')::float AS rate, r.event_io,
				LEAD(r.time) OVER (PARTITION BY w.id ORDER BY r.time) AS nxt
			FROM win w JOIN records r ON r.imei = w.imei AND r.time >= w.f AND r.time < w.t
			WHERE w.t > w.f
		), agg AS (
			SELECT aid,
				COALESCE(SUM(LEAST(EXTRACT(EPOCH FROM nxt - time), 300)) FILTER (WHERE ign = 1 AND speed <= 2 AND nxt IS NOT NULL), 0)::float AS idle_s,
				AVG(rate * 0.1) FILTER (WHERE ign = 1 AND speed <= 2 AND rate > 0)::float AS idle_rate,
				MIN(time) FILTER (WHERE speed > 5) AS first_move,
				MAX(time) FILTER (WHERE speed > 5) AS last_move,
				COUNT(*) FILTER (WHERE event_io = 255)::int AS overspeed,
				COUNT(*) FILTER (WHERE event_io = 253)::int AS harsh
			FROM rec GROUP BY aid
		)
		SELECT w.id, w.driver_id, w.driver_name, w.imei, w.planned_start, w.planned_end, w.f, w.t,
			COALESCE(g.idle_s, 0), g.idle_rate, g.first_move, g.last_move, COALESCE(g.overspeed, 0), COALESCE(g.harsh, 0),
			(SELECT COUNT(*) FROM trips t WHERE t.assignment_id = w.id AND t.leg = 'go' AND t.status = 'done')::int,
			-- cycle time = hauling + return + lama di front + lama di dumping (rata-rata penugasan ini)
			(SELECT AVG(t.duration_ms) FILTER (WHERE t.leg = 'go') + AVG(t.duration_ms) FILTER (WHERE t.leg = 'back')
				FROM trips t WHERE t.assignment_id = w.id AND t.status = 'done')::float
				+ COALESCE((SELECT AVG(v.dwell_ms) FROM site_visits v WHERE v.assignment_id = w.id AND v.role = 'loading' AND v.status = 'done'), 0)
				+ COALESCE((SELECT AVG(v.dwell_ms) FROM site_visits v WHERE v.assignment_id = w.id AND v.role = 'unloading' AND v.status = 'done'), 0),
			(SELECT AVG(v.dwell_ms) FROM site_visits v WHERE v.assignment_id = w.id AND v.role = 'unloading' AND v.status = 'done')::float
		FROM win w LEFT JOIN agg g ON g.aid = w.id
		WHERE w.t > w.f
		ORDER BY w.driver_name, w.f`, database.FromMs(from), database.FromMs(to))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.AssignmentStats{}
	for rows.Next() {
		var s dto.AssignmentStats
		var ps, pe, f, t time.Time
		var rate, cycle, dump sql.NullFloat64
		var first, last sql.NullTime
		if err := rows.Scan(&s.AssignmentID, &s.DriverID, &s.DriverName, &s.Imei, &ps, &pe, &f, &t, &s.IdleS, &rate,
			&first, &last, &s.Overspeed, &s.Harsh, &s.Ritase, &cycle, &dump); err != nil {
			return nil, err
		}
		s.PlannedStart, s.PlannedEnd, s.WindowStart, s.WindowEnd = database.Ms(ps), database.Ms(pe), database.Ms(f), database.Ms(t)
		s.IdleRateLph, s.CycleMs, s.DumpMs = database.NullFloat(rate), database.NullFloat(cycle), database.NullFloat(dump)
		s.FirstMove, s.LastMove = database.NullMs(first), database.NullMs(last)
		out = append(out, s)
	}
	return out, rows.Err()
}
