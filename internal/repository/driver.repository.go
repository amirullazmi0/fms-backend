package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
)

// DriverRepository covers operators (users with user_type operator) and shift templates (the shift master data).
type DriverRepository interface {
	GetDrivers(ctx context.Context, q *dto.QueryGlobal) ([]dto.Driver, int, error)
	GetDriverByID(ctx context.Context, id string) (*dto.Driver, error)
	CreateDriver(ctx context.Context, body dto.UpsertDriverRequest, createdBy string) (string, error)
	UpdateDriver(ctx context.Context, id string, body dto.UpsertDriverRequest, updatedBy string) (bool, error)
	DriverEverAssigned(ctx context.Context, id string) (bool, error)
	DeactivateDriver(ctx context.Context, id, updatedBy string) (bool, error)
	DeleteDriver(ctx context.Context, id, deletedBy string) (bool, error)
	LastAssignments(ctx context.Context) (map[string]dto.DriverLast, error)

	GetTemplates(ctx context.Context) ([]dto.ShiftTemplate, error)
	GetTemplateByID(ctx context.Context, id string) (*dto.ShiftTemplate, error)
	CreateTemplate(ctx context.Context, body dto.UpsertShiftTemplateRequest, createdBy string) (string, error)
	UpdateTemplate(ctx context.Context, id string, body dto.UpsertShiftTemplateRequest, updatedBy string) (bool, error)
	DeleteTemplate(ctx context.Context, id, deletedBy string) (bool, error)
	// SeedTemplates inserts "Pagi" & "Sore" when there are no templates yet.
	SeedTemplates(ctx context.Context) error
}

type driverRepository struct {
	db *sql.DB
}

func NewDriverRepository(db *sql.DB) DriverRepository {
	return &driverRepository{db: db}
}

// Operator = user dengan user_type 'operator' (login MDT memakai NIK + PIN, bukan dashboard).
// JSON-nya tetap dto.Driver, supaya /api/drivers dan semua tampilan lama tidak berubah.
var driverColumns = []string{"d.id", "COALESCE(d.name, '')", "d.phone", "d.employee_no", "d.ibutton", "d.is_active", "d.note", "d.created_at", "d.updated_at",
	"d.pin_hash IS NOT NULL"}

// operators: users tipe operator yang belum dihapus.
func operators() *database.QueryBuilder {
	return database.NewQueryBuilder("users d").Select(driverColumns...).
		Where("d.user_type = 'operator'").
		WhereNull("d.deleted_at")
}

// operatorRole: role OPERATOR (dari migrasi), dipakai sebagai role_id operator baru.
const operatorRole = "(SELECT id FROM roles WHERE name = 'OPERATOR' AND deleted_at IS NULL LIMIT 1)"

func scanDriver(row interface{ Scan(...any) error }) (dto.Driver, error) {
	var d dto.Driver
	var phone, emp, ib, note sql.NullString
	var created, updated time.Time
	if err := row.Scan(&d.ID, &d.Name, &phone, &emp, &ib, &d.Active, &note, &created, &updated, &d.HasPin); err != nil {
		return d, err
	}
	d.Phone, d.EmployeeNo, d.Ibutton, d.Note = database.NullString(phone), database.NullString(emp), database.NullString(ib), database.NullString(note)
	d.CreatedAt, d.UpdatedAt = database.Ms(created), database.Ms(updated)
	return d, nil
}

