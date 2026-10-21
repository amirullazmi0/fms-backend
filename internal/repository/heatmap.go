package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// HeatCellDeg: grid size of the speed heat map (±22 m).
const HeatCellDeg = 0.0002

// insideSite: the record is within the site radius (equirectangular, fine for a few hundred metres).
func insideSite(site string) string {
	return fmt.Sprintf(`sqrt(power((r.lat - %[1]s.lat) * 111320, 2) + power((r.lng - %[1]s.lng) * 111320 * cos(radians(%[1]s.lat)), 2)) <= %[1]s.radius_m`, site)
}

// Heatmap: speeds of moving units during finished ritase, per grid cell, outside both sites of
// the trip. leg "" = both, "go" = loaded (hauling), "back" = empty (return).
func (r *productionRepository) Heatmap(ctx context.Context, from, to int64, routeID, leg string, limit int) ([]dto.HeatCell, bool, error) {
	args := []any{database.FromMs(from), database.FromMs(to), HeatCellDeg}
	where := ""
	if routeID != "" {
		args = append(args, routeID)
		where += fmt.Sprintf(" AND t.route_id = $%d", len(args))
	}
	if leg != "" {
		args = append(args, leg)
		where += fmt.Sprintf(" AND t.leg::text = $%d", len(args))
	}
	args = append(args, limit+1)
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT round(r.lat / $3) * $3, round(r.lng / $3) * $3, COUNT(*)::int,
			AVG(r.speed)::float, MAX(r.speed)::int
		FROM trips t
		JOIN sites l ON l.id = t.loading_site_id
		JOIN sites u ON u.id = t.unloading_site_id
		JOIN records r ON r.imei = t.imei AND r.time >= t.depart_at AND r.time <= t.arrive_at
		WHERE t.status = 'done' AND t.depart_at >= $1 AND t.depart_at < $2`+where+`
			AND r.valid AND r.speed > 2
			AND NOT (`+insideSite("l")+`) AND NOT (`+insideSite("u")+`)
		GROUP BY 1, 2
		ORDER BY 3 DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.HeatCell{}
	for rows.Next() {
		var c dto.HeatCell
		if err := rows.Scan(&c.Lat, &c.Lng, &c.Points, &c.AvgSpeed, &c.MaxSpeed); err != nil {
			return nil, false, err
		}
		out = append(out, c)
	}
	truncated := len(out) > limit
	if truncated {
		out = out[:limit]
	}
	return out, truncated, rows.Err()
}

// Shocks: accelerometer events (green driving IO 253 = 1 accel, 2 brake, 3 corner; crash IO 247)
// with their position, optionally only for units on the route in the period.
func (r *productionRepository) Shocks(ctx context.Context, from, to int64, routeID string, limit int) ([]dto.ShockPoint, error) {
	args := []any{database.FromMs(from), database.FromMs(to), limit}
	where := ""
	if routeID != "" {
		args = append(args, routeID)
		where = fmt.Sprintf(` AND EXISTS (SELECT 1 FROM assignments a JOIN shifts s ON s.id = a.shift_id
			WHERE a.imei = r.imei AND a.route_id = $%d AND r.time >= COALESCE(a.start_at, s.started_at)
			  AND r.time < COALESCE(LEAST(a.end_at, s.closed_at), a.end_at, s.closed_at, 'infinity'::timestamptz))`, len(args))
	}
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT r.lat, r.lng, r.time, r.imei, r.event_io,
			COALESCE(r.io->>'253', ''), (r.io->>'254')::float
		FROM records r
		WHERE r.time >= $1 AND r.time < $2 AND r.valid AND r.event_io IN (253, 247)`+where+`
		ORDER BY r.time DESC LIMIT $3`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.ShockPoint{}
	for rows.Next() {
		var p dto.ShockPoint
		var at time.Time
		var eventIo int
		var typ string
		var g sql.NullFloat64
		if err := rows.Scan(&p.Lat, &p.Lng, &at, &p.Imei, &eventIo, &typ, &g); err != nil {
			return nil, err
		}
		p.Time = database.Ms(at)
		p.Kind = map[string]string{"1": "accel", "2": "brake", "3": "corner"}[typ]
		if eventIo == 247 {
			p.Kind = "crash"
		}
		if p.Kind == "" {
			continue
		}
		if g.Valid {
			v := g.Float64 * 0.01 // IO 254: G-force × 100
			p.G = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
