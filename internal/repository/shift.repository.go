package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/lib/pq"
)

// ShiftRepository covers work shifts and their driver–truck–route assignments.
type ShiftRepository interface {
	// ListShifts: shifts with a work date in [fromDate, toDate], plus every active shift.
	ListShifts(ctx context.Context, fromDate, toDate string) ([]dto.Shift, error)
	GetShift(ctx context.Context, id string) (*dto.Shift, error)
	ActiveShifts(ctx context.Context) ([]dto.Shift, error)
	// RecentlyClosedShifts: a ritase that departed before closing may arrive later.
	RecentlyClosedShifts(ctx context.Context, withinMs int64) ([]dto.Shift, error)
	// PreviousShift: the latest earlier shift on the same route that has assignments.
	PreviousShift(ctx context.Context, id string) (*dto.Shift, error)
	CreateShift(ctx context.Context, s dto.ShiftInput, createdBy string) (string, error)
	UpdateShift(ctx context.Context, id string, s dto.ShiftInput, routeChanged bool, updatedBy string) error
	StartShift(ctx context.Context, id string, at int64) (bool, error)
	CloseShift(ctx context.Context, id, reason string, closedBy *string, at int64) (bool, error)
	ReopenShift(ctx context.Context, id string) (bool, error)
	// DeleteShift soft-deletes a shift that has not started.
	DeleteShift(ctx context.Context, id, deletedBy string) (bool, error)
	MarkOverdueNoted(ctx context.Context, id string) error
	DueShifts(ctx context.Context, now int64) (toStart, toClose, overdue []dto.DueShift, err error)

	// ClashOtherShift: driver/truck still on duty in another non-closed shift whose hours overlap.
	ClashOtherShift(ctx context.Context, shiftID, driverID, imei string, from, end int64) (string, error)
	InsertAssignment(ctx context.Context, shiftID, driverID, imei, routeID string, note *string, startAt *int64) (*dto.Assignment, error)
	GetAssignment(ctx context.Context, id string) (*dto.Assignment, error)
	UpdateAssignment(ctx context.Context, id, driverID, imei, routeID string, note *string) (*dto.Assignment, error)
	EndAssignment(ctx context.Context, id string, at int64) error
	// AssignmentState: shift status, end, and stored trip count of an assignment.
	AssignmentState(ctx context.Context, id string) (state *AssignmentState, err error)
	DeleteAssignment(ctx context.Context, id string) error
}

type AssignmentState struct {
	ShiftID string
	Status  string
	EndAt   *int64
	Trips   int
}

type shiftRepository struct {
	db *sql.DB
}

func NewShiftRepository(db *sql.DB) ShiftRepository {
	return &shiftRepository{db: db}
}

var shiftColumns = []string{
	"s.id", "s.name", "s.route_id", "to_char(s.date, 'YYYY-MM-DD')", "s.planned_start", "s.planned_end", "s.auto_close", "s.status::text",
	"s.started_at", "s.closed_at", "s.close_reason::text", "s.overdue_noted", "s.note", "s.created_at", "s.updated_at",
}

func scanShift(row interface{ Scan(...any) error }, now int64) (dto.Shift, error) {
	var s dto.Shift
	var route, reason, note sql.NullString
	var ps, pe, created, updated time.Time
	var started, closed sql.NullTime
	if err := row.Scan(&s.ID, &s.Name, &route, &s.Date, &ps, &pe, &s.AutoClose, &s.Status, &started, &closed, &reason, &s.OverdueNoted, &note, &created, &updated); err != nil {
		return s, err
	}
	s.RouteID, s.CloseReason, s.Note = database.NullString(route), database.NullString(reason), database.NullString(note)
	s.PlannedStart, s.PlannedEnd = database.Ms(ps), database.Ms(pe)
	s.StartedAt, s.ClosedAt = database.NullMs(started), database.NullMs(closed)
	s.CreatedAt, s.UpdatedAt = database.Ms(created), database.Ms(updated)
	s.Overdue = s.Status == "active" && now > s.PlannedEnd
	s.Assignments = []dto.Assignment{}
	return s, nil
}

var assignmentColumns = []string{"id", "shift_id", "driver_id", "imei", "route_id", "note", "start_at", "end_at", "created_at"}

