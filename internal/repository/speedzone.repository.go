package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// SpeedZoneRepository: zona batas kecepatan dan record GPS yang melebihinya (P3-3).
type SpeedZoneRepository interface {
	List(ctx context.Context, activeOnly bool) ([]dto.SpeedZone, error)
	Create(ctx context.Context, b dto.UpsertSpeedZoneRequest, by string) (string, error)
	Update(ctx context.Context, id string, b dto.UpsertSpeedZoneRequest, by string) (bool, error)
	Delete(ctx context.Context, id, by string) (bool, error)
	// OverspeedPoints: records inside an active zone faster than its limit, ordered by unit, zone, time.
	OverspeedPoints(ctx context.Context, from, to int64, limit int) ([]dto.OverspeedPoint, error)
}

type speedZoneRepository struct {
	db *sql.DB
}

func NewSpeedZoneRepository(db *sql.DB) SpeedZoneRepository {
	return &speedZoneRepository{db: db}
}

func (r *speedZoneRepository) List(ctx context.Context, activeOnly bool) ([]dto.SpeedZone, error) {
	qb := database.NewQueryBuilder("speed_zones").
		Select("id", "name", "kind::text", "lat", "lng", "radius_m", "limit_kmh", "active").
		WhereNull("deleted_at").
		OrderBy("name")
	if activeOnly {
		qb.Where("active")
	}
	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.SpeedZone{}
	for rows.Next() {
		var z dto.SpeedZone
		if err := rows.Scan(&z.ID, &z.Name, &z.Kind, &z.Lat, &z.Lng, &z.Radius, &z.LimitKmh, &z.Active); err != nil {
			return nil, err
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

func (r *speedZoneRepository) Create(ctx context.Context, b dto.UpsertSpeedZoneRequest, by string) (string, error) {
	var id string
	err := database.NewInsertBuilder("speed_zones").
		Set("name", strings.TrimSpace(b.Name)).
		SetRaw("kind", "'"+b.Kind+"'::speed_zone_kind").
		Set("lat", b.Lat).
		Set("lng", b.Lng).
		Set("radius_m", b.Radius).
		Set("limit_kmh", b.LimitKmh).
		Set("active", b.Active == nil || *b.Active).
		SetCreatedBy(by).
		QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *speedZoneRepository) Update(ctx context.Context, id string, b dto.UpsertSpeedZoneRequest, by string) (bool, error) {
	res, err := database.NewUpdateBuilder("speed_zones").
		Set("name", strings.TrimSpace(b.Name)).
		SetRaw("kind", "'"+b.Kind+"'::speed_zone_kind").
		Set("lat", b.Lat).
		Set("lng", b.Lng).
		Set("radius_m", b.Radius).
		Set("limit_kmh", b.LimitKmh).
		Set("active", b.Active == nil || *b.Active).
		SetUpdatedBy(by).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *speedZoneRepository) Delete(ctx context.Context, id, by string) (bool, error) {
	res, err := database.NewDeleteBuilder("speed_zones").
		SetDeletedBy(by).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *speedZoneRepository) OverspeedPoints(ctx context.Context, from, to int64, limit int) ([]dto.OverspeedPoint, error) {
	// insideSite("z") reads z.lat / z.lng / z.radius_m; the bounding box keeps the join cheap.
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT r.imei, r.time, r.speed, z.id, z.name, z.limit_kmh, drv.name
		FROM speed_zones z
		JOIN records r ON r.time >= $1 AND r.time < $2 AND r.valid AND r.speed > z.limit_kmh
			AND r.lat BETWEEN z.lat - z.radius_m / 111320.0 AND z.lat + z.radius_m / 111320.0
			AND `+insideSite("z")+`
		LEFT JOIN LATERAL (SELECT u.name FROM assignments a JOIN shifts s ON s.id = a.shift_id JOIN users u ON u.id = a.driver_id
			WHERE a.imei = r.imei AND s.started_at IS NOT NULL AND r.time >= COALESCE(a.start_at, s.started_at)
			  AND r.time < COALESCE(LEAST(a.end_at, s.closed_at), a.end_at, s.closed_at, 'infinity'::timestamptz)
			ORDER BY COALESCE(a.start_at, s.started_at) DESC LIMIT 1) drv ON true
		WHERE z.deleted_at IS NULL AND z.active
		ORDER BY r.imei, z.id, r.time
		LIMIT $3`, database.FromMs(from), database.FromMs(to), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.OverspeedPoint{}
	for rows.Next() {
		var p dto.OverspeedPoint
		var at time.Time
		var driver sql.NullString
		if err := rows.Scan(&p.Imei, &at, &p.Speed, &p.ZoneID, &p.ZoneName, &p.LimitKmh, &driver); err != nil {
			return nil, err
		}
		p.Time, p.DriverName = database.Ms(at), database.NullString(driver)
		out = append(out, p)
	}
	return out, rows.Err()
}
