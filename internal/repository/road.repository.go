package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/routing"
)

// RoadRepository stores road data (implements routing.Store) and lists/deletes it for
// fms-administrator.
type RoadRepository interface {
	routing.Store
	ListPaths(ctx context.Context, q *dto.QueryGlobal) ([]dto.RoadPathRow, int, error)
	GetPathByID(ctx context.Context, id string) (*dto.RoadPathRow, error)
	DeletePath(ctx context.Context, id string) (bool, error)
	DeleteAllPaths(ctx context.Context) (int, error)
	ListAreas(ctx context.Context) ([]dto.RoadAreaRow, error)
	DeleteArea(ctx context.Context, id string) (bool, error)
	DeleteAllAreas(ctx context.Context) (int, error)
}

type roadRepository struct {
	db *sql.DB
}

func NewRoadRepository(db *sql.DB) RoadRepository {
	return &roadRepository{db: db}
}

// ---------- routing.Store ----------

func (r *roadRepository) GetPath(ctx context.Context, key string) (*routing.RoadResult, bool) {
	var coords []byte
	var res routing.RoadResult
	var used time.Time
	err := database.RawQueryRowContext(ctx, r.db,
		`SELECT coords, distance_m, duration_s, last_used_at FROM road_paths WHERE key = $1`, key).
		Scan(&coords, &res.Distance, &res.DurationS, &used)
	if err != nil || json.Unmarshal(coords, &res.Coords) != nil {
		return nil, false
	}
	if time.Since(used) > time.Hour {
		_, _ = database.RawExecContext(ctx, r.db, `UPDATE road_paths SET last_used_at = CURRENT_TIMESTAMP WHERE key = $1`, key)
	}
	return &res, true
}

func (r *roadRepository) PutPath(ctx context.Context, key string, from, to routing.LatLng, res *routing.RoadResult) {
	coords, err := json.Marshal(res.Coords)
	if err != nil {
		return
	}
	_, err = database.RawExecContext(ctx, r.db, `
		INSERT INTO road_paths (key, from_lat, from_lng, to_lat, to_lng, coords, point_count, distance_m, duration_s)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (key) DO UPDATE SET coords = EXCLUDED.coords, point_count = EXCLUDED.point_count,
			distance_m = EXCLUDED.distance_m, duration_s = EXCLUDED.duration_s, last_used_at = CURRENT_TIMESTAMP`,
		key, from.Lat, from.Lng, to.Lat, to.Lng, string(coords), len(res.Coords), res.Distance, res.DurationS)
	if err != nil {
		logger.Warn("Gagal nyimpen jalur jalan: " + err.Error())
	}
}

func (r *roadRepository) GetArea(ctx context.Context, key string, maxAge time.Duration) ([]byte, bool) {
	var raw []byte
	err := database.RawQueryRowContext(ctx, r.db,
		`SELECT elements FROM road_areas WHERE key = $1 AND fetched_at > $2`, key, time.Now().Add(-maxAge)).Scan(&raw)
	if err != nil {
		return nil, false
	}
	return raw, true
}

func (r *roadRepository) PutArea(ctx context.Context, key string, bbox [4]float64, kind string, raw []byte, elements int) {
	_, err := database.RawExecContext(ctx, r.db, `
		INSERT INTO road_areas (key, kind, south, west, north, east, elements, element_count, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (key) DO UPDATE SET elements = EXCLUDED.elements, element_count = EXCLUDED.element_count,
			size_bytes = EXCLUDED.size_bytes, fetched_at = CURRENT_TIMESTAMP`,
		key, kind, bbox[0], bbox[1], bbox[2], bbox[3], string(raw), elements, len(raw))
	if err != nil {
		logger.Warn("Gagal nyimpen data jalan area: " + err.Error())
	}
}

// ---------- fms-administrator ----------

// nearSite: name of the site within 50 m of a point (rough box + distance).
const nearSite = `(SELECT s.name FROM sites s WHERE s.deleted_at IS NULL
	AND abs(s.lat - %[1]s) < 0.0005 AND abs(s.lng - %[2]s) < 0.0005
	ORDER BY (s.lat - %[1]s)^2 + (s.lng - %[2]s)^2 LIMIT 1)`

