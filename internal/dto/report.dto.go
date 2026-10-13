package dto

// TripRecord is a stored ritase (trips table), the base of every report.
type TripRecord struct {
	ID              string   `json:"id"`
	AssignmentID    *string  `json:"assignmentId"`
	ShiftID         *string  `json:"shiftId"`
	ShiftName       *string  `json:"shiftName"`
	ShiftDate       *string  `json:"shiftDate"`
	DriverID        *string  `json:"driverId"`
	DriverName      *string  `json:"driverName"`
	Imei            string   `json:"imei"`
	Truck           *string  `json:"truck"`
	RouteID         *string  `json:"routeId"`
	RouteName       string   `json:"routeName"`
	Leg             string   `json:"leg"`
	LoadingName     string   `json:"loadingName"`
	UnloadingName   string   `json:"unloadingName"`
	DepartAt        int64    `json:"departAt"`
	ArriveAt        *int64   `json:"arriveAt"`
	DurationMs      int64    `json:"durationMs"`
	Distance        float64  `json:"distance"`
	MovingMs        int64    `json:"movingMs"`
	Stops           int      `json:"stops"`
	StoppedMs       int64    `json:"stoppedMs"`
	MaxSpeed        int      `json:"maxSpeed"`
	AvgSpeed        float64  `json:"avgSpeed"`
	FuelUnit        *string  `json:"fuelUnit"`
	FuelAtLoading   *float64 `json:"fuelAtLoading"`
	FuelAtUnloading *float64 `json:"fuelAtUnloading"`
	FuelUsed        *float64 `json:"fuelUsed"`
	LoadDwellMs     int64    `json:"loadDwellMs"`
	UnloadDwellMs   *int64   `json:"unloadDwellMs"`
	Status          string   `json:"status"` // done | unfinished
}

// TripUpsert is one computed ritase to store for an assignment.
type TripUpsert struct {
	AssignmentID    string
	ShiftID         string
	DriverID        string
	Imei            string
	RouteID         string
	RouteName       string
	Leg             string
	LoadingSiteID   string
	LoadingName     string
	UnloadingSiteID string
	UnloadingName   string
	DepartAt        int64
	ArriveAt        *int64
	DurationMs      int64
	Distance        float64
	MovingMs        int64
	Stops           int
	StoppedMs       int64
	MaxSpeed        int
	AvgSpeed        float64
	FuelUnit        *string
	FuelAtLoading   *float64
	FuelAtUnloading *float64
	FuelUsed        *float64
	LoadDwellMs     int64
	UnloadDwellMs   *int64
	Status          string
}

type ReportRow struct {
	Key                string   `json:"key"`
	Label              string   `json:"label"`
	Sub                *string  `json:"sub"`
	Trips              int      `json:"trips"`
	Done               int      `json:"done"`
	AvgDurationMs      float64  `json:"avgDurationMs"`
	MinDurationMs      float64  `json:"minDurationMs"`
	MaxDurationMs      float64  `json:"maxDurationMs"`
	BackDone           int      `json:"backDone"`
	BackAvgMs          float64  `json:"backAvgMs"`
	BackMinMs          float64  `json:"backMinMs"`
	BackMaxMs          float64  `json:"backMaxMs"`
	Distance           float64  `json:"distance"`
	Stops              int      `json:"stops"`
	StoppedMs          float64  `json:"stoppedMs"`
	FuelUnit           *string  `json:"fuelUnit"`
	FuelUsed           *float64 `json:"fuelUsed"`
	AvgFuel            *float64 `json:"avgFuel"`
	AvgFuelAtLoading   *float64 `json:"avgFuelAtLoading"`
	AvgFuelAtUnloading *float64 `json:"avgFuelAtUnloading"`
}

type Report struct {
	From  int64       `json:"from"`
	To    int64       `json:"to"`
	Group string      `json:"group"`
	Rows  []ReportRow `json:"rows"`
	Total ReportRow   `json:"total"`
}

