package dto

// ---------- Delay code & unit status ----------

// Unit status (PA/UA base): working, standby (planned or not) or breakdown.
const (
	UnitWorking   = "working"
	UnitStandby   = "standby"
	UnitBreakdown = "breakdown"
)

// DelayCode: a reason the operator picks on the MDT (P2H, Ishoma, Sholat, Antre, BD Ban, ...).
type DelayCode struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Category string  `json:"category"` // working | standby_planned | standby_unplanned | breakdown
	Status   string  `json:"status"`   // working | standby | breakdown (from the category)
	Icon     *string `json:"icon"`
	Sort     int     `json:"sort"`
	Active   bool    `json:"active"`
}

// StatusOf: unit status of a delay category.
func StatusOf(category string) string {
	switch category {
	case "working":
		return UnitWorking
	case "breakdown":
		return UnitBreakdown
	}
	return UnitStandby
}

// Queue delay codes (the operator says the unit is queuing).
var QueueCodes = map[string]bool{"Q_LOAD": true, "Q_DUMP": true}

type UpsertDelayCodeRequest struct {
	Code     string  `json:"code" validate:"required,max=20"` // A-Z, 0-9, _ (checked in the usecase)
	Name     string  `json:"name" validate:"required,max=100"`
	Category string  `json:"category" validate:"required,oneof=working standby_planned standby_unplanned breakdown"`
	Icon     *string `json:"icon" validate:"omitempty,max=40"`
	Sort     int     `json:"sort" validate:"min=0,max=9999"`
	Active   *bool   `json:"active"`
}

// UnitStatusLog: one status period of a unit. EndAt nil = current status.
type UnitStatusLog struct {
	ID         string  `json:"id"`
	Imei       string  `json:"imei"`
	Truck      string  `json:"truck"`
	DriverID   *string `json:"driverId"`
	DriverName *string `json:"driverName"`
	Status     string  `json:"status"`
	DelayCode  *string `json:"delayCode"`
	DelayName  *string `json:"delayName"`
	Category   *string `json:"category"`
	StartAt    int64   `json:"startAt"`
	EndAt      *int64  `json:"endAt"`
	Source     string  `json:"source"` // auto | supervisor | dmr | mdt
	Note       *string `json:"note"`
	ReceivedAt int64   `json:"receivedAt"`
}

// UnitStatusInput is one status change to store.
type UnitStatusInput struct {
	ID        string // uuid; MDT makes it so a re-sent change is not stored twice ("" = new)
	Imei      string
	DriverID  *string
	DelayCode string
	Status    string
	StartAt   int64
	ClientAt  *int64
	Source    string
	Note      *string
	CreatedBy *string
}

// SetUnitStatusRequest: supervisor sets a unit's status by hand.
type SetUnitStatusRequest struct {
	Imei      string  `json:"imei" validate:"required,len=15,numeric"`
	DelayCode string  `json:"delayCode" validate:"required,max=20"`
	StartAt   int64   `json:"startAt"` // 0 = now
	Note      *string `json:"note" validate:"omitempty,max=200"`
}

type UnitStatusQuery struct {
	Imei string `form:"imei" validate:"omitempty,len=15,numeric"`
	From int64  `form:"from"`
	To   int64  `form:"to"`
}

// ---------- MDT (tablet in the cab) ----------

type MdtPairCode struct {
	Code      string `json:"code"` // 6 digits, show to the operator once
	ExpiresAt int64  `json:"expiresAt"`
}

type MdtPairRequest struct {
	Code string  `json:"code" validate:"required,len=6,numeric"`
	Name *string `json:"name" validate:"omitempty,max=100"`
}

type MdtPairResponse struct {
	Token string  `json:"token"` // long-lived tablet token (send as Bearer)
	Imei  string  `json:"imei"`
	Unit  MdtUnit `json:"unit"`
}

type MdtTablet struct {
	ID         string  `json:"id"`
	Imei       string  `json:"imei"`
	Name       *string `json:"name"`
	PairedAt   *int64  `json:"pairedAt"`
	LastSyncAt *int64  `json:"lastSyncAt"`
	RevokedAt  *int64  `json:"revokedAt"`
	CreatedAt  int64   `json:"createdAt"`
}

