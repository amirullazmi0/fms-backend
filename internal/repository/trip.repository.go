package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/lib/pq"
)

// TripRepository stores computed ritase (trips) and aggregates them into reports.
type TripRepository interface {
	// ReplaceTrips stores the ritase of one assignment (drops the ones no longer found).
	// Returns how many rows changed.
	ReplaceTrips(ctx context.Context, assignmentID string, trips []dto.TripUpsert) (int, error)
	ListTrips(ctx context.Context, f dto.TripFilter, limit, offset int) ([]dto.TripRecord, int, error)
	Report(ctx context.Context, group string, f dto.TripFilter, tz string) (*dto.Report, error)
}

type tripRepository struct {
	db *sql.DB
}

func NewTripRepository(db *sql.DB) TripRepository {
	return &tripRepository{db: db}
}

var tripCols = []string{
	"assignment_id", "shift_id", "driver_id", "imei", "route_id", "route_name", "loading_site_id", "loading_name",
	"unloading_site_id", "unloading_name", "depart_at", "arrive_at", "duration_ms", "distance_m", "moving_ms", "stops",
	"stopped_ms", "max_speed", "avg_speed", "fuel_unit", "fuel_at_loading", "fuel_at_unloading", "fuel_used",
	"load_dwell_ms", "unload_dwell_ms", "payload_ton", "status", "leg",
}

