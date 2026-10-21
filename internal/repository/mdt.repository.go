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
)

// MdtRepository covers delay codes, unit status logs and the MDT tablets in the cabs.
type MdtRepository interface {
	// StartBreak / EndBreak: scheduled break of a shift (P2-3); returns the units changed.
	StartBreak(ctx context.Context, shiftID, code string, start int64) ([]string, error)
	EndBreak(ctx context.Context, shiftID, code string, start, end int64) ([]string, error)
	DelayCodes(ctx context.Context, activeOnly bool) ([]dto.DelayCode, error)
	DelayCode(ctx context.Context, code string) (*dto.DelayCode, error)
	UpsertDelayCode(ctx context.Context, body dto.UpsertDelayCodeRequest, userID string) error

	// InsertStatuses stores status changes (an existing id is skipped: re-sent from the
	// tablet), then re-links the periods of each unit (end = start of the next change).
	// Returns the ids now stored (new or already there).
	InsertStatuses(ctx context.Context, items []dto.UnitStatusInput) ([]string, error)
	StatusLogs(ctx context.Context, imei string, from, to int64) ([]dto.UnitStatusLog, error)
	// CurrentStatuses: the open status of every unit (imei → log).
	CurrentStatuses(ctx context.Context) (map[string]dto.UnitStatusLog, error)

	CreatePairCode(ctx context.Context, imei, codeHash string, expires int64, createdBy string) error
	// Pair redeems a pairing code. Returns the tablet id and unit, or "" when the code is wrong / expired.
	Pair(ctx context.Context, codeHash, tokenHash string, name *string) (id, imei string, err error)
	TabletByToken(ctx context.Context, tokenHash string) (id, imei string, err error)
	TouchTablet(ctx context.Context, id string) error
	Tablets(ctx context.Context, imei string) ([]dto.MdtTablet, error)
	RevokeTablet(ctx context.Context, id string) (bool, error)

	SetPin(ctx context.Context, driverID, pinHash, updatedBy string) (bool, error)
	// OperatorByEmployeeNo: active driver with that employee number and its PIN hash.
	OperatorByEmployeeNo(ctx context.Context, employeeNo string) (*dto.MdtOperator, string, error)
	Operators(ctx context.Context) ([]dto.MdtOperator, error)
	// Duty: the unit's assignment in an active shift, else in the next planned one.
	Duty(ctx context.Context, imei string) (*dto.MdtDuty, error)
}

type mdtRepository struct {
	db *sql.DB
}

func NewMdtRepository(db *sql.DB) MdtRepository {
	return &mdtRepository{db: db}
}

// ---------- Delay codes ----------

var delayColumns = []string{"code", "name", "category::text", "icon", "sort", "active"}

func scanDelay(row interface{ Scan(...any) error }) (dto.DelayCode, error) {
	var d dto.DelayCode
	var icon sql.NullString
	if err := row.Scan(&d.Code, &d.Name, &d.Category, &icon, &d.Sort, &d.Active); err != nil {
		return d, err
	}
	d.Icon = database.NullString(icon)
	d.Status = dto.StatusOf(d.Category)
	return d, nil
}