func pathSelect(withCoords bool) []string {
	cols := []string{"p.id", "p.from_lat", "p.from_lng", "p.to_lat", "p.to_lng",
		fmt.Sprintf(nearSite, "p.from_lat", "p.from_lng"), fmt.Sprintf(nearSite, "p.to_lat", "p.to_lng"),
		"p.point_count", "p.distance_m", "p.duration_s", "p.created_at", "p.last_used_at"}
	if withCoords {
		cols = append(cols, "p.coords")
	}
	return cols
}

func scanPath(row interface{ Scan(...any) error }, withCoords bool) (dto.RoadPathRow, error) {
	var p dto.RoadPathRow
	var from, to sql.NullString
	var created, used time.Time
	dest := []any{&p.ID, &p.FromLat, &p.FromLng, &p.ToLat, &p.ToLng, &from, &to, &p.PointCount, &p.DistanceM, &p.DurationS, &created, &used}
	var coords []byte
	if withCoords {
		dest = append(dest, &coords)
	}
	if err := row.Scan(dest...); err != nil {
		return p, err
	}
	p.FromSite, p.ToSite = database.NullString(from), database.NullString(to)
	p.CreatedAt, p.LastUsedAt = database.Ms(created), database.Ms(used)
	if withCoords {
		_ = json.Unmarshal(coords, &p.Coords)
	}
	return p, nil
}

func (r *roadRepository) ListPaths(ctx context.Context, q *dto.QueryGlobal) ([]dto.RoadPathRow, int, error) {
	qb := database.NewQueryBuilder("road_paths p").Select(pathSelect(false)...)
	total, err := count(ctx, r.db, database.NewQueryBuilder("road_paths p"))
	if err != nil {
		return nil, 0, err
	}
	qb.OrderBy("p.created_at DESC")
	if q.Paginated() {
		limit, offset := database.GetPagination(q.Page, q.Limit)
		qb.Limit(limit).Offset(offset)
	}
	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.RoadPathRow{}
	for rows.Next() {
		p, err := scanPath(rows, false)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

func (r *roadRepository) GetPathByID(ctx context.Context, id string) (*dto.RoadPathRow, error) {
	query, args := database.NewQueryBuilder("road_paths p").Select(pathSelect(true)...).Where("p.id = $1", id).Build()
	p, err := scanPath(database.RawQueryRowContext(ctx, r.db, query, args...), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *roadRepository) deleteWhere(ctx context.Context, table, where string, args ...any) (int, error) {
	res, err := database.RawExecContext(ctx, r.db, "DELETE FROM "+table+" "+where, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (r *roadRepository) DeletePath(ctx context.Context, id string) (bool, error) {
	n, err := r.deleteWhere(ctx, "road_paths", "WHERE id = $1", id)
	return n > 0, err
}

func (r *roadRepository) DeleteAllPaths(ctx context.Context) (int, error) {
	return r.deleteWhere(ctx, "road_paths", "")
}

func (r *roadRepository) ListAreas(ctx context.Context) ([]dto.RoadAreaRow, error) {
	rows, err := database.NewQueryBuilder("road_areas a").
		Select("a.id", "a.kind", "a.south", "a.west", "a.north", "a.east", "a.element_count", "a.size_bytes", "a.fetched_at").
		OrderBy("a.fetched_at DESC").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.RoadAreaRow{}
	for rows.Next() {
		var a dto.RoadAreaRow
		var at time.Time
		if err := rows.Scan(&a.ID, &a.Kind, &a.South, &a.West, &a.North, &a.East, &a.ElementCount, &a.SizeBytes, &at); err != nil {
			return nil, err
		}
		a.FetchedAt = database.Ms(at)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *roadRepository) DeleteArea(ctx context.Context, id string) (bool, error) {
	n, err := r.deleteWhere(ctx, "road_areas", "WHERE id = $1", id)
	return n > 0, err
}

func (r *roadRepository) DeleteAllAreas(ctx context.Context) (int, error) {
	return r.deleteWhere(ctx, "road_areas", "")
}
