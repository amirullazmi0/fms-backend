package dto

// ProductionTarget: production target of one route (fleet), valid from ValidFrom until a newer one.
type ProductionTarget struct {
	ID          string   `json:"id"`
	RouteID     string   `json:"routeId"`
	RouteName   string   `json:"routeName"`
	RitPerHour  *float64 `json:"ritPerHour"`
	TonPerHour  *float64 `json:"tonPerHour"`
	RitPerShift *float64 `json:"ritPerShift"`
	TonPerShift *float64 `json:"tonPerShift"`
	ValidFrom   string   `json:"validFrom"` // YYYY-MM-DD
	Note        *string  `json:"note"`
	UpdatedAt   int64    `json:"updatedAt"`
}

type UpsertTargetRequest struct {
	RouteID     string   `json:"routeId" validate:"required,uuid"`
	RitPerHour  *float64 `json:"ritPerHour" validate:"omitempty,min=0,max=1000"`
	TonPerHour  *float64 `json:"tonPerHour" validate:"omitempty,min=0,max=100000"`
	RitPerShift *float64 `json:"ritPerShift" validate:"omitempty,min=0,max=10000"`
	TonPerShift *float64 `json:"tonPerShift" validate:"omitempty,min=0,max=1000000"`
	ValidFrom   string   `json:"validFrom" validate:"required,datetime=2006-01-02"`
	Note        *string  `json:"note" validate:"omitempty,max=200"`
}

// AssignmentWindow: a truck assigned to a route from Start until End (nil = still on).
type AssignmentWindow struct {
	Imei    string
	RouteID string
	Start   int64
	End     *int64
}

// Hour status: the numbers of an hour are final once every truck on duty in that hour has
// sent data past its end (data from blank spots arrives late, store and forward).
const (
	HourRunning     = "berjalan"  // the hour is not over yet
	HourProvisional = "sementara" // over, but some trucks have not sent data past its end
	HourFinal       = "final"
)

type HourlyRow struct {
	Key       string  `json:"key"`   // YYYY-MM-DD HH (report time zone)
	Label     string  `json:"label"` // 07:00
	Date      string  `json:"date"`  // YYYY-MM-DD
	Start     int64   `json:"start"`
	End       int64   `json:"end"`
	Rit       int     `json:"rit"` // ritase done (arrived at unloading)
	Ton       float64 `json:"ton"`
	TargetRit float64 `json:"targetRit"` // 0 = no target
	TargetTon float64 `json:"targetTon"`
	// Achievement: % of target (ton when a ton target exists, else ritase). nil = no target.
	Achievement *float64 `json:"achievement"`
	Status      string   `json:"status"` // berjalan | sementara | final
	Units       int      `json:"units"`  // trucks on duty in the hour
	// Pending: trucks on duty whose data has not reached the end of the hour yet.
	Pending []PendingUnit `json:"pending"`
}

type PendingUnit struct {
	Imei      string `json:"imei"`
	Truck     string `json:"truck"`
	LastGpsAt *int64 `json:"lastGpsAt"`
}

type HourlyReport struct {
	From  int64       `json:"from"`
	To    int64       `json:"to"`
	Rows  []HourlyRow `json:"rows"`
	Total HourlyRow   `json:"total"`
}

// Freshness: how late the data of one truck arrives (GPS time vs server time).
type Freshness struct {
	Imei           string  `json:"imei"`
	Truck          string  `json:"truck"`
	LastGpsAt      *int64  `json:"lastGpsAt"`      // newest GPS time stored
	LastReceivedAt *int64  `json:"lastReceivedAt"` // when the server last got data
	AvgDelayMs     float64 `json:"avgDelayMs"`     // received_at − time, last 12 h
	MaxDelayMs     float64 `json:"maxDelayMs"`
	LateRecords    int     `json:"lateRecords"` // records that arrived > 2 min after their GPS time (12 h)
	Records        int     `json:"records"`     // records received in the last 12 h
}

// StageStats: average time of one site stage set (finished stays).
type StageStats struct {
	Visits     int      `json:"visits"`
	AvgQueueMs float64  `json:"avgQueueMs"`
	AvgSpotMs  *float64 `json:"avgSpotMs"` // nil = no spot zone on the site
	AvgWorkMs  float64  `json:"avgWorkMs"`
	AvgDwellMs float64  `json:"avgDwellMs"`
}

// StageRow: cycle time per stage of one route (fleet), notes 2.4.
type StageRow struct {
	RouteID    string     `json:"routeId"`
	RouteName  string     `json:"routeName"`
	Loading    StageStats `json:"loading"`
	Hauling    float64    `json:"haulingMs"` // go leg (loaded)
	HaulingN   int        `json:"haulingCount"`
	Unloading  StageStats `json:"unloading"`
	Returning  float64    `json:"returningMs"` // back leg (empty)
	ReturningN int        `json:"returningCount"`
	CycleMs    float64    `json:"cycleMs"`    // queue+spot+work at both sites + hauling + returning
	RitPerHour float64    `json:"ritPerHour"` // 60 ÷ CT (minutes)
}

type StageReport struct {
	From int64      `json:"from"`
	To   int64      `json:"to"`
	Rows []StageRow `json:"rows"`
}

// ---------- PA & UA (P2-2) ----------

// AvailabilityQuery: period + bucket (total | day | month), optional unit / unit type.
type AvailabilityQuery struct {
	From     int64  `form:"from"`
	To       int64  `form:"to"`
	Bucket   string `form:"bucket" validate:"omitempty,oneof=total day month"`
	Imei     string `form:"imei" validate:"omitempty,len=15,numeric"`
	UnitType string `form:"unitType" validate:"omitempty,oneof=DT EX LV SUPPORT"`
}

// StatusInterval: one unit status log clipped to the period. Category follows the delay code,
// or the status when there is no code (standby without a code = unplanned).
type StatusInterval struct {
	Imei     string
	Category string // working | standby_planned | standby_unplanned | breakdown
	Start    int64
	End      int64
}

// AvailabilityUnit: unit identity for the availability report.
type AvailabilityUnit struct {
	Imei     string
	Label    string
	UnitType string
}

// AvailabilityRow: time per category and the indicators (percent, nil when undefined).
// PA = (T − BD) / T, UA = Working / (T − BD), MA = Working / (Working + BD), EU = Working / T.
// NoStatusMs = time without any status log (counted as available, not working).
type AvailabilityRow struct {
	Key                string   `json:"key"` // imei | unit type | "all"
	Label              string   `json:"label"`
	UnitType           string   `json:"unitType,omitempty"`
	Period             string   `json:"period,omitempty"` // YYYY-MM-DD | YYYY-MM, empty = whole period
	Units              int      `json:"units"`
	TotalMs            float64  `json:"totalMs"`
	WorkingMs          float64  `json:"workingMs"`
	StandbyPlannedMs   float64  `json:"standbyPlannedMs"`
	StandbyUnplannedMs float64  `json:"standbyUnplannedMs"`
	BreakdownMs        float64  `json:"breakdownMs"`
	NoStatusMs         float64  `json:"noStatusMs"`
	PA                 *float64 `json:"pa"`
	UA                 *float64 `json:"ua"`
	MA                 *float64 `json:"ma"`
	EU                 *float64 `json:"eu"`
}

type AvailabilityReport struct {
	From   int64             `json:"from"`
	To     int64             `json:"to"`
	Bucket string            `json:"bucket"`
	Units  []AvailabilityRow `json:"units"` // per unit (per period when bucketed)
	Types  []AvailabilityRow `json:"types"` // per unit type
	Total  []AvailabilityRow `json:"total"` // all units (one row per period)
}
