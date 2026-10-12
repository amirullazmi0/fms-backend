package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// EventRepository stores detected fleet events, GPRS command lines and the
// reverse-geocode cache. All are append-only logs (no soft delete).
type EventRepository interface {
	SaveEvent(ctx context.Context, ev dto.FleetEvent) error
	LastEvents(ctx context.Context, limit int) ([]dto.FleetEvent, error)
	SaveCommand(ctx context.Context, c dto.CommandLine) error
	LastCommands(ctx context.Context, limit int) ([]dto.CommandLine, error)
	ListPlaces(ctx context.Context) (map[string]*string, error)
	SavePlace(ctx context.Context, key string, label *string) error
}

type eventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) EventRepository {
	return &eventRepository{db: db}
}

func (r *eventRepository) SaveEvent(ctx context.Context, ev dto.FleetEvent) error {
	_, err := database.NewInsertBuilder("events").
		Set("imei", ev.Imei).
		Set("time", database.FromMs(ev.Time)).
		Set("level", ev.Level).
		Set("kind", ev.Kind).
		Set("text", ev.Text).
		WithoutReturning().
		ExecContext(ctx, r.db)
	return err
}

func (r *eventRepository) LastEvents(ctx context.Context, limit int) ([]dto.FleetEvent, error) {
	inner, args := database.NewQueryBuilder("events e").
		Select("e.imei", "e.time", "e.level", "e.kind", "e.text").
		InnerJoin("devices d", "d.imei = e.imei AND d.deleted_at IS NULL").
		OrderBy("e.time DESC").
		Limit(limit).
		Build()
	rows, err := database.RawQueryContext(ctx, r.db, "SELECT imei, time, level, kind, text FROM ("+inner+") x ORDER BY time ASC", args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.FleetEvent{}
	for rows.Next() {
		var ev dto.FleetEvent
		var t time.Time
		if err := rows.Scan(&ev.Imei, &t, &ev.Level, &ev.Kind, &ev.Text); err != nil {
			return nil, err
		}
		ev.Time = database.Ms(t)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (r *eventRepository) SaveCommand(ctx context.Context, c dto.CommandLine) error {
	_, err := database.NewInsertBuilder("commands").
		Set("imei", c.Imei).
		Set("time", database.FromMs(c.Time)).
		Set("dir", c.Dir).
		Set("text", c.Text).
		WithoutReturning().
		ExecContext(ctx, r.db)
	return err
}

func (r *eventRepository) LastCommands(ctx context.Context, limit int) ([]dto.CommandLine, error) {
	inner, args := database.NewQueryBuilder("commands").
		Select("imei", "time", "dir", "text").
		OrderBy("time DESC").
		Limit(limit).
		Build()
	rows, err := database.RawQueryContext(ctx, r.db, "SELECT imei, time, dir, text FROM ("+inner+") x ORDER BY time ASC", args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.CommandLine{}
	for rows.Next() {
		var c dto.CommandLine
		var t time.Time
		if err := rows.Scan(&c.Imei, &t, &c.Dir, &c.Text); err != nil {
			return nil, err
		}
		c.Time = database.Ms(t)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *eventRepository) ListPlaces(ctx context.Context) (map[string]*string, error) {
	rows, err := database.NewQueryBuilder("places").Select("key", "label").ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]*string{}
	for rows.Next() {
		var key string
		var label sql.NullString
		if err := rows.Scan(&key, &label); err != nil {
			return nil, err
		}
		out[key] = database.NullString(label)
	}
	return out, rows.Err()
}

func (r *eventRepository) SavePlace(ctx context.Context, key string, label *string) error {
	query, args := database.NewInsertBuilder("places").
		Set("key", key).
		Set("label", label).
		Set("fetched_at", time.Now()).
		WithoutReturning().
		Build()
	_, err := database.RawExecContext(ctx, r.db, query+" ON CONFLICT (key) DO UPDATE SET label = EXCLUDED.label, fetched_at = EXCLUDED.fetched_at", args...)
	return err
}