func (r *driverRepository) GetDrivers(ctx context.Context, q *dto.QueryGlobal) ([]dto.Driver, int, error) {
	qb := operators()

	n := 0
	next := func() int { n++; return n }

	if q.Search != "" {
		p := next()
		qb.Where(fmt.Sprintf("(d.name ILIKE $%d OR d.employee_no ILIKE $%d OR d.phone ILIKE $%d)", p, p, p), "%"+q.Search+"%")
	}
	total, err := count(ctx, r.db, qb)
	if err != nil {
		return nil, 0, err
	}
	if q.SortBy == "" {
		qb.OrderBy("d.is_active DESC").OrderBy("d.name")
		if q.Paginated() {
			limit, offset := database.GetPagination(q.Page, q.Limit)
			qb.Limit(limit).Offset(offset)
		}
	} else {
		page(qb, q, map[string]string{"name": "d.name", "employee_no": "d.employee_no", "created_at": "d.created_at"}, "d.name", "ASC")
	}

	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.Driver{}
	for rows.Next() {
		d, err := scanDriver(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

func (r *driverRepository) GetDriverByID(ctx context.Context, id string) (*dto.Driver, error) {
	query, args := operators().Where("d.id = $1", id).Build()
	d, err := scanDriver(database.RawQueryRowContext(ctx, r.db, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func ibuttonOf(s *string) *string {
	v := noteOf(s)
	if v == nil {
		return nil
	}
	up := strings.ToUpper(*v)
	return &up
}

// enabledOf: a missing flag means true (active operator, auto-close template).
func enabledOf(b *bool) bool {
	return b == nil || *b
}

func (r *driverRepository) CreateDriver(ctx context.Context, body dto.UpsertDriverRequest, createdBy string) (string, error) {
	var id string
	err := database.NewInsertBuilder("users").
		Set("user_type", "operator").
		SetRaw("role_id", operatorRole).
		Set("name", strings.TrimSpace(body.Name)).
		Set("phone", noteOf(body.Phone)).
		Set("employee_no", noteOf(body.EmployeeNo)).
		Set("ibutton", ibuttonOf(body.Ibutton)).
		Set("is_active", enabledOf(body.Active)).
		Set("note", noteOf(body.Note)).
		SetCreatedBy(createdBy).
		QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *driverRepository) UpdateDriver(ctx context.Context, id string, body dto.UpsertDriverRequest, updatedBy string) (bool, error) {
	res, err := database.NewUpdateBuilder("users").
		Set("name", strings.TrimSpace(body.Name)).
		Set("phone", noteOf(body.Phone)).
		Set("employee_no", noteOf(body.EmployeeNo)).
		Set("ibutton", ibuttonOf(body.Ibutton)).
		Set("is_active", enabledOf(body.Active)).
		Set("note", noteOf(body.Note)).
		SetUpdatedBy(updatedBy).
		Where("id = $1", id).
		Where("user_type = 'operator'").
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *driverRepository) DriverEverAssigned(ctx context.Context, id string) (bool, error) {
	n, err := count(ctx, r.db, database.NewQueryBuilder("assignments").Where("driver_id = $1", id))
	return n > 0, err
}

func (r *driverRepository) DeactivateDriver(ctx context.Context, id, updatedBy string) (bool, error) {
	res, err := database.NewUpdateBuilder("users").
		Set("is_active", false).
		SetUpdatedBy(updatedBy).
		Where("id = $1", id).
		Where("user_type = 'operator'").
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *driverRepository) DeleteDriver(ctx context.Context, id, deletedBy string) (bool, error) {
	res, err := database.NewDeleteBuilder("users").
		SetDeletedBy(deletedBy).
		Where("id = $1", id).
		Where("user_type = 'operator'").
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *driverRepository) LastAssignments(ctx context.Context) (map[string]dto.DriverLast, error) {
	rows, err := database.NewQueryBuilder("assignments a").
		Select("DISTINCT ON (a.driver_id) a.driver_id", "a.imei", "a.route_id").
		InnerJoin("shifts s", "s.id = a.shift_id AND s.deleted_at IS NULL").
		OrderBy("a.driver_id").
		OrderBy("s.planned_start DESC").
		OrderBy("COALESCE(a.start_at, a.created_at) DESC").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]dto.DriverLast{}
	for rows.Next() {
		var driverID string
		var last dto.DriverLast
		if err := rows.Scan(&driverID, &last.Imei, &last.RouteID); err != nil {
			return nil, err
		}
		out[driverID] = last
	}
	return out, rows.Err()
}

// ---------- Shift templates ----------

var templateColumns = []string{"id", "name", "to_char(start_time, 'HH24:MI')", "to_char(end_time, 'HH24:MI')", "auto_close", "sort"}

func scanTemplate(row interface{ Scan(...any) error }) (dto.ShiftTemplate, error) {
	var t dto.ShiftTemplate
	err := row.Scan(&t.ID, &t.Name, &t.StartTime, &t.EndTime, &t.AutoClose, &t.Sort)
	return t, err
}

func (r *driverRepository) GetTemplates(ctx context.Context) ([]dto.ShiftTemplate, error) {
	rows, err := database.NewQueryBuilder("shift_templates").Select(templateColumns...).
		WhereNull("deleted_at").
		OrderBy("sort").
		OrderBy("start_time").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.ShiftTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *driverRepository) GetTemplateByID(ctx context.Context, id string) (*dto.ShiftTemplate, error) {
	query, args := database.NewQueryBuilder("shift_templates").Select(templateColumns...).
		Where("id = $1", id).
		WhereNull("deleted_at").
		Build()
	t, err := scanTemplate(database.RawQueryRowContext(ctx, r.db, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *driverRepository) CreateTemplate(ctx context.Context, body dto.UpsertShiftTemplateRequest, createdBy string) (string, error) {
	var id string
	err := database.NewInsertBuilder("shift_templates").
		Set("name", strings.TrimSpace(body.Name)).
		Set("start_time", body.StartTime).
		Set("end_time", body.EndTime).
		Set("auto_close", enabledOf(body.AutoClose)).
		Set("sort", body.Sort).
		SetCreatedBy(createdBy).
		QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *driverRepository) UpdateTemplate(ctx context.Context, id string, body dto.UpsertShiftTemplateRequest, updatedBy string) (bool, error) {
	res, err := database.NewUpdateBuilder("shift_templates").
		Set("name", strings.TrimSpace(body.Name)).
		Set("start_time", body.StartTime).
		Set("end_time", body.EndTime).
		Set("auto_close", enabledOf(body.AutoClose)).
		Set("sort", body.Sort).
		SetUpdatedBy(updatedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *driverRepository) DeleteTemplate(ctx context.Context, id, deletedBy string) (bool, error) {
	res, err := database.NewDeleteBuilder("shift_templates").
		SetDeletedBy(deletedBy).
		Where("id = $1", id).
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *driverRepository) SeedTemplates(ctx context.Context) error {
	n, err := count(ctx, r.db, database.NewQueryBuilder("shift_templates").WhereNull("deleted_at"))
	if err != nil || n > 0 {
		return err
	}
	for _, t := range []struct {
		name, start, end string
		sort             int
	}{{"Pagi", "06:00", "14:00", 1}, {"Sore", "14:00", "22:00", 2}} {
		if _, err := database.NewInsertBuilder("shift_templates").
			Set("name", t.name).
			Set("start_time", t.start).
			Set("end_time", t.end).
			Set("auto_close", true).
			Set("sort", t.sort).
			WithoutReturning().
			ExecContext(ctx, r.db); err != nil {
			return err
		}
	}
	return nil
}