func scanAssignment(row interface{ Scan(...any) error }) (dto.Assignment, error) {
	var a dto.Assignment
	var note sql.NullString
	var start, end sql.NullTime
	var created time.Time
	if err := row.Scan(&a.ID, &a.ShiftID, &a.DriverID, &a.Imei, &a.RouteID, &note, &start, &end, &created); err != nil {
		return a, err
	}
	a.Note = database.NullString(note)
	a.StartAt, a.EndAt = database.NullMs(start), database.NullMs(end)
	a.CreatedAt = database.Ms(created)
	return a, nil
}

func shifts() *database.QueryBuilder {
	return database.NewQueryBuilder("shifts s").Select(shiftColumns...).WhereNull("s.deleted_at")
}

// queryShifts runs a shift query and attaches each shift's assignments.
func (r *shiftRepository) queryShifts(ctx context.Context, qb *database.QueryBuilder) ([]dto.Shift, error) {
	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	now := database.NowMs()
	list := []dto.Shift{}
	for rows.Next() {
		s, err := scanShift(rows, now)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		list = append(list, s)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil || len(list) == 0 {
		return list, err
	}

	ids := make([]string, len(list))
	for i, s := range list {
		ids[i] = s.ID
	}
	arows, err := database.NewQueryBuilder("assignments").Select(assignmentColumns...).
		Where("shift_id = ANY($1::uuid[])", pq.Array(ids)).
		OrderBy("COALESCE(start_at, created_at)").
		OrderBy("created_at").
		ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = arows.Close() }()
	by := map[string][]dto.Assignment{}
	for arows.Next() {
		a, err := scanAssignment(arows)
		if err != nil {
			return nil, err
		}
		by[a.ShiftID] = append(by[a.ShiftID], a)
	}
	for i := range list {
		if a, ok := by[list[i].ID]; ok {
			list[i].Assignments = a
		}
	}
	return list, arows.Err()
}

