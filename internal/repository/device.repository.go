package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// DeviceRepository covers devices and their AVL records.
type DeviceRepository interface {
	// RecordInstall / Installs: riwayat pemasangan tracker ke unit (P2-4).
	RecordInstall(ctx context.Context, imei, plate string, name *string, by string) error
	Installs(ctx context.Context, imei string) ([]dto.DeviceInstall, error)
	ListDevices(ctx context.Context) ([]dto.DeviceRow, error)
	// EnsureDevice registers a device (or restores a soft-deleted one). True if it is new.
	EnsureDevice(ctx context.Context, imei string) (bool, error)
	MarkConnected(ctx context.Context, imei, remote string) error
	SetDeviceMeta(ctx context.Context, imei string, m dto.DeviceMeta, updatedBy string) error
	DeleteDevice(ctx context.Context, imei, deletedBy string) (bool, error)

	// SaveRecords stores one packet in a transaction and returns the records that are
	// really new (not a re-send of an un-ACKed packet).
	SaveRecords(ctx context.Context, imei string, records []dto.AvlRecord) ([]dto.AvlRecord, error)
	LastRecords(ctx context.Context, imei string, limit int) ([]dto.AvlRecord, error)
	RecordsBetween(ctx context.Context, imei string, from, to int64, limit int) ([]dto.AvlRecord, error)
	// RecordsPage: stored records of one truck, newest first, with the total count.
	RecordsPage(ctx context.Context, imei string, from, to int64, limit, offset int) ([]dto.AvlRecord, int, error)
	Counts(ctx context.Context) (dto.DBCounts, error)
}

type deviceRepository struct {
	db *sql.DB
}

func NewDeviceRepository(db *sql.DB) DeviceRepository {
	return &deviceRepository{db: db}
}