func (r *tripRepository) ReplaceTrips(ctx context.Context, assignmentID string, trips []dto.TripUpsert) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	keep := make([]time.Time, len(trips))
	for i, t := range trips {
		keep[i] = database.FromMs(t.DepartAt)
	}
	res, err := database.NewDeleteBuilder("trips").HardDelete().
		Where("assignment_id = $1", assignmentID).
		Where("NOT (depart_at = ANY($1::timestamptz[]))", pq.Array(keep)).
		ExecuteContext(ctx, tx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	changed := int(n)

	sets := []string{}
	for _, c := range tripCols {
		if c != "imei" && c != "depart_at" {
			sets = append(sets, fmt.Sprintf("%s = EXCLUDED.%s", c, c))
		}
	}
	upsert := " ON CONFLICT (imei, depart_at) DO UPDATE SET " + strings.Join(sets, ", ") + `, computed_at = CURRENT_TIMESTAMP
		WHERE trips.status IS DISTINCT FROM EXCLUDED.status OR trips.arrive_at IS DISTINCT FROM EXCLUDED.arrive_at
		   OR trips.duration_ms IS DISTINCT FROM EXCLUDED.duration_ms OR trips.assignment_id IS DISTINCT FROM EXCLUDED.assignment_id
		   OR trips.driver_id IS DISTINCT FROM EXCLUDED.driver_id OR trips.fuel_used IS DISTINCT FROM EXCLUDED.fuel_used
		   OR trips.payload_ton IS DISTINCT FROM EXCLUDED.payload_ton`

	for _, t := range trips {
		var unloadDwell any
		if t.UnloadDwellMs != nil {
			unloadDwell = *t.UnloadDwellMs
		}
		query, args := database.NewInsertBuilder("trips").
			Set("assignment_id", t.AssignmentID).
			Set("shift_id", t.ShiftID).
			Set("driver_id", t.DriverID).
			Set("imei", t.Imei).
			Set("route_id", t.RouteID).
			Set("route_name", t.RouteName).
			Set("loading_site_id", t.LoadingSiteID).
			Set("loading_name", t.LoadingName).
			Set("unloading_site_id", t.UnloadingSiteID).
			Set("unloading_name", t.UnloadingName).
			Set("depart_at", database.FromMs(t.DepartAt)).
			Set("arrive_at", database.FromMsPtr(t.ArriveAt)).
			Set("duration_ms", t.DurationMs).
			Set("distance_m", t.Distance).
			Set("moving_ms", t.MovingMs).
			Set("stops", t.Stops).
			Set("stopped_ms", t.StoppedMs).
			Set("max_speed", t.MaxSpeed).
			Set("avg_speed", t.AvgSpeed).
			Set("fuel_unit", t.FuelUnit).
			Set("fuel_at_loading", t.FuelAtLoading).
			Set("fuel_at_unloading", t.FuelAtUnloading).
			Set("fuel_used", t.FuelUsed).
			Set("load_dwell_ms", t.LoadDwellMs).
			Set("unload_dwell_ms", unloadDwell).
			Set("payload_ton", t.PayloadTon).
			Set("status", t.Status).
			Set("leg", t.Leg).
			WithoutReturning().
			Build()
		res, err := database.RawExecContext(ctx, tx, query+upsert, args...)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		changed += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

// fromTrips is trips joined with their driver, shift and truck names.
func fromTrips() *database.QueryBuilder {
	return database.NewQueryBuilder("trips t").
		LeftJoin("users d", "d.id = t.driver_id").
		LeftJoin("shifts s", "s.id = t.shift_id").
		LeftJoin("devices v", "v.imei = t.imei")
}

// filter adds the trip filter to qb.
func filter(qb *database.QueryBuilder, f dto.TripFilter) {
	n := 0
	next := func() int { n++; return n }
	qb.Where(fmt.Sprintf("t.depart_at >= $%d", next()), database.FromMs(f.From))
	qb.Where(fmt.Sprintf("t.depart_at <= $%d", next()), database.FromMs(f.To))
	if f.RouteID != "" {
		qb.Where(fmt.Sprintf("t.route_id = $%d", next()), f.RouteID)
	}
	if f.DriverID != "" {
		qb.Where(fmt.Sprintf("t.driver_id = $%d", next()), f.DriverID)
	}
	if f.Imei != "" {
		qb.Where(fmt.Sprintf("t.imei = $%d", next()), f.Imei)
	}
	if f.ShiftID != "" {
		qb.Where(fmt.Sprintf("t.shift_id = $%d", next()), f.ShiftID)
	}
}

var tripRecordColumns = []string{
	"t.id", "t.assignment_id", "t.shift_id", "s.name", "to_char(s.date, 'YYYY-MM-DD')", "t.driver_id", "d.name", "t.imei",
	"COALESCE(v.plate, v.name, t.imei)", "t.route_id", "t.route_name", "t.leg::text", "t.loading_name", "t.unloading_name",
	"t.depart_at", "t.arrive_at", "t.duration_ms", "t.distance_m", "t.moving_ms", "t.stops", "t.stopped_ms", "t.max_speed",
	"t.avg_speed", "t.fuel_unit", "t.fuel_at_loading", "t.fuel_at_unloading", "t.fuel_used", "t.load_dwell_ms",
	"t.unload_dwell_ms", "t.payload_ton", "t.status::text",
}

func (r *tripRepository) ListTrips(ctx context.Context, f dto.TripFilter, limit, offset int) ([]dto.TripRecord, int, error) {
	qb := fromTrips().Select(tripRecordColumns...)
	filter(qb, f)
	total, err := count(ctx, r.db, qb)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	rows, err := qb.OrderBy("t.depart_at DESC").Limit(limit).Offset(max(0, offset)).ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := []dto.TripRecord{}
	for rows.Next() {
		var t dto.TripRecord
		var assignment, shift, shiftName, shiftDate, driver, driverName, truck, route, fuelUnit sql.NullString
		var depart time.Time
		var arrive sql.NullTime
		var fuelLoad, fuelUnload, fuelUsed, payload sql.NullFloat64
		var unloadDwell sql.NullInt64
		if err := rows.Scan(&t.ID, &assignment, &shift, &shiftName, &shiftDate, &driver, &driverName, &t.Imei, &truck, &route,
			&t.RouteName, &t.Leg, &t.LoadingName, &t.UnloadingName, &depart, &arrive, &t.DurationMs, &t.Distance, &t.MovingMs,
			&t.Stops, &t.StoppedMs, &t.MaxSpeed, &t.AvgSpeed, &fuelUnit, &fuelLoad, &fuelUnload, &fuelUsed, &t.LoadDwellMs,
			&unloadDwell, &payload, &t.Status); err != nil {
			return nil, 0, err
		}
		t.AssignmentID, t.ShiftID, t.ShiftName, t.ShiftDate = database.NullString(assignment), database.NullString(shift), database.NullString(shiftName), database.NullString(shiftDate)
		t.DriverID, t.DriverName, t.Truck, t.RouteID, t.FuelUnit = database.NullString(driver), database.NullString(driverName), database.NullString(truck), database.NullString(route), database.NullString(fuelUnit)
		t.DepartAt, t.ArriveAt = database.Ms(depart), database.NullMs(arrive)
		t.FuelAtLoading, t.FuelAtUnloading, t.FuelUsed = database.NullFloat(fuelLoad), database.NullFloat(fuelUnload), database.NullFloat(fuelUsed)
		t.UnloadDwellMs = database.NullInt(unloadDwell)
		t.PayloadTon = database.NullFloat(payload)
		out = append(out, t)
	}
	return out, total, rows.Err()
}

type groupSQL struct {
	key, label, sub, order string
}

// $TZ is replaced with the report time zone (per-day grouping only).
var reportGroups = map[string]groupSQL{
	"route":  {"COALESCE(t.route_id::text, t.route_name)", "MAX(t.route_name)", "MAX(t.loading_name || ' → ' || t.unloading_name)", "label"},
	"driver": {"COALESCE(t.driver_id::text, '-')", "COALESCE(MAX(d.name), 'Tanpa operator')", "MAX(d.employee_no)", "label"},
	"truck":  {"t.imei", "MAX(COALESCE(v.plate, v.name, t.imei))", "MAX(v.vehicle)", "label"},
	"shift":  {"COALESCE(t.shift_id::text, '-')", "COALESCE(MAX(s.name), 'Tanpa shift')", "MAX(to_char(s.date, 'YYYY-MM-DD'))", "sub DESC, label"},
	"day":    {"to_char(t.depart_at AT TIME ZONE $TZ, 'YYYY-MM-DD')", "to_char(MIN(t.depart_at) AT TIME ZONE $TZ, 'YYYY-MM-DD')", "NULL", "label DESC"},
	// Per jam (waktu GPS berangkat dari front loading), key = "YYYY-MM-DD HH".
	"hour": {"to_char(t.depart_at AT TIME ZONE $TZ, 'YYYY-MM-DD HH24')", "to_char(MIN(t.depart_at) AT TIME ZONE $TZ, 'HH24:00')", "to_char(MIN(t.depart_at) AT TIME ZONE $TZ, 'YYYY-MM-DD')", "key"},
}

// Ritase & duration = the way out (go). The way back is counted apart. Distance, stops, total fuel = both ways.
const (
	legGo   = "t.leg = 'go'"
	legBack = "t.leg = 'back'"
)

var aggColumns = []string{
	"COUNT(*) FILTER (WHERE " + legGo + ")::int AS trips",
	"COUNT(*) FILTER (WHERE " + legGo + " AND t.status = 'done')::int AS done",
	"COALESCE(AVG(t.duration_ms) FILTER (WHERE " + legGo + " AND t.status = 'done'), 0)::float",
	"COALESCE(MIN(t.duration_ms) FILTER (WHERE " + legGo + " AND t.status = 'done'), 0)::float",
	"COALESCE(MAX(t.duration_ms) FILTER (WHERE " + legGo + " AND t.status = 'done'), 0)::float",
	"COUNT(*) FILTER (WHERE " + legBack + " AND t.status = 'done')::int",
	"COALESCE(AVG(t.duration_ms) FILTER (WHERE " + legBack + " AND t.status = 'done'), 0)::float",
	"COALESCE(MIN(t.duration_ms) FILTER (WHERE " + legBack + " AND t.status = 'done'), 0)::float",
	"COALESCE(MAX(t.duration_ms) FILTER (WHERE " + legBack + " AND t.status = 'done'), 0)::float",
	"COALESCE(SUM(t.distance_m), 0)::float",
	"COALESCE(SUM(t.stops), 0)::int",
	"COALESCE(SUM(t.stopped_ms), 0)::float",
	"MAX(t.fuel_unit)",
	"SUM(t.fuel_used)::float",
	"AVG(t.fuel_used) FILTER (WHERE " + legGo + " AND t.status = 'done')::float",
	"AVG(t.fuel_at_loading) FILTER (WHERE " + legGo + " AND t.status = 'done')::float",
	"AVG(t.fuel_at_unloading) FILTER (WHERE " + legGo + " AND t.status = 'done')::float",
	// Tonase: ritase yang tiba di dumping × truck factor (trip lama tanpa snapshot pakai payload unit sekarang).
	"COALESCE(SUM(COALESCE(t.payload_ton, v.payload_ton)) FILTER (WHERE " + legGo + " AND t.status = 'done'), 0)::float",
	// Waktu siklus: dua arah + waktu di loading/unloading (ritase selesai).
	"COALESCE(SUM(t.duration_ms + t.load_dwell_ms) FILTER (WHERE t.status = 'done'), 0)::float",
}

func r1(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	v := math.Floor(f.Float64*10+0.5) / 10
	return &v
}

func scanReportRow(row interface{ Scan(...any) error }) (dto.ReportRow, error) {
	var x dto.ReportRow
	var sub, fuelUnit sql.NullString
	var fuelUsed, avgFuel, avgLoad, avgUnload sql.NullFloat64
	if err := row.Scan(&x.Key, &x.Label, &sub, &x.Trips, &x.Done, &x.AvgDurationMs, &x.MinDurationMs, &x.MaxDurationMs,
		&x.BackDone, &x.BackAvgMs, &x.BackMinMs, &x.BackMaxMs, &x.Distance, &x.Stops, &x.StoppedMs,
		&fuelUnit, &fuelUsed, &avgFuel, &avgLoad, &avgUnload, &x.Ton, &x.CycleMs); err != nil {
		return x, err
	}
	if x.CycleMs > 0 {
		x.TonPerHour = math.Floor(x.Ton/(x.CycleMs/3_600_000)*10+0.5) / 10
	}
	x.Sub, x.FuelUnit = database.NullString(sub), database.NullString(fuelUnit)
	x.FuelUsed, x.AvgFuel, x.AvgFuelAtLoading, x.AvgFuelAtUnloading = r1(fuelUsed), r1(avgFuel), r1(avgLoad), r1(avgUnload)
	return x, nil
}

func (r *tripRepository) Report(ctx context.Context, group string, f dto.TripFilter, tz string) (*dto.Report, error) {
	g, ok := reportGroups[group]
	if !ok {
		group, g = "route", reportGroups["route"]
	}

	qb := fromTrips()
	filter(qb, f)
	// $TZ (per-day grouping) is the configured zone, validated at startup by time.LoadLocation.
	zone := pq.QuoteLiteral(tz)
	sub := func(s string) string { return strings.ReplaceAll(s, "$TZ", zone) }
	qb.Select(append([]string{sub(g.key) + " AS key", sub(g.label) + " AS label", sub(g.sub) + " AS sub"}, aggColumns...)...).
		GroupBy(sub(g.key)).
		OrderBy(g.order)

	rows, err := qb.ExecuteContext(ctx, r.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	report := &dto.Report{From: f.From, To: f.To, Group: group, Rows: []dto.ReportRow{}}
	for rows.Next() {
		x, err := scanReportRow(rows)
		if err != nil {
			return nil, err
		}
		report.Rows = append(report.Rows, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	totalQB := fromTrips().Select(append([]string{"'total'", "'Total'", "NULL"}, aggColumns...)...)
	filter(totalQB, f)
	query, args := totalQB.Build()
	total, err := scanReportRow(database.RawQueryRowContext(ctx, r.db, query, args...))
	if err != nil {
		return nil, err
	}
	report.Total = total
	return report, nil
}