type MdtUnit struct {
	Imei       string  `json:"imei"`
	Label      string  `json:"label"` // nomor lambung / plat
	Vehicle    *string `json:"vehicle"`
	UnitType   string  `json:"unitType"`
	PayloadTon float64 `json:"payloadTon"`
}

type MdtLoginRequest struct {
	EmployeeNo string `json:"employeeNo" validate:"required,max=30"`
	Pin        string `json:"pin" validate:"required,min=4,max=8,numeric"`
}

type MdtOperator struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	EmployeeNo *string `json:"employeeNo"`
}

// MdtDuty: the unit's current (or next planned) assignment.
type MdtDuty struct {
	AssignmentID  string   `json:"assignmentId"`
	ShiftID       string   `json:"shiftId"`
	ShiftName     string   `json:"shiftName"`
	ShiftStatus   string   `json:"shiftStatus"` // planned | active
	PlannedStart  int64    `json:"plannedStart"`
	PlannedEnd    int64    `json:"plannedEnd"`
	StartedAt     *int64   `json:"startedAt"`
	RouteID       string   `json:"routeId"`
	RouteName     string   `json:"routeName"`
	LoadingName   string   `json:"loadingName"`
	UnloadingName string   `json:"unloadingName"`
	DriverID      string   `json:"driverId"`
	DriverName    string   `json:"driverName"`
	TargetRit     *float64 `json:"targetRit"` // per shift, route target
	TargetTon     *float64 `json:"targetTon"`
	Loading       MdtSite  `json:"loading"`   // front loading of the route
	Unloading     MdtSite  `json:"unloading"` // dumping of the route
}

// MdtSite: a site the tablet locates itself against (cached for use without signal).
type MdtSite struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Lat    float64    `json:"lat"`
	Lng    float64    `json:"lng"`
	Radius int        `json:"radius"`
	Zones  []SiteZone `json:"zones"`
}

// MdtPosition: last GPS position of the unit from its tracker (Teltonika), as of the last sync.
type MdtPosition struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Angle int     `json:"angle"`
	Speed int     `json:"speed"`
	Time  int64   `json:"time"`
}

type MdtProduction struct {
	Rit        int     `json:"rit"`
	Ton        float64 `json:"ton"`
	AvgCycleMs float64 `json:"avgCycleMs"`
	At         int64   `json:"at"`
}

// MdtBootstrap: everything the tablet caches to work offline.
type MdtBootstrap struct {
	ServerTime int64          `json:"serverTime"`
	Unit       MdtUnit        `json:"unit"`
	Duty       *MdtDuty       `json:"duty"`
	Production *MdtProduction `json:"production"`
	DelayCodes []DelayCode    `json:"delayCodes"`
	Operators  []MdtOperator  `json:"operators"` // active drivers with a PIN (no PIN hashes)
	Current    *UnitStatusLog `json:"current"`
	Position   *MdtPosition   `json:"position"` // last tracker position (nil = none yet)
}

// MdtStatusItem: one status change made on the tablet (maybe offline).
type MdtStatusItem struct {
	ID        string  `json:"id" validate:"required,uuid"`
	DriverID  *string `json:"driverId" validate:"omitempty,uuid"`
	DelayCode string  `json:"delayCode" validate:"required,max=20"`
	ClientAt  int64   `json:"clientAt" validate:"required,gt=0"` // tablet clock when pressed
	Note      *string `json:"note" validate:"omitempty,max=200"`
}

type MdtStatusBatch struct {
	// SentAt: tablet clock when the batch was sent; the server uses it to correct the tablet clock.
	SentAt int64           `json:"sentAt" validate:"required,gt=0"`
	Items  []MdtStatusItem `json:"items" validate:"required,min=1,max=500,dive"`
}

type MdtStatusBatchResult struct {
	Accepted []string `json:"accepted"` // ids stored now or already stored before
	Rejected []string `json:"rejected"` // unknown delay code
	OffsetMs int64    `json:"offsetMs"` // server − tablet clock
}

type SetPinRequest struct {
	Pin string `json:"pin" validate:"required,min=4,max=8,numeric"`
}
