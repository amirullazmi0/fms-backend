package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/lib/pq"
)

// VisitRepository stores the stays at loading/unloading sites and aggregates them.
type VisitRepository interface {
	// ReplaceVisits stores the stays of one assignment (drops the ones no longer found).
	ReplaceVisits(ctx context.Context, assignmentID string, visits []dto.SiteVisitUpsert) (int, error)
	Report(ctx context.Context, f dto.TripFilter, now int64) (*dto.VisitReport, error)
	// ListVisits: raw stays in the period, newest first. role = "" | loading | unloading.
	ListVisits(ctx context.Context, f dto.TripFilter, role string, limit, offset int, now int64) ([]dto.SiteVisitRecord, int, error)
}

type visitRepository struct {
	db *sql.DB
}

func NewVisitRepository(db *sql.DB) VisitRepository {
	return &visitRepository{db: db}
}

func (r *visitRepository) ReplaceVisits(ctx context.Context, assignmentID string, visits []dto.SiteVisitUpsert) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	keep := make([]string, len(visits))
	for i, v := range visits {
		keep[i] = fmt.Sprintf("%s|%d", v.SiteID, v.ArriveAt)
	}
	res, err := database.NewDeleteBuilder("site_visits").HardDelete().
		Where("assignment_id = $1", assignmentID).
		Where("NOT ((site_id::text || '|' || (EXTRACT(EPOCH FROM arrive_at) * 1000)::bigint::text) = ANY($1::text[]))", pq.Array(keep)).
		ExecuteContext(ctx, tx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	changed := int(n)

	const upsert = ` ON CONFLICT (imei, site_id, arrive_at) DO UPDATE SET
		assignment_id = EXCLUDED.assignment_id, shift_id = EXCLUDED.shift_id, driver_id = EXCLUDED.driver_id,
		route_id = EXCLUDED.route_id, site_name = EXCLUDED.site_name, role = EXCLUDED.role,
		depart_at = EXCLUDED.depart_at, dwell_ms = EXCLUDED.dwell_ms, status = EXCLUDED.status, computed_at = CURRENT_TIMESTAMP
		WHERE site_visits.depart_at IS DISTINCT FROM EXCLUDED.depart_at OR site_visits.dwell_ms IS DISTINCT FROM EXCLUDED.dwell_ms
		   OR site_visits.assignment_id IS DISTINCT FROM EXCLUDED.assignment_id OR site_visits.status IS DISTINCT FROM EXCLUDED.status`
	for _, v := range visits {
		status := "done"
		if v.Finished {
			status = "finished"
		} else if v.DepartAt == nil {
			status = "inside"
		}
		query, args := database.NewInsertBuilder("site_visits").
			Set("assignment_id", v.AssignmentID).
			Set("shift_id", v.ShiftID).
			Set("driver_id", v.DriverID).
			Set("imei", v.Imei).
			Set("route_id", v.RouteID).
			Set("site_id", v.SiteID).
			Set("site_name", v.SiteName).
			Set("role", v.Role).
			Set("arrive_at", database.FromMs(v.ArriveAt)).
			Set("depart_at", database.FromMsPtr(v.DepartAt)).
			Set("dwell_ms", v.DwellMs).
			Set("status", status).
			WithoutReturning().
			Build()
		res, err := database.RawExecContext(ctx, tx, query+upsert, args...)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		changed += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

// visitWhere: stays overlapping [from, to] matching the filter. Ongoing stays count up to now.
func visitWhere(f dto.TripFilter) (string, []any) {
	conds := []string{"v.arrive_at <= $1", "COALESCE(v.depart_at, now()) >= $2"}
	args := []any{database.FromMs(f.To), database.FromMs(f.From)}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if f.RouteID != "" {
		add("v.route_id = $%d", f.RouteID)
	}
	if f.DriverID != "" {
		add("v.driver_id = $%d", f.DriverID)
	}
	if f.Imei != "" {
		add("v.imei = $%d", f.Imei)
	}
	if f.ShiftID != "" {
		add("v.shift_id = $%d", f.ShiftID)
	}
	return strings.Join(conds, " AND "), args
}

func (r *visitRepository) Report(ctx context.Context, f dto.TripFilter, now int64) (*dto.VisitReport, error) {
	where, args := visitWhere(f)
	out := &dto.VisitReport{From: f.From, To: f.To, Sites: []dto.SiteDwellRow{}, Current: []dto.SiteStay{}}

	// Ongoing stays: dwell grows until now.
	dwell := "CASE WHEN v.status = 'inside' THEN GREATEST(v.dwell_ms, (EXTRACT(EPOCH FROM now() - v.arrive_at) * 1000)::bigint) ELSE v.dwell_ms END"
	rows, err := database.RawQueryContext(ctx, r.db, `
		SELECT v.site_id, v.site_name, v.role::text,
		       COUNT(*) FILTER (WHERE v.status = 'done'),
		       COALESCE(AVG(v.dwell_ms) FILTER (WHERE v.status = 'done'), 0),
		       COALESCE(MIN(v.dwell_ms) FILTER (WHERE v.status = 'done'), 0),
		       COALESCE(MAX(`+dwell+`) FILTER (WHERE v.status <> 'finished'), 0),
		       COALESCE(SUM(`+dwell+`) FILTER (WHERE v.status <> 'finished'), 0),
		       COUNT(*) FILTER (WHERE v.status = 'inside')
		FROM site_visits v WHERE `+where+`
		GROUP BY v.site_id, v.site_name, v.role ORDER BY v.role, v.site_name`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	totals := map[string]*dto.DwellStats{"loading": &out.Loading, "unloading": &out.Unloading}
	sums := map[string]float64{}
	for rows.Next() {
		var row dto.SiteDwellRow
		var minMs, total int64
		if err := rows.Scan(&row.SiteID, &row.SiteName, &row.Role, &row.Visits, &row.AvgDwellMs, &minMs, &row.MaxDwellMs, &total, &row.Inside); err != nil {
			return nil, err
		}
		out.Sites = append(out.Sites, row)
		t := totals[row.Role]
		if t == nil {
			continue
		}
		if row.Visits > 0 && (t.Visits == 0 || minMs < t.MinDwellMs) {
			t.MinDwellMs = minMs
		}
		t.MaxDwellMs = max(t.MaxDwellMs, row.MaxDwellMs)
		t.TotalMs += total
		t.Inside += row.Inside
		t.Visits += row.Visits
		sums[row.Role] += row.AvgDwellMs * float64(row.Visits)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for role, t := range totals {
		if t.Visits > 0 {
			t.AvgDwellMs = sums[role] / float64(t.Visits)
		}
	}

	// Trucks inside a site right now (not limited to the period).
	cur, err := database.RawQueryContext(ctx, r.db, `
		SELECT v.imei, COALESCE(NULLIF(d.plate, ''), NULLIF(d.name, ''), v.imei), dr.name, ro.name, v.site_name, v.role::text, v.arrive_at
		FROM site_visits v
		LEFT JOIN devices d ON d.imei = v.imei
		LEFT JOIN drivers dr ON dr.id = v.driver_id
		LEFT JOIN routes ro ON ro.id = v.route_id
		WHERE v.status = 'inside' AND v.arrive_at > now() - interval '1 day'
		ORDER BY v.arrive_at`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close() }()
	for cur.Next() {
		var s dto.SiteStay
		var driver, route sql.NullString
		var arrive time.Time
		if err := cur.Scan(&s.Imei, &s.Truck, &driver, &route, &s.SiteName, &s.Role, &arrive); err != nil {
			return nil, err
		}
		s.DriverName, s.RouteName = database.NullString(driver), database.NullString(route)
		s.ArriveAt = database.Ms(arrive)
		s.DwellMs = max(0, now-s.ArriveAt)
		out.Current = append(out.Current, s)
	}
	return out, cur.Err()
}

func (r *visitRepository) ListVisits(ctx context.Context, f dto.TripFilter, role string, limit, offset int, now int64) ([]dto.SiteVisitRecord, int, error) {
	where, args := visitWhere(f)
	if role == "loading" || role == "unloading" {
		args = append(args, role)
		where += fmt.Sprintf(" AND v.role = $%d::site_visit_role", len(args))
	}
	var total int
	if err := database.RawQueryRowContext(ctx, r.db, "SELECT COUNT(*) FROM site_visits v WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := database.RawQueryContext(ctx, r.db, fmt.Sprintf(`
		SELECT v.id, v.imei, COALESCE(NULLIF(d.plate, ''), NULLIF(d.name, ''), v.imei), dr.name, sh.name, ro.name,
		       v.site_name, v.role::text, v.status::text, v.arrive_at, v.depart_at, v.dwell_ms
		FROM site_visits v
		LEFT JOIN devices d ON d.imei = v.imei
		LEFT JOIN drivers dr ON dr.id = v.driver_id
		LEFT JOIN shifts sh ON sh.id = v.shift_id
		LEFT JOIN routes ro ON ro.id = v.route_id
		WHERE %s ORDER BY v.arrive_at DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.SiteVisitRecord{}
	for rows.Next() {
		var v dto.SiteVisitRecord
		var driver, shift, route sql.NullString
		var arrive time.Time
		var depart sql.NullTime
		if err := rows.Scan(&v.ID, &v.Imei, &v.Truck, &driver, &shift, &route, &v.SiteName, &v.Role, &v.Status, &arrive, &depart, &v.DwellMs); err != nil {
			return nil, 0, err
		}
		v.DriverName, v.ShiftName, v.RouteName = database.NullString(driver), database.NullString(shift), database.NullString(route)
		v.ArriveAt, v.DepartAt = database.Ms(arrive), database.NullMs(depart)
		if v.Status == "inside" {
			v.DwellMs = max(v.DwellMs, now-v.ArriveAt)
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