func (r *shiftRepository) first(ctx context.Context, qb *database.QueryBuilder) (*dto.Shift, error) {
	list, err := r.queryShifts(ctx, qb)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func (r *shiftRepository) ListShifts(ctx context.Context, fromDate, toDate string) ([]dto.Shift, error) {
	return r.queryShifts(ctx, shifts().
		Where("((s.date BETWEEN $1 AND $2) OR s.status = 'active')", fromDate, toDate).
		OrderBy("s.date").
		OrderBy("s.planned_start"))
}

func (r *shiftRepository) GetShift(ctx context.Context, id string) (*dto.Shift, error) {
	return r.first(ctx, shifts().Where("s.id = $1", id))
}

func (r *shiftRepository) ActiveShifts(ctx context.Context) ([]dto.Shift, error) {
	return r.queryShifts(ctx, shifts().Where("s.status = 'active'").OrderBy("s.planned_start"))
}

func (r *shiftRepository) RecentlyClosedShifts(ctx context.Context, withinMs int64) ([]dto.Shift, error) {
	return r.queryShifts(ctx, shifts().
		Where("s.status = 'closed'").
		Where("s.closed_at > $1", database.FromMs(database.NowMs()-withinMs)))
}

func (r *shiftRepository) PreviousShift(ctx context.Context, id string) (*dto.Shift, error) {
	return r.first(ctx, shifts().
		InnerJoin("shifts c", "c.id = $1 AND s.id <> c.id AND s.planned_start < c.planned_start AND s.route_id IS NOT DISTINCT FROM c.route_id").
		Where("EXISTS (SELECT 1 FROM assignments a WHERE a.shift_id = s.id)", id).
		OrderBy("s.planned_start DESC").
		Limit(1))
}

func (r *shiftRepository) CreateShift(ctx context.Context, s dto.ShiftInput, createdBy string) (string, error) {
	var id string
	err := database.NewInsertBuilder("shifts").
		Set("name", s.Name).
		Set("date", s.Date).
		Set("planned_start", database.FromMs(s.PlannedStart)).
		Set("planned_end", database.FromMs(s.PlannedEnd)).
		Set("auto_close", s.AutoClose).
		Set("note", s.Note).
		Set("route_id", s.RouteID).
		SetCreatedBy(createdBy).
		QueryRowContext(ctx, r.db).Scan(&id)
	return id, err
}

func (r *shiftRepository) UpdateShift(ctx context.Context, id string, s dto.ShiftInput, routeChanged bool, updatedBy string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ub := database.NewUpdateBuilder("shifts").
		Set("name", s.Name).
		Set("date", s.Date).
		Set("planned_start", database.FromMs(s.PlannedStart)).
		Set("planned_end", database.FromMs(s.PlannedEnd)).
		Set("auto_close", s.AutoClose).
		Set("note", s.Note).
		Set("route_id", s.RouteID).
		// Moving the planned end (extending) lets the "overdue" warning fire again later.
		SetExpr("overdue_noted", "CASE WHEN $1 <> planned_end THEN false ELSE overdue_noted END", database.FromMs(s.PlannedEnd)).
		Where("id = $1", id).
		Where("deleted_at IS NULL")
	if updatedBy != "" {
		ub.SetUpdatedBy(updatedBy)
	}
	if _, err := ub.ExecuteContext(ctx, tx); err != nil {
		return err
	}
	// Assignments follow the shift's route.
	if routeChanged && s.RouteID != nil {
		if _, err := database.NewUpdateBuilder("assignments").
			Set("route_id", *s.RouteID).
			Where("shift_id = $1", id).
			ExecuteContext(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *shiftRepository) StartShift(ctx context.Context, id string, at int64) (bool, error) {
	res, err := database.NewUpdateBuilder("shifts").
		SetRaw("status", "'active'").
		Set("started_at", database.FromMs(at)).
		SetRaw("updated_at", "CURRENT_TIMESTAMP").
		Where("id = $1", id).
		Where("status = 'planned'").
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *shiftRepository) CloseShift(ctx context.Context, id, reason string, closedBy *string, at int64) (bool, error) {
	res, err := database.NewUpdateBuilder("shifts").
		SetRaw("status", "'closed'").
		Set("closed_at", database.FromMs(at)).
		Set("close_reason", reason).
		Set("closed_by", closedBy).
		SetRaw("updated_at", "CURRENT_TIMESTAMP").
		Where("id = $1", id).
		Where("status = 'active'").
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *shiftRepository) ReopenShift(ctx context.Context, id string) (bool, error) {
	res, err := database.NewUpdateBuilder("shifts").
		SetRaw("status", "'active'").
		SetRaw("closed_at", "NULL").
		SetRaw("close_reason", "NULL").
		SetRaw("closed_by", "NULL").
		SetRaw("overdue_noted", "false").
		SetRaw("updated_at", "CURRENT_TIMESTAMP").
		Where("id = $1", id).
		Where("status = 'closed'").
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *shiftRepository) DeleteShift(ctx context.Context, id, deletedBy string) (bool, error) {
	res, err := database.NewDeleteBuilder("shifts").
		SetDeletedBy(deletedBy).
		Where("id = $1", id).
		Where("status = 'planned'").
		Where("deleted_at IS NULL").
		ExecuteContext(ctx, r.db)
	return affected(res, err)
}

func (r *shiftRepository) MarkOverdueNoted(ctx context.Context, id string) error {
	_, err := database.NewUpdateBuilder("shifts").
		SetRaw("overdue_noted", "true").
		Where("id = $1", id).
		ExecuteContext(ctx, r.db)
	return err
}

func (r *shiftRepository) dueQuery(ctx context.Context, qb *database.QueryBuilder) ([]dto.DueShift, error) {
	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.DueShift{}
	for rows.Next() {
		var d dto.DueShift
		var ps, pe time.Time
		if err := rows.Scan(&d.ID, &d.Name, &d.Date, &ps, &pe); err != nil {
			return nil, err
		}
		d.PlannedStart, d.PlannedEnd = database.Ms(ps), database.Ms(pe)
		out = append(out, d)
	}
	return out, rows.Err()
}

func due() *database.QueryBuilder {
	return database.NewQueryBuilder("shifts").
		Select("id", "name", "to_char(date, 'YYYY-MM-DD')", "planned_start", "planned_end").
		WhereNull("deleted_at")
}

func (r *shiftRepository) DueShifts(ctx context.Context, now int64) (toStart, toClose, overdue []dto.DueShift, err error) {
	t := database.FromMs(now)
	if toStart, err = r.dueQuery(ctx, due().Where("status = 'planned' AND planned_start <= $1 AND planned_end > $1", t)); err != nil {
		return
	}
	if toClose, err = r.dueQuery(ctx, due().Where("status = 'active' AND auto_close AND planned_end <= $1", t)); err != nil {
		return
	}
	overdue, err = r.dueQuery(ctx, due().Where("status = 'active' AND NOT auto_close AND planned_end <= $1 AND NOT overdue_noted", t))
	return
}

func (r *shiftRepository) ClashOtherShift(ctx context.Context, shiftID, driverID, imei string, from, end int64) (string, error) {
	query, args := database.NewQueryBuilder("assignments a").
		Select("s.name", "to_char(s.date, 'YYYY-MM-DD')", "a.driver_id = $2", "r.name").
		InnerJoin("shifts s", "s.id = a.shift_id AND s.deleted_at IS NULL").
		LeftJoin("routes r", "r.id = s.route_id").
		Where("a.shift_id <> $1", shiftID).
		Where("s.status <> 'closed'").
		Where("a.end_at IS NULL").
		Where("(a.driver_id = $2 OR a.imei = $3)", driverID, imei).
		Where("s.planned_start < $4", database.FromMs(end)).
		Where("GREATEST(s.planned_end, CASE WHEN s.status = 'active' THEN now() ELSE s.planned_end END) > $5", database.FromMs(from)).
		Limit(1).
		Build()
	var shift, date string
	var sameDriver bool
	var route sql.NullString
	err := database.RawQueryRowContext(ctx, r.db, query, args...).Scan(&shift, &date, &sameDriver, &route)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	who := "Truk"
	if sameDriver {
		who = "Driver"
	}
	on := ""
	if route.Valid {
		on = " · " + route.String
	}
	return fmt.Sprintf("%s ini udah ada di shift %s%s (%s) yang jamnya bentrok", who, shift, on, date), nil
}

func (r *shiftRepository) InsertAssignment(ctx context.Context, shiftID, driverID, imei, routeID string, note *string, startAt *int64) (*dto.Assignment, error) {
	a, err := scanAssignment(database.NewInsertBuilder("assignments").
		Set("shift_id", shiftID).
		Set("driver_id", driverID).
		Set("imei", imei).
		Set("route_id", routeID).
		Set("note", note).
		Set("start_at", database.FromMsPtr(startAt)).
		Returning(assignmentColumns...).
		QueryRowContext(ctx, r.db))
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *shiftRepository) GetAssignment(ctx context.Context, id string) (*dto.Assignment, error) {
	query, args := database.NewQueryBuilder("assignments").Select(assignmentColumns...).Where("id = $1", id).Build()
	a, err := scanAssignment(database.RawQueryRowContext(ctx, r.db, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *shiftRepository) UpdateAssignment(ctx context.Context, id, driverID, imei, routeID string, note *string) (*dto.Assignment, error) {
	query, args := database.NewUpdateBuilder("assignments").
		Set("driver_id", driverID).
		Set("imei", imei).
		Set("route_id", routeID).
		Set("note", note).
		Where("id = $1", id).
		Returning(assignmentColumns...).
		Build()
	a, err := scanAssignment(database.RawQueryRowContext(ctx, r.db, query, args...))
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *shiftRepository) EndAssignment(ctx context.Context, id string, at int64) error {
	_, err := database.NewUpdateBuilder("assignments").
		Set("end_at", database.FromMs(at)).
		Where("id = $1", id).
		ExecuteContext(ctx, r.db)
	return err
}

func (r *shiftRepository) AssignmentState(ctx context.Context, id string) (*AssignmentState, error) {
	query, args := database.NewQueryBuilder("assignments a").
		Select("a.shift_id", "s.status::text", "a.end_at", "(SELECT COUNT(*)::int FROM trips t WHERE t.assignment_id = a.id)").
		InnerJoin("shifts s", "s.id = a.shift_id").
		Where("a.id = $1", id).
		Build()
	var st AssignmentState
	var end sql.NullTime
	err := database.RawQueryRowContext(ctx, r.db, query, args...).Scan(&st.ShiftID, &st.Status, &end, &st.Trips)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st.EndAt = database.NullMs(end)
	return &st, nil
}

func (r *shiftRepository) DeleteAssignment(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := database.NewDeleteBuilder("trips").HardDelete().Where("assignment_id = $1", id).ExecuteContext(ctx, tx); err != nil {
		return err
	}
	if _, err := database.NewDeleteBuilder("assignments").HardDelete().Where("id = $1", id).ExecuteContext(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
