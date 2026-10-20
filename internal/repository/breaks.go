package repository

import (
	"context"
	"encoding/json"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/lib/pq"
)

// BreaksOf parses the breaks JSONB column of shifts / shift_templates ("" or invalid = none).
func BreaksOf(raw string) []dto.ShiftBreak {
	out := []dto.ShiftBreak{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

// BreaksJSON is the value written to a breaks JSONB column (as text, Postgres casts it).
func BreaksJSON(list []dto.ShiftBreak) string {
	if len(list) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func (r *shiftRepository) UnknownDelayCodes(ctx context.Context, codes []string) ([]string, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT c FROM unnest($1::text[]) c
		WHERE NOT EXISTS (SELECT 1 FROM delay_codes d WHERE d.code = c AND d.active)`, pq.Array(codes))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// relinkStatus recomputes end_at (each status lasts until the next one of the unit) after an insert.
const relinkStatus = `UPDATE unit_status_logs u SET end_at = n.next_at
	FROM (SELECT id, LEAD(start_at) OVER (ORDER BY start_at, received_at) AS next_at
		FROM unit_status_logs WHERE imei = $1) n
	WHERE u.id = n.id AND u.end_at IS DISTINCT FROM n.next_at`

// StartBreak: every open assignment of the shift whose unit is not in breakdown gets a planned
// standby with the break's delay code from the break start. Skipped when the unit already has this
// break, or its current status was set after the break started (the operator decides then).
func (r *mdtRepository) StartBreak(ctx context.Context, shiftID, code string, start int64) ([]string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := database.RawQueryContext(ctx, tx, `INSERT INTO unit_status_logs (imei, driver_id, assignment_id, status, delay_code, start_at, source, note)
		SELECT a.imei, a.driver_id, a.id, 'standby', $2, $3, 'auto', 'Istirahat terjadwal'
		FROM assignments a
		LEFT JOIN LATERAL (SELECT l.start_at, l.status FROM unit_status_logs l WHERE l.imei = a.imei AND l.start_at <= $3
			ORDER BY l.start_at DESC, l.received_at DESC LIMIT 1) cur ON true
		WHERE a.shift_id = $1 AND a.end_at IS NULL
			AND COALESCE(cur.status::text, '') <> 'breakdown'
			AND NOT EXISTS (SELECT 1 FROM unit_status_logs l WHERE l.imei = a.imei AND l.start_at >= $3)
		RETURNING imei`, shiftID, code, database.FromMs(start))
	if err != nil {
		return nil, err
	}
	imeis := []string{}
	for rows.Next() {
		var imei string
		if err := rows.Scan(&imei); err != nil {
			_ = rows.Close()
			return nil, err
		}
		imeis = append(imeis, imei)
	}
	_ = rows.Close()
	for _, imei := range imeis {
		if _, err := database.RawExecContext(ctx, tx, relinkStatus, imei); err != nil {
			return nil, err
		}
	}
	return imeis, tx.Commit()
}

// EndBreak: units still on the automatic break status go back to the status they had before it
// (working without a code when there was none), from the break end.
func (r *mdtRepository) EndBreak(ctx context.Context, shiftID, code string, start, end int64) ([]string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := database.RawQueryContext(ctx, tx, `INSERT INTO unit_status_logs (imei, driver_id, assignment_id, status, delay_code, start_at, source, note)
		SELECT b.imei, b.driver_id, b.assignment_id, COALESCE(prev.status, 'working'), prev.delay_code, $4, 'auto', 'Istirahat selesai'
		FROM unit_status_logs b
		JOIN assignments a ON a.id = b.assignment_id AND a.shift_id = $1
		LEFT JOIN LATERAL (SELECT p.status, p.delay_code FROM unit_status_logs p WHERE p.imei = b.imei AND p.start_at < b.start_at
			ORDER BY p.start_at DESC, p.received_at DESC LIMIT 1) prev ON true
		WHERE b.source = 'auto' AND b.delay_code = $2 AND b.start_at = $3 AND b.end_at IS NULL
		RETURNING imei`, shiftID, code, database.FromMs(start), database.FromMs(end))
	if err != nil {
		return nil, err
	}
	imeis := []string{}
	for rows.Next() {
		var imei string
		if err := rows.Scan(&imei); err != nil {
			_ = rows.Close()
			return nil, err
		}
		imeis = append(imeis, imei)
	}
	_ = rows.Close()
	for _, imei := range imeis {
		if _, err := database.RawExecContext(ctx, tx, relinkStatus, imei); err != nil {
			return nil, err
		}
	}
	return imeis, tx.Commit()
}