// ReportQuery is ?from=&to= (ms) &group=&route=&driver=&imei=&shift=&limit=&offset=
type ReportQuery struct {
	From   int64  `form:"from"`
	To     int64  `form:"to"`
	Group  string `form:"group" validate:"omitempty,oneof=route driver truck shift day"`
	Route  string `form:"route" validate:"omitempty,uuid"`
	Driver string `form:"driver" validate:"omitempty,uuid"`
	Shift  string `form:"shift" validate:"omitempty,uuid"`
	Imei   string `form:"imei" validate:"omitempty,len=15,numeric"`
	Role   string `form:"role" validate:"omitempty,oneof=loading unloading"`
	Limit  int    `form:"limit"`
	Offset int    `form:"offset"`
}

type TripFilter struct {
	From     int64
	To       int64
	RouteID  string
	DriverID string
	Imei     string
	ShiftID  string
}

// SiteVisitUpsert is one stay of a truck at a loading/unloading site (scheduler → site_visits).
type SiteVisitUpsert struct {
	AssignmentID string
	ShiftID      string
	DriverID     string
	Imei         string
	RouteID      string
	SiteID       string
	SiteName     string
	Role         string // loading | unloading
	ArriveAt     int64
	DepartAt     *int64 // nil = still inside
	DwellMs      int64
	Finished     bool // still inside when the shift ended (status finished)
}

// DwellStats: time spent at one kind of site in the period.
type DwellStats struct {
	Visits     int     `json:"visits"`     // finished stays
	AvgDwellMs float64 `json:"avgDwellMs"` // finished stays
	MinDwellMs int64   `json:"minDwellMs"`
	MaxDwellMs int64   `json:"maxDwellMs"`
	TotalMs    int64   `json:"totalMs"` // finished + ongoing
	Inside     int     `json:"inside"`  // trucks there right now
}

// SiteStay is a truck currently inside a site.
type SiteStay struct {
	Imei       string  `json:"imei"`
	Truck      string  `json:"truck"`
	DriverName *string `json:"driverName"`
	RouteName  *string `json:"routeName"`
	SiteName   string  `json:"siteName"`
	Role       string  `json:"role"`
	ArriveAt   int64   `json:"arriveAt"`
	DwellMs    int64   `json:"dwellMs"`
}

// SiteDwellRow: dwell per site.
type SiteDwellRow struct {
	SiteID     string  `json:"siteId"`
	SiteName   string  `json:"siteName"`
	Role       string  `json:"role"`
	Visits     int     `json:"visits"`
	AvgDwellMs float64 `json:"avgDwellMs"`
	MaxDwellMs int64   `json:"maxDwellMs"`
	Inside     int     `json:"inside"`
}

// VisitReport: time at loading / unloading sites (GET /reports/visits).
type VisitReport struct {
	From      int64          `json:"from"`
	To        int64          `json:"to"`
	Loading   DwellStats     `json:"loading"`
	Unloading DwellStats     `json:"unloading"`
	Sites     []SiteDwellRow `json:"sites"`
	Current   []SiteStay     `json:"current"` // trucks inside a site now (any period)
}

// SiteVisitRecord is one stored stay at a loading/unloading site (raw list).
type SiteVisitRecord struct {
	ID         string  `json:"id"`
	Imei       string  `json:"imei"`
	Truck      string  `json:"truck"`
	DriverName *string `json:"driverName"`
	ShiftName  *string `json:"shiftName"`
	RouteName  *string `json:"routeName"`
	SiteName   string  `json:"siteName"`
	Role       string  `json:"role"`   // loading | unloading
	Status     string  `json:"status"` // inside | done | finished (shift ended while there)
	ArriveAt   int64   `json:"arriveAt"`
	DepartAt   *int64  `json:"departAt"`
	DwellMs    int64   `json:"dwellMs"` // inside: until now
}
