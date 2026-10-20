package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/lib/pq"
)

// FinishingTrip: the assignment's last trip left loading before the shift closed and has not
// reached unloading yet. The truck finishes that ritase (max 6 h, the scheduler's late-arrival
// window), then it is done. Shared with the simulator (pkg/simulator).
const FinishingTrip = `EXISTS (SELECT 1 FROM trips t WHERE t.assignment_id = a.id AND t.leg = 'go' AND t.arrive_at IS NULL
	AND t.depart_at <= sh.closed_at AND t.depart_at = (SELECT max(t2.depart_at) FROM trips t2 WHERE t2.assignment_id = a.id))`

// onDuty = what trucks are working on: an active shift (phase active), or a closed shift whose
// last ritase is still heading to unloading (phase finishing).
const onDuty = `(SELECT a.id, a.imei, a.driver_id, a.route_id, sh.id AS shift_id, sh.name AS shift_name, 'active' AS phase, NULL::timestamptz AS closed_at
		FROM assignments a JOIN shifts sh ON sh.id = a.shift_id AND sh.status = 'active' AND sh.deleted_at IS NULL
		WHERE a.end_at IS NULL
	UNION ALL
	SELECT a.id, a.imei, a.driver_id, a.route_id, sh.id, sh.name, 'finishing', sh.closed_at
		FROM assignments a JOIN shifts sh ON sh.id = a.shift_id AND sh.status = 'closed' AND sh.deleted_at IS NULL
			AND sh.closed_at > now() - interval '6 hours'
		WHERE a.end_at IS NULL AND ` + FinishingTrip + `) a`

// recentDone = assignments of shifts closed in the last 12 h whose truck is done (not finishing).
const recentDone = `(SELECT a.id, a.imei, a.driver_id, a.route_id, sh.id AS shift_id, sh.name AS shift_name, 'done' AS phase, sh.closed_at
		FROM assignments a JOIN shifts sh ON sh.id = a.shift_id AND sh.status = 'closed' AND sh.deleted_at IS NULL
			AND sh.closed_at > now() - interval '12 hours'
		WHERE a.end_at IS NULL AND NOT ` + FinishingTrip + `) a`

// SiteRepository covers sites (geofence points) and routes (loading -> unloading).
type SiteRepository interface {
	GetSites(ctx context.Context, q *dto.QueryGlobal) ([]dto.Site, int, error)
	// SitesByIDs includes soft-deleted sites when withDeleted (old routes still reference them).
	SitesByIDs(ctx context.Context, ids []string, withDeleted bool) ([]dto.Site, error)
	CreateSite(ctx context.Context, body dto.UpsertSiteRequest, createdBy string) (string, error)
	UpdateSite(ctx context.Context, id string, body dto.UpsertSiteRequest, updatedBy string) (bool, error)
	DeleteSite(ctx context.Context, id, deletedBy string) (bool, error)
	// ReplaceZones swaps every queue/spot zone of a site and sets its queue alert thresholds.
	ReplaceZones(ctx context.Context, id string, body dto.UpdateSiteZonesRequest, updatedBy string) (bool, error)
	RoutesUsingSite(ctx context.Context, id string) ([]string, error)

	GetRoutes(ctx context.Context, q *dto.QueryGlobal) ([]dto.Route, int, error)
	GetRouteByID(ctx context.Context, id string) (*dto.Route, error)
	CreateRoute(ctx context.Context, body dto.UpsertRouteRequest, createdBy string) (string, error)
	UpdateRoute(ctx context.Context, id string, body dto.UpsertRouteRequest, updatedBy string) (bool, error)
	DeleteRoute(ctx context.Context, id, deletedBy string) (bool, error)
	RouteInUse(ctx context.Context, id string) (bool, error)
	// RoutesForDevice: routes this truck ran through shift assignments in [from, to].
	RoutesForDevice(ctx context.Context, imei string, from, to int64) ([]dto.Route, error)
	// TrucksOnRoute: trucks ever assigned to this route in [from, to].
	TrucksOnRoute(ctx context.Context, routeID string, from, to int64) ([]string, error)
}

type siteRepository struct {
	db *sql.DB
}

func NewSiteRepository(db *sql.DB) SiteRepository {
	return &siteRepository{db: db}
}

