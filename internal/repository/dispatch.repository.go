package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// DispatchRepository: excavator per fleet (P2-5) and the data behind the live match factor.
type DispatchRepository interface {
	// CurrentLoaders: the open excavator period of every route (route id → loader).
	CurrentLoaders(ctx context.Context) (map[string]dto.FleetLoader, error)
	// SetLoader closes the route's open excavator period and, when imei is set, opens a new one.
	SetLoader(ctx context.Context, routeID, imei string, tph *float64, by string) error
	// SiteStays: stays of any unit inside the site radius overlapping [from, to) (open stay = until to).
	SiteStays(ctx context.Context, siteID string, from, to int64) ([]dto.StayInterval, error)
}

type dispatchRepository struct {
	db *sql.DB
}

func NewDispatchRepository(db *sql.DB) DispatchRepository {
	return &dispatchRepository{db: db}
}

func (r *dispatchRepository) CurrentLoaders(ctx context.Context) (map[string]dto.FleetLoader, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT f.id, f.route_id, f.imei, COALESCE(NULLIF(d.plate, ''), NULLIF(d.name, ''), f.imei),
			f.productivity_tph, f.start_at
		FROM fleet_loaders f JOIN devices d ON d.imei = f.imei
		WHERE f.end_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]dto.FleetLoader{}
	for rows.Next() {
		var l dto.FleetLoader
		var tph sql.NullFloat64
		var start time.Time
		if err := rows.Scan(&l.ID, &l.RouteID, &l.Imei, &l.Label, &tph, &start); err != nil {
			return nil, err
		}
		l.ProductivityTph, l.StartAt = database.NullFloat(tph), database.Ms(start)
		out[l.RouteID] = l
	}
	return out, rows.Err()
}

func (r *dispatchRepository) SetLoader(ctx context.Context, routeID, imei string, tph *float64, by string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// One excavator works one fleet: moving it ends its period on the other route too.
	if _, err := database.RawExecContext(ctx, tx, `UPDATE fleet_loaders SET end_at = CURRENT_TIMESTAMP
		WHERE end_at IS NULL AND (route_id = $1 OR ($2 <> '' AND imei = $2))`, routeID, imei); err != nil {
		return err
	}
	if imei != "" {
		var createdBy any
		if by != "" {
			createdBy = by
		}
		if _, err := database.RawExecContext(ctx, tx, `INSERT INTO fleet_loaders (route_id, imei, productivity_tph, created_by)
			VALUES ($1, $2, $3, $4)`, routeID, imei, tph, createdBy); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *dispatchRepository) SiteStays(ctx context.Context, siteID string, from, to int64) ([]dto.StayInterval, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT
			(EXTRACT(EPOCH FROM GREATEST(arrive_at, $2)) * 1000)::bigint,
			(EXTRACT(EPOCH FROM LEAST(COALESCE(depart_at, $3), $3)) * 1000)::bigint
		FROM site_visits WHERE site_id = $1 AND arrive_at < $3 AND COALESCE(depart_at, $3) > $2
		ORDER BY arrive_at`, siteID, database.FromMs(from), database.FromMs(to))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.StayInterval{}
	for rows.Next() {
		var s dto.StayInterval
		if err := rows.Scan(&s.Start, &s.End); err != nil {
			return nil, err
		}
		if s.End > s.Start {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}
