package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// BreakdownLogs: breakdown status logs overlapping [from, to), ordered by unit and time (P3-5).
func (r *productionRepository) BreakdownLogs(ctx context.Context, from, to int64) ([]dto.BreakdownLog, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT l.imei, COALESCE(NULLIF(v.plate, ''), NULLIF(v.name, ''), l.imei),
			l.delay_code, c.name, u.name, l.note, l.source::text, l.start_at, l.end_at
		FROM unit_status_logs l
		LEFT JOIN devices v ON v.imei = l.imei
		LEFT JOIN delay_codes c ON c.code = l.delay_code
		LEFT JOIN users u ON u.id = l.driver_id
		WHERE l.status = 'breakdown' AND l.start_at < $2 AND COALESCE(l.end_at, now()) > $1
		ORDER BY l.imei, l.start_at`, database.FromMs(from), database.FromMs(to))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.BreakdownLog{}
	for rows.Next() {
		var b dto.BreakdownLog
		var code, codeName, driver, note sql.NullString
		var start time.Time
		var end sql.NullTime
		if err := rows.Scan(&b.Imei, &b.Unit, &code, &codeName, &driver, &note, &b.Source, &start, &end); err != nil {
			return nil, err
		}
		b.Code, b.CodeName, b.DriverName, b.Note = database.NullString(code), database.NullString(codeName), database.NullString(driver), database.NullString(note)
		b.StartAt, b.EndAt = database.Ms(start), database.NullMs(end)
		out = append(out, b)
	}
	return out, rows.Err()
}