func (r *deviceRepository) ListDevices(ctx context.Context) ([]dto.DeviceRow, error) {
	rows, err := database.NewQueryBuilder("devices d").
		Select("d.imei", "d.name", "d.plate", "d.vehicle", "d.payload_ton", "d.unit_type::text", "d.brand", "d.model", "d.tracker_model", "d.bucket_m3", "d.first_seen", "d.last_seen", "d.last_connected_at", "d.last_remote",
			"(SELECT COUNT(*) FROM records r WHERE r.imei = d.imei)::int AS records").
		WhereNull("d.deleted_at").
		OrderBy("d.first_seen").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.DeviceRow{}
	for rows.Next() {
		var d dto.DeviceRow
		var name, plate, vehicle, remote sql.NullString
		var first time.Time
		var last, conn sql.NullTime
		var brand, mdl, tracker sql.NullString
		var bucket sql.NullFloat64
		if err := rows.Scan(&d.Imei, &name, &plate, &vehicle, &d.PayloadTon, &d.UnitType, &brand, &mdl, &tracker, &bucket, &first, &last, &conn, &remote, &d.Records); err != nil {
			return nil, err
		}
		d.Brand, d.Model, d.TrackerModel, d.BucketM3 = database.NullString(brand), database.NullString(mdl), database.NullString(tracker), database.NullFloat(bucket)
		d.Name, d.Plate, d.Vehicle, d.LastRemote = database.NullString(name), database.NullString(plate), database.NullString(vehicle), database.NullString(remote)
		d.FirstSeen = database.Ms(first)
		d.LastSeen = database.NullMs(last)
		d.LastConnectedAt = database.NullMs(conn)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *deviceRepository) EnsureDevice(ctx context.Context, imei string) (bool, error) {
	var got string
	err := database.NewInsertBuilder("devices").
		Set("imei", imei).
		Set("first_seen", time.Now()).
		OnConflictDoNothing("imei").
		Returning("imei").
		QueryRowContext(ctx, r.db).Scan(&got)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	// Already there: bring a soft-deleted device back (it reconnected).
	res, err := database.NewUpdateBuilder("devices").
		SetRaw("deleted_at", "NULL").
		SetRaw("deleted_by", "NULL").
		SetRaw("first_seen", "CURRENT_TIMESTAMP").
		SetRaw("updated_at", "CURRENT_TIMESTAMP").
		Where("imei = $1", imei).
		Where("deleted_at IS NOT NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *deviceRepository) MarkConnected(ctx context.Context, imei, remote string) error {
	now := time.Now()
	_, err := database.NewUpdateBuilder("devices").
		Set("last_connected_at", now).
		Set("last_seen", now).
		Set("last_remote", remote).
		Where("imei = $1", imei).
		ExecuteContext(ctx, r.db)
	return err
}

func (r *deviceRepository) SetDeviceMeta(ctx context.Context, imei string, m dto.DeviceMeta, updatedBy string) error {
	ub := database.NewUpdateBuilder("devices").
		Set("name", m.Name).
		Set("plate", m.Plate).
		Set("vehicle", m.Vehicle)
	if m.PayloadTon != nil {
		ub.Set("payload_ton", *m.PayloadTon)
	}
	if m.UnitType != nil {
		ub.Set("unit_type", *m.UnitType)
	}
	for col, v := range map[string]*string{"brand": m.Brand, "model": m.Model, "tracker_model": m.TrackerModel} {
		if v != nil {
			ub.Set(col, nullIfBlank(*v))
		}
	}
	if m.BucketM3 != nil {
		if *m.BucketM3 > 0 {
			ub.Set("bucket_m3", *m.BucketM3)
		} else {
			ub.SetRaw("bucket_m3", "NULL")
		}
	}
	if updatedBy != "" {
		ub.SetUpdatedBy(updatedBy)
	} else {
		ub.SetRaw("updated_at", "CURRENT_TIMESTAMP")
	}
	// Where after every Set: its placeholder is numbered after the SET values.
	ub.Where("imei = $1", imei)
	_, err := ub.ExecuteContext(ctx, r.db)
	return err
}

// DeleteDevice soft-deletes the device. Its records stay for old reports; if the
// tracker reconnects it is registered again.
func (r *deviceRepository) DeleteDevice(ctx context.Context, imei, deletedBy string) (bool, error) {
	res, err := database.NewDeleteBuilder("devices").
		SetDeletedBy(deletedBy).
		Where("imei = $1", imei).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *deviceRepository) SaveRecords(ctx context.Context, imei string, records []dto.AvlRecord) ([]dto.AvlRecord, error) {
	fresh := []dto.AvlRecord{}
	if len(records) == 0 {
		return fresh, nil
	}
	now := time.Now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	for _, rec := range records {
		io, err := json.Marshal(rec.Io)
		if err != nil {
			return nil, err
		}
		var id string
		err = database.NewInsertBuilder("records").
			Set("imei", imei).
			Set("time", database.FromMs(rec.Time)).
			Set("received_at", now).
			Set("lat", rec.Lat).
			Set("lng", rec.Lng).
			Set("alt", rec.Alt).
			Set("angle", rec.Angle).
			Set("sats", rec.Sats).
			Set("speed", rec.Speed).
			Set("priority", rec.Priority).
			Set("event_io", rec.EventIo).
			Set("valid", rec.Valid).
			Set("io", string(io)).
			// The device re-sends a packet when the ACK is lost; this unique key drops the copy.
			OnConflictDoNothing("imei", "time", "event_io", "priority").
			QueryRowContext(ctx, tx).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		fresh = append(fresh, rec)
	}
	if _, err := database.NewUpdateBuilder("devices").
		Set("last_seen", now).
		Where("imei = $1", imei).
		ExecuteContext(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return fresh, nil
}

const recordColumns = "time, lat, lng, alt, angle, sats, speed, priority, event_io, valid, io, received_at"

func scanRecords(rows *sql.Rows) ([]dto.AvlRecord, error) {
	defer func() { _ = rows.Close() }()
	out := []dto.AvlRecord{}
	for rows.Next() {
		var rec dto.AvlRecord
		var t, received time.Time
		var io []byte
		if err := rows.Scan(&t, &rec.Lat, &rec.Lng, &rec.Alt, &rec.Angle, &rec.Sats, &rec.Speed, &rec.Priority, &rec.EventIo, &rec.Valid, &io, &received); err != nil {
			return nil, err
		}
		rec.Time = database.Ms(t)
		rec.ReceivedAt = database.Ms(received)
		rec.Io = dto.IoMap{}
		if len(io) > 0 {
			if err := json.Unmarshal(io, &rec.Io); err != nil {
				return nil, err
			}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// latest wraps "the newest N records" so the result comes back oldest first.
func (r *deviceRepository) latest(ctx context.Context, inner *database.QueryBuilder) ([]dto.AvlRecord, error) {
	innerSQL, args := inner.Build()
	rows, err := database.RawQueryContext(ctx, r.db, "SELECT "+recordColumns+" FROM ("+innerSQL+") x ORDER BY time ASC", args...)
	if err != nil {
		return nil, err
	}
	return scanRecords(rows)
}

func (r *deviceRepository) LastRecords(ctx context.Context, imei string, limit int) ([]dto.AvlRecord, error) {
	return r.latest(ctx, database.NewQueryBuilder("records").
		Select(recordColumns).
		Where("imei = $1", imei).
		OrderBy("time DESC").
		Limit(limit))
}

func (r *deviceRepository) RecordsBetween(ctx context.Context, imei string, from, to int64, limit int) ([]dto.AvlRecord, error) {
	return r.latest(ctx, database.NewQueryBuilder("records").
		Select(recordColumns).
		Where("imei = $1", imei).
		Where("time >= $2", database.FromMs(from)).
		Where("time <= $3", database.FromMs(to)).
		OrderBy("time DESC").
		Limit(limit))
}

func (r *deviceRepository) RecordsPage(ctx context.Context, imei string, from, to int64, limit, offset int) ([]dto.AvlRecord, int, error) {
	qb := database.NewQueryBuilder("records").
		Where("imei = $1", imei).
		Where("time >= $2", database.FromMs(from)).
		Where("time <= $3", database.FromMs(to))
	total, err := count(ctx, r.db, qb)
	if err != nil {
		return nil, 0, err
	}
	rows, err := qb.Select(recordColumns).OrderBy("time DESC").Limit(limit).Offset(offset).ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	recs, err := scanRecords(rows)
	return recs, total, err
}

// Counts is read once at startup; the tracker keeps the totals up to date in memory.
func (r *deviceRepository) Counts(ctx context.Context) (dto.DBCounts, error) {
	var c dto.DBCounts
	err := database.RawQueryRowContext(ctx, r.db, `SELECT
		(SELECT COUNT(*) FROM devices WHERE deleted_at IS NULL)::int,
		(SELECT COUNT(*) FROM records)::int,
		(SELECT COUNT(*) FROM events)::int`).Scan(&c.Devices, &c.Records, &c.Events)
	return c, err
}
