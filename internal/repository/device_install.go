package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

func nullIfBlank(s string) any {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	return s
}

// RecordInstall: the tracker is now on the unit with this plate. The open install (other plate)
// is closed and a new one is opened; the same plate again is a no-op.
func (r *deviceRepository) RecordInstall(ctx context.Context, imei, plate string, name *string, by string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var open string
	err = database.RawQueryRowContext(ctx, tx, `SELECT plate FROM device_installs WHERE imei = $1 AND removed_at IS NULL
		ORDER BY installed_at DESC LIMIT 1`, imei).Scan(&open)
	if err == nil && open == plate {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if _, err := database.RawExecContext(ctx, tx, `UPDATE device_installs SET removed_at = CURRENT_TIMESTAMP
		WHERE imei = $1 AND removed_at IS NULL`, imei); err != nil {
		return err
	}
	var createdBy any
	if by != "" {
		createdBy = by
	}
	if _, err := database.RawExecContext(ctx, tx, `INSERT INTO device_installs (imei, plate, name, installed_at, created_by)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP, $4)`, imei, plate, name, createdBy); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *deviceRepository) Installs(ctx context.Context, imei string) ([]dto.DeviceInstall, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT id, imei, plate, name, installed_at, removed_at
		FROM device_installs WHERE imei = $1 ORDER BY installed_at DESC`, imei)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.DeviceInstall{}
	for rows.Next() {
		var d dto.DeviceInstall
		var name sql.NullString
		var installed time.Time
		var removed sql.NullTime
		if err := rows.Scan(&d.ID, &d.Imei, &d.Plate, &name, &installed, &removed); err != nil {
			return nil, err
		}
		d.Name = database.NullString(name)
		d.InstalledAt, d.RemovedAt = database.Ms(installed), database.NullMs(removed)
		out = append(out, d)
	}
	return out, rows.Err()
}