// siteColumns: imeis of a site = trucks on duty on a route passing through it.
var siteColumns = []string{
	"s.id", "s.name", "s.lat", "s.lng", "s.radius_m", "s.note", "s.created_at", "s.updated_at",
	`(SELECT array_agg(DISTINCT a.imei) FROM ` + onDuty + ` JOIN routes r ON r.id = a.route_id
		WHERE r.loading_site_id = s.id OR r.unloading_site_id = s.id) AS imeis`,
	"s.queue_alert_trucks", "s.queue_alert_min",
	`(SELECT COALESCE(json_agg(json_build_object('id', z.id, 'kind', z.kind, 'lat', z.lat, 'lng', z.lng, 'radius', z.radius_m)
		ORDER BY z.kind, z.created_at), '[]') FROM site_zones z WHERE z.site_id = s.id AND z.deleted_at IS NULL) AS zones`,
}

func scanSites(rows *sql.Rows) ([]dto.Site, error) {
	defer func() { _ = rows.Close() }()
	out := []dto.Site{}
	for rows.Next() {
		var s dto.Site
		var note sql.NullString
		var created, updated time.Time
		var imeis pq.StringArray
		var zones []byte
		if err := rows.Scan(&s.ID, &s.Name, &s.Lat, &s.Lng, &s.Radius, &note, &created, &updated, &imeis,
			&s.QueueAlertTrucks, &s.QueueAlertMin, &zones); err != nil {
			return nil, err
		}
		s.Zones = []dto.SiteZone{}
		if err := json.Unmarshal(zones, &s.Zones); err != nil {
			return nil, err
		}
		s.Note = database.NullString(note)
		s.CreatedAt, s.UpdatedAt = database.Ms(created), database.Ms(updated)
		s.Imeis = []string(imeis)
		if s.Imeis == nil {
			s.Imeis = []string{}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// count runs the builder's COUNT(*) form.
func count(ctx context.Context, db database.TX, qb *database.QueryBuilder) (int, error) {
	query, args := qb.BuildCount()
	var n int
	err := database.RawQueryRowContext(ctx, db, query, args...).Scan(&n)
	return n, err
}

// page applies q's sort (whitelisted) and optional pagination.
func page(qb *database.QueryBuilder, q *dto.QueryGlobal, allowed map[string]string, defaultSort, defaultOrder string) {
	sortBy, order := database.GetSorting(q.SortBy, q.Order, allowed, defaultSort)
	if q.Order == "" {
		order = defaultOrder
	}
	qb.OrderBy(sortBy + " " + order)
	if q.Paginated() {
		limit, offset := database.GetPagination(q.Page, q.Limit)
		qb.Limit(limit).Offset(offset)
	}
}

func (r *siteRepository) GetSites(ctx context.Context, q *dto.QueryGlobal) ([]dto.Site, int, error) {
	qb := database.NewQueryBuilder("sites s").Select(siteColumns...).WhereNull("s.deleted_at")

	n := 0
	next := func() int { n++; return n }

	if q.Search != "" {
		p := next()
		qb.Where(fmt.Sprintf("(s.name ILIKE $%d OR s.note ILIKE $%d)", p, p), "%"+q.Search+"%")
	}
	total, err := count(ctx, r.db, qb)
	if err != nil {
		return nil, 0, err
	}
	page(qb, q, map[string]string{
		"name": "s.name", "radius": "s.radius_m", "created_at": "s.created_at", "updated_at": "s.updated_at",
	}, "s.name", "ASC")

	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	sites, err := scanSites(rows)
	return sites, total, err
}

func (r *siteRepository) SitesByIDs(ctx context.Context, ids []string, withDeleted bool) ([]dto.Site, error) {
	if len(ids) == 0 {
		return []dto.Site{}, nil
	}
	qb := database.NewQueryBuilder("sites s").Select(siteColumns...).
		Where("s.id = ANY($1::uuid[])", pq.Array(ids)).
		OrderBy("s.name")
	if !withDeleted {
		qb.WhereNull("s.deleted_at")
	}
	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	return scanSites(rows)
}

func radiusOf(body dto.UpsertSiteRequest) int {
	if body.Radius == nil {
		return 10
	}
	return *body.Radius
}

func (r *siteRepository) CreateSite(ctx context.Context, body dto.UpsertSiteRequest, createdBy string) (string, error) {
	var id string
	err := database.NewInsertBuilder("sites").
		Set("name", strings.TrimSpace(body.Name)).
		Set("lat", *body.Lat).
		Set("lng", *body.Lng).
		Set("radius_m", radiusOf(body)).
		Set("note", noteOf(body.Note)).
		SetCreatedBy(createdBy).
		QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *siteRepository) UpdateSite(ctx context.Context, id string, body dto.UpsertSiteRequest, updatedBy string) (bool, error) {
	res, err := database.NewUpdateBuilder("sites").
		Set("name", strings.TrimSpace(body.Name)).
		Set("lat", *body.Lat).
		Set("lng", *body.Lng).
		Set("radius_m", radiusOf(body)).
		Set("note", noteOf(body.Note)).
		SetUpdatedBy(updatedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *siteRepository) ReplaceZones(ctx context.Context, id string, body dto.UpdateSiteZonesRequest, updatedBy string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	ok, err := affected(database.NewUpdateBuilder("sites").
		Set("queue_alert_trucks", body.QueueAlertTrucks).
		Set("queue_alert_min", body.QueueAlertMin).
		SetUpdatedBy(updatedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, tx))
	if err != nil || !ok {
		return ok, err
	}
	if _, err := database.NewDeleteBuilder("site_zones").HardDelete().Where("site_id = $1", id).ExecuteContext(ctx, tx); err != nil {
		return false, err
	}
	for _, z := range body.Zones {
		radius := z.Radius
		if radius <= 0 {
			radius = 20
		}
		if _, err := database.NewInsertBuilder("site_zones").
			Set("site_id", id).
			Set("kind", z.Kind).
			Set("lat", *z.Lat).
			Set("lng", *z.Lng).
			Set("radius_m", radius).
			SetCreatedBy(updatedBy).
			WithoutReturning().
			ExecContext(ctx, tx); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func (r *siteRepository) DeleteSite(ctx context.Context, id, deletedBy string) (bool, error) {
	res, err := database.NewDeleteBuilder("sites").
		SetDeletedBy(deletedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *siteRepository) RoutesUsingSite(ctx context.Context, id string) ([]string, error) {
	rows, err := database.NewQueryBuilder("routes").
		Select("name").
		Where("(loading_site_id = $1 OR unloading_site_id = $1)", id).
		WhereNull("deleted_at").
		OrderBy("name").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

// ---------- Routes ----------

var routeColumns = []string{
	"r.id", "r.name", "r.loading_site_id", "r.unloading_site_id", "r.note", "r.created_at", "r.updated_at",
	`(SELECT json_agg(json_build_object('shiftId', a.shift_id, 'shiftName', a.shift_name, 'driverId', d.id, 'driverName', d.name, 'imei', a.imei,
			'phase', a.phase, 'closedAt', (EXTRACT(EPOCH FROM a.closed_at) * 1000)::bigint) ORDER BY d.name)
		FROM ` + onDuty + ` JOIN users d ON d.id = a.driver_id WHERE a.route_id = r.id) AS on_duty`,
	`(SELECT json_agg(json_build_object('shiftId', a.shift_id, 'shiftName', a.shift_name, 'driverId', d.id, 'driverName', d.name, 'imei', a.imei,
			'phase', a.phase, 'closedAt', (EXTRACT(EPOCH FROM a.closed_at) * 1000)::bigint) ORDER BY a.closed_at DESC)
		FROM ` + recentDone + ` JOIN users d ON d.id = a.driver_id WHERE a.route_id = r.id) AS recent_done`,
}

func scanRoutes(rows *sql.Rows) ([]dto.Route, error) {
	defer func() { _ = rows.Close() }()
	out := []dto.Route{}
	for rows.Next() {
		var x dto.Route
		var note sql.NullString
		var created, updated time.Time
		var duty, done []byte
		if err := rows.Scan(&x.ID, &x.Name, &x.LoadingID, &x.UnloadingID, &note, &created, &updated, &duty, &done); err != nil {
			return nil, err
		}
		x.Note = database.NullString(note)
		x.CreatedAt, x.UpdatedAt = database.Ms(created), database.Ms(updated)
		x.OnDuty = []dto.OnDuty{}
		if len(duty) > 0 {
			if err := json.Unmarshal(duty, &x.OnDuty); err != nil {
				return nil, err
			}
		}
		x.RecentDone = []dto.OnDuty{}
		if len(done) > 0 {
			if err := json.Unmarshal(done, &x.RecentDone); err != nil {
				return nil, err
			}
		}
		x.Imeis = make([]string, len(x.OnDuty))
		for i, d := range x.OnDuty {
			x.Imeis[i] = d.Imei
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *siteRepository) GetRoutes(ctx context.Context, q *dto.QueryGlobal) ([]dto.Route, int, error) {
	qb := database.NewQueryBuilder("routes r").Select(routeColumns...).WhereNull("r.deleted_at")

	n := 0
	next := func() int { n++; return n }

	if q.Search != "" {
		p := next()
		qb.Where(fmt.Sprintf("(r.name ILIKE $%d OR r.note ILIKE $%d)", p, p), "%"+q.Search+"%")
	}
	total, err := count(ctx, r.db, qb)
	if err != nil {
		return nil, 0, err
	}
	page(qb, q, map[string]string{"name": "r.name", "created_at": "r.created_at", "updated_at": "r.updated_at"}, "r.name", "ASC")

	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	routes, err := scanRoutes(rows)
	return routes, total, err
}

func (r *siteRepository) GetRouteByID(ctx context.Context, id string) (*dto.Route, error) {
	rows, err := database.NewQueryBuilder("routes r").Select(routeColumns...).
		Where("r.id = $1", id).
		WhereNull("r.deleted_at").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	list, err := scanRoutes(rows)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func (r *siteRepository) CreateRoute(ctx context.Context, body dto.UpsertRouteRequest, createdBy string) (string, error) {
	var id string
	err := database.NewInsertBuilder("routes").
		Set("name", strings.TrimSpace(body.Name)).
		Set("loading_site_id", body.LoadingID).
		Set("unloading_site_id", body.UnloadingID).
		Set("note", noteOf(body.Note)).
		SetCreatedBy(createdBy).
		QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *siteRepository) UpdateRoute(ctx context.Context, id string, body dto.UpsertRouteRequest, updatedBy string) (bool, error) {
	res, err := database.NewUpdateBuilder("routes").
		Set("name", strings.TrimSpace(body.Name)).
		Set("loading_site_id", body.LoadingID).
		Set("unloading_site_id", body.UnloadingID).
		Set("note", noteOf(body.Note)).
		SetUpdatedBy(updatedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *siteRepository) DeleteRoute(ctx context.Context, id, deletedBy string) (bool, error) {
	res, err := database.NewDeleteBuilder("routes").
		SetDeletedBy(deletedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

// RouteInUse: a route used by a shift or an assignment keeps its history, so it stays.
func (r *siteRepository) RouteInUse(ctx context.Context, id string) (bool, error) {
	n, err := count(ctx, r.db, database.NewQueryBuilder("shifts s").
		Where("s.deleted_at IS NULL").
		Where("(s.route_id = $1 OR EXISTS (SELECT 1 FROM assignments a WHERE a.shift_id = s.id AND a.route_id = $1))", id))
	return n > 0, err
}

// assignedIn: assignments of a started shift whose validity overlaps [from, to].
// p is the next free placeholder; QueryBuilder.Where does not renumber, so args go
// in the same order as their numbers.
func assignedIn(qb *database.QueryBuilder, p int, from, to int64) *database.QueryBuilder {
	return qb.
		Where("sh.started_at IS NOT NULL").
		Where(fmt.Sprintf("COALESCE(a.end_at, sh.closed_at, now()) >= $%d", p), database.FromMs(from)).
		Where(fmt.Sprintf("COALESCE(a.start_at, sh.started_at) <= $%d", p+1), database.FromMs(to))
}

func (r *siteRepository) RoutesForDevice(ctx context.Context, imei string, from, to int64) ([]dto.Route, error) {
	sub := assignedIn(database.NewQueryBuilder("assignments a").
		Select("a.route_id").
		InnerJoin("shifts sh", "sh.id = a.shift_id AND sh.deleted_at IS NULL").
		Where("a.imei = $1", imei), 2, from, to)
	subSQL, args := sub.Build()
	rows, err := database.NewQueryBuilder("routes r").Select(routeColumns...).
		Where("r.id IN ("+subSQL+")", args...).
		OrderBy("r.name").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	return scanRoutes(rows)
}

func (r *siteRepository) TrucksOnRoute(ctx context.Context, routeID string, from, to int64) ([]string, error) {
	rows, err := assignedIn(database.NewQueryBuilder("assignments a").
		Select("a.imei").
		Distinct().
		InnerJoin("shifts sh", "sh.id = a.shift_id AND sh.deleted_at IS NULL").
		Where("a.route_id = $1", routeID), 2, from, to).
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	return scanStrings(rows)
}

func scanStrings(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