func (r *mdtRepository) DelayCodes(ctx context.Context, activeOnly bool) ([]dto.DelayCode, error) {
	qb := database.NewQueryBuilder("delay_codes").Select(delayColumns...).OrderBy("sort").OrderBy("name")
	if activeOnly {
		qb.Where("active")
	}
	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.DelayCode{}
	for rows.Next() {
		d, err := scanDelay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *mdtRepository) DelayCode(ctx context.Context, code string) (*dto.DelayCode, error) {
	query, args := database.NewQueryBuilder("delay_codes").Select(delayColumns...).Where("code = $1", code).Build()
	d, err := scanDelay(database.RawQueryRowContext(ctx, r.db, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *mdtRepository) UpsertDelayCode(ctx context.Context, b dto.UpsertDelayCodeRequest, userID string) error {
	active := true
	if b.Active != nil {
		active = *b.Active
	}
	_, err := database.RawExecContext(ctx, r.db, `INSERT INTO delay_codes (code, name, category, icon, sort, active, created_by, updated_by)
		VALUES ($1, $2, $3::delay_category, $4, $5, $6, $7, $7)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, category = EXCLUDED.category, icon = EXCLUDED.icon,
			sort = EXCLUDED.sort, active = EXCLUDED.active, updated_by = EXCLUDED.updated_by, updated_at = CURRENT_TIMESTAMP`,
		b.Code, b.Name, b.Category, b.Icon, b.Sort, active, userID)
	return err
}

// ---------- Unit status ----------

func (r *mdtRepository) InsertStatuses(ctx context.Context, items []dto.UnitStatusInput) ([]string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	stored := []string{}
	units := map[string]bool{}
	for _, it := range items {
		ib := database.NewInsertBuilder("unit_status_logs").
			Set("imei", it.Imei).
			Set("driver_id", it.DriverID).
			Set("status", it.Status).
			Set("delay_code", it.DelayCode).
			Set("start_at", database.FromMs(it.StartAt)).
			Set("client_at", database.FromMsPtr(it.ClientAt)).
			Set("source", it.Source).
			Set("note", it.Note).
			Set("created_by", it.CreatedBy).
			OnConflictDoNothing("id")
		if it.ID != "" {
			ib.Set("id", it.ID)
		}
		var id string
		err := ib.QueryRowContext(ctx, tx).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			stored = append(stored, it.ID) // already stored by an earlier send
			continue
		}
		if err != nil {
			return nil, err
		}
		stored = append(stored, id)
		units[it.Imei] = true
	}
	// Each status lasts until the next one of the same unit (late changes slot in by time).
	for imei := range units {
		if _, err := database.RawExecContext(ctx, tx, `UPDATE unit_status_logs u SET end_at = n.next_at
			FROM (SELECT id, LEAD(start_at) OVER (ORDER BY start_at, received_at) AS next_at
				FROM unit_status_logs WHERE imei = $1) n
			WHERE u.id = n.id AND u.end_at IS DISTINCT FROM n.next_at`, imei); err != nil {
			return nil, err
		}
		if _, err := database.RawExecContext(ctx, tx, `UPDATE unit_status_logs u SET assignment_id = a.id
			FROM assignments a JOIN shifts s ON s.id = a.shift_id AND s.started_at IS NOT NULL
			WHERE u.imei = $1 AND u.assignment_id IS NULL AND a.imei = u.imei
			  AND u.start_at >= COALESCE(a.start_at, s.started_at)
			  AND u.start_at < COALESCE(LEAST(a.end_at, s.closed_at), a.end_at, s.closed_at, 'infinity'::timestamptz)`, imei); err != nil {
			return nil, err
		}
	}
	return stored, tx.Commit()
}

var statusColumns = []string{
	"u.id", "u.imei", "COALESCE(v.plate, v.name, u.imei)", "u.driver_id", "d.name", "u.status::text", "u.delay_code", "c.name",
	"c.category::text", "u.start_at", "u.end_at", "u.source::text", "u.note", "u.received_at",
}

func statusQuery() *database.QueryBuilder {
	return database.NewQueryBuilder("unit_status_logs u").Select(statusColumns...).
		LeftJoin("devices v", "v.imei = u.imei").
		LeftJoin("users d", "d.id = u.driver_id").
		LeftJoin("delay_codes c", "c.code = u.delay_code")
}

func scanStatusLogs(rows *sql.Rows) ([]dto.UnitStatusLog, error) {
	defer func() { _ = rows.Close() }()
	out := []dto.UnitStatusLog{}
	for rows.Next() {
		var l dto.UnitStatusLog
		var driver, driverName, code, codeName, category, note sql.NullString
		var start, received time.Time
		var end sql.NullTime
		if err := rows.Scan(&l.ID, &l.Imei, &l.Truck, &driver, &driverName, &l.Status, &code, &codeName, &category,
			&start, &end, &l.Source, &note, &received); err != nil {
			return nil, err
		}
		l.DriverID, l.DriverName = database.NullString(driver), database.NullString(driverName)
		l.DelayCode, l.DelayName, l.Category = database.NullString(code), database.NullString(codeName), database.NullString(category)
		l.Note = database.NullString(note)
		l.StartAt, l.EndAt, l.ReceivedAt = database.Ms(start), database.NullMs(end), database.Ms(received)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *mdtRepository) StatusLogs(ctx context.Context, imei string, from, to int64) ([]dto.UnitStatusLog, error) {
	qb := statusQuery().
		Where("u.start_at <= $1", database.FromMs(to)).
		Where("COALESCE(u.end_at, 'infinity'::timestamptz) >= $2", database.FromMs(from))
	if imei != "" {
		qb.Where("u.imei = $3", imei)
	}
	rows, err := qb.OrderBy("u.start_at DESC").Limit(2000).ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	return scanStatusLogs(rows)
}

func (r *mdtRepository) CurrentStatuses(ctx context.Context) (map[string]dto.UnitStatusLog, error) {
	rows, err := statusQuery().WhereNull("u.end_at").ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	list, err := scanStatusLogs(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string]dto.UnitStatusLog, len(list))
	for _, l := range list {
		out[l.Imei] = l
	}
	return out, nil
}

// ---------- Tablets ----------

func (r *mdtRepository) CreatePairCode(ctx context.Context, imei, codeHash string, expires int64, createdBy string) error {
	_, err := database.NewInsertBuilder("mdt_tablets").
		Set("imei", imei).
		Set("pair_code_hash", codeHash).
		Set("pair_code_expires", database.FromMs(expires)).
		Set("created_by", createdBy).
		WithoutReturning().
		ExecContext(ctx, r.db)
	return err
}

func (r *mdtRepository) Pair(ctx context.Context, codeHash, tokenHash string, name *string) (string, string, error) {
	var id, imei string
	err := database.RawQueryRowContext(ctx, r.db, `UPDATE mdt_tablets SET token_hash = $2, name = $3, paired_at = now(),
			pair_code_hash = NULL, pair_code_expires = NULL
		WHERE id = (SELECT id FROM mdt_tablets WHERE pair_code_hash = $1 AND pair_code_expires > now()
			AND token_hash IS NULL AND revoked_at IS NULL ORDER BY created_at DESC LIMIT 1)
		RETURNING id, imei`, codeHash, tokenHash, name).Scan(&id, &imei)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return id, imei, err
}

func (r *mdtRepository) TabletByToken(ctx context.Context, tokenHash string) (string, string, error) {
	var id, imei string
	err := database.RawQueryRowContext(ctx, r.db, `SELECT t.id, t.imei FROM mdt_tablets t
		JOIN devices d ON d.imei = t.imei AND d.deleted_at IS NULL
		WHERE t.token_hash = $1 AND t.revoked_at IS NULL`, tokenHash).Scan(&id, &imei)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return id, imei, err
}

func (r *mdtRepository) TouchTablet(ctx context.Context, id string) error {
	_, err := database.RawExecContext(ctx, r.db, `UPDATE mdt_tablets SET last_sync_at = now() WHERE id = $1`, id)
	return err
}

func (r *mdtRepository) Tablets(ctx context.Context, imei string) ([]dto.MdtTablet, error) {
	rows, err := database.NewQueryBuilder("mdt_tablets").
		Select("id", "imei", "name", "paired_at", "last_sync_at", "revoked_at", "created_at").
		Where("imei = $1", imei).
		WhereNotNull("paired_at").
		OrderBy("paired_at DESC").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.MdtTablet{}
	for rows.Next() {
		var t dto.MdtTablet
		var name sql.NullString
		var paired, synced, revoked sql.NullTime
		var created time.Time
		if err := rows.Scan(&t.ID, &t.Imei, &name, &paired, &synced, &revoked, &created); err != nil {
			return nil, err
		}
		t.Name = database.NullString(name)
		t.PairedAt, t.LastSyncAt, t.RevokedAt = database.NullMs(paired), database.NullMs(synced), database.NullMs(revoked)
		t.CreatedAt = database.Ms(created)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *mdtRepository) RevokeTablet(ctx context.Context, id string) (bool, error) {
	return affected(database.RawExecContext(ctx, r.db, `UPDATE mdt_tablets SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id))
}

// ---------- Operators ----------

func (r *mdtRepository) SetPin(ctx context.Context, driverID, pinHash, updatedBy string) (bool, error) {
	return affected(database.NewUpdateBuilder("users").
		Set("pin_hash", pinHash).
		SetUpdatedBy(updatedBy).
		Where("id = $1", driverID).
		Where("user_type = 'operator'").
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db))
}

func (r *mdtRepository) OperatorByEmployeeNo(ctx context.Context, employeeNo string) (*dto.MdtOperator, string, error) {
	var o dto.MdtOperator
	var emp, hash sql.NullString
	err := database.RawQueryRowContext(ctx, r.db, `SELECT id, COALESCE(name, ''), employee_no, pin_hash FROM users
		WHERE user_type = 'operator' AND lower(employee_no) = lower($1) AND is_active AND deleted_at IS NULL LIMIT 1`, employeeNo).Scan(&o.ID, &o.Name, &emp, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	o.EmployeeNo = database.NullString(emp)
	return &o, hash.String, nil
}

func (r *mdtRepository) Operators(ctx context.Context) ([]dto.MdtOperator, error) {
	rows, err := database.RawQueryContext(ctx, r.db, `SELECT id, COALESCE(name, ''), employee_no FROM users
		WHERE user_type = 'operator' AND is_active AND deleted_at IS NULL AND pin_hash IS NOT NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.MdtOperator{}
	for rows.Next() {
		var o dto.MdtOperator
		var emp sql.NullString
		if err := rows.Scan(&o.ID, &o.Name, &emp); err != nil {
			return nil, err
		}
		o.EmployeeNo = database.NullString(emp)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *mdtRepository) Duty(ctx context.Context, imei string) (*dto.MdtDuty, error) {
	var d dto.MdtDuty
	var ps, pe time.Time
	var started sql.NullTime
	var rit, ton sql.NullFloat64
	var loading, unloading []byte
	err := database.RawQueryRowContext(ctx, r.db, `SELECT a.id, sh.id, sh.name, CASE WHEN sh.status = 'closed' THEN 'finishing' ELSE sh.status::text END, sh.planned_start, sh.planned_end, sh.started_at,
			r.id, r.name, ls.name, us.name, d.id, COALESCE(d.name, ''),
			(SELECT t.rit_per_shift FROM production_targets t WHERE t.route_id = r.id AND t.deleted_at IS NULL AND t.valid_from <= sh.date
				ORDER BY t.valid_from DESC LIMIT 1),
			(SELECT t.ton_per_shift FROM production_targets t WHERE t.route_id = r.id AND t.deleted_at IS NULL AND t.valid_from <= sh.date
				ORDER BY t.valid_from DESC LIMIT 1),
			`+mdtSiteJSON("ls")+`, `+mdtSiteJSON("us")+`
		FROM assignments a
		JOIN shifts sh ON sh.id = a.shift_id AND sh.deleted_at IS NULL
		JOIN routes r ON r.id = a.route_id
		JOIN sites ls ON ls.id = r.loading_site_id
		JOIN sites us ON us.id = COALESCE(a.unloading_site_id, r.unloading_site_id)
		JOIN users d ON d.id = a.driver_id
		WHERE a.imei = $1 AND a.end_at IS NULL AND (sh.status = 'active' OR (sh.status = 'planned' AND sh.planned_end > now())
			-- Shift ditutup tetapi ritase terakhir belum selesai (masih hauling / dumping): selesaikan dulu.
			OR (sh.status = 'closed' AND sh.closed_at > now() - interval '6 hours' AND `+FinishingTrip+`))
		ORDER BY (sh.status = 'active') DESC, (sh.status = 'closed') DESC, sh.planned_start
		LIMIT 1`, imei).Scan(&d.AssignmentID, &d.ShiftID, &d.ShiftName, &d.ShiftStatus, &ps, &pe, &started,
		&d.RouteID, &d.RouteName, &d.LoadingName, &d.UnloadingName, &d.DriverID, &d.DriverName, &rit, &ton, &loading, &unloading)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("duty: %w", err)
	}
	d.PlannedStart, d.PlannedEnd, d.StartedAt = database.Ms(ps), database.Ms(pe), database.NullMs(started)
	d.TargetRit, d.TargetTon = database.NullFloat(rit), database.NullFloat(ton)
	if err := json.Unmarshal(loading, &d.Loading); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(unloading, &d.Unloading); err != nil {
		return nil, err
	}
	return &d, nil
}

// mdtSiteJSON: site alias a as {id, name, lat, lng, radius, zones[]} so the tablet can show its
// position against front loading / dumping without signal.
func mdtSiteJSON(a string) string {
	return `json_build_object('id', ` + a + `.id, 'name', ` + a + `.name, 'lat', ` + a + `.lat, 'lng', ` + a + `.lng, 'radius', ` + a + `.radius_m,
		'zones', (SELECT COALESCE(json_agg(json_build_object('id', z.id, 'kind', z.kind, 'lat', z.lat, 'lng', z.lng, 'radius', z.radius_m)), '[]')
			FROM site_zones z WHERE z.site_id = ` + a + `.id AND z.deleted_at IS NULL))`
}
