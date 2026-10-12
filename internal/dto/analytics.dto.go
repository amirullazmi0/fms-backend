package dto

// Trip / ritase analytics computed from AVL records (fmctest/server/trips.ts + sites.ts).

type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type TrackPoint struct {
	T     int64    `json:"t"`
	Lat   float64  `json:"lat"`
	Lng   float64  `json:"lng"`
	Speed int      `json:"speed"`
	Angle int      `json:"angle"`
	Ign   *bool    `json:"ign"`
	Fuel  *float64 `json:"fuel"`
}

type FuelEvent struct {
	Type   string  `json:"type"` // refuel | drop
	Time   int64   `json:"time"`
	Amount float64 `json:"amount"`
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
}

type TripSegment struct {
	Type      string   `json:"type"` // trip | stop
	Start     int64    `json:"start"`
	End       int64    `json:"end"`
	Distance  float64  `json:"distance"`
	MaxSpeed  int      `json:"maxSpeed"`
	AvgSpeed  float64  `json:"avgSpeed"`
	From      LatLng   `json:"from"`
	To        LatLng   `json:"to"`
	Idle      bool     `json:"idle"`
	Place     *string  `json:"place"`
	FuelUsed  *float64 `json:"fuelUsed"`
	FromIndex int      `json:"fromIndex"`
	ToIndex   int      `json:"toIndex"`
}

type DaySummary struct {
	From       int64         `json:"from"`
	To         int64         `json:"to"`
	Distance   float64       `json:"distance"`
	MovingMs   int64         `json:"movingMs"`
	StoppedMs  int64         `json:"stoppedMs"`
	IdleMs     int64         `json:"idleMs"`
	MaxSpeed   int           `json:"maxSpeed"`
	AvgSpeed   float64       `json:"avgSpeed"`
	Trips      int           `json:"trips"`
	Stops      int           `json:"stops"`
	EngineMs   int64         `json:"engineMs"`
	FuelUnit   *string       `json:"fuelUnit"` // L | % | null
	FuelUsed   *float64      `json:"fuelUsed"`
	FuelEvents []FuelEvent   `json:"fuelEvents"`
	Segments   []TripSegment `json:"segments"`
	Points     []TrackPoint  `json:"points"`
}

type SiteVisit struct {
	SiteID  string `json:"siteId"`
	Name    string `json:"name"`
	Arrive  int64  `json:"arrive"`
	Depart  *int64 `json:"depart"` // null = still inside (last data)
	DwellMs int64  `json:"dwellMs"`
}

type RouteTrip struct {
	RouteID         string     `json:"routeId"`
	RouteName       string     `json:"routeName"`
	Leg             string     `json:"leg"` // go | back
	Loading         SiteVisit  `json:"loading"`
	Unloading       *SiteVisit `json:"unloading"`
	Status          string     `json:"status"` // done | ongoing | unfinished
	Start           int64      `json:"start"`
	End             int64      `json:"end"`
	DurationMs      int64      `json:"durationMs"`
	Distance        float64    `json:"distance"`
	MovingMs        int64      `json:"movingMs"`
	Stops           int        `json:"stops"`
	StoppedMs       int64      `json:"stoppedMs"`
	MaxSpeed        int        `json:"maxSpeed"`
	AvgSpeed        float64    `json:"avgSpeed"`
	FuelUsed        *float64   `json:"fuelUsed"`
	FuelAtLoading   *float64   `json:"fuelAtLoading"`
	FuelAtUnloading *float64   `json:"fuelAtUnloading"`
}

type RouteReportTotals struct {
	Trips         int      `json:"trips"`
	Done          int      `json:"done"`
	AvgDurationMs float64  `json:"avgDurationMs"`
	BackDone      int      `json:"backDone"`
	BackAvgMs     float64  `json:"backAvgMs"`
	Distance      float64  `json:"distance"`
	Stops         int      `json:"stops"`
	StoppedMs     int64    `json:"stoppedMs"`
	FuelUsed      *float64 `json:"fuelUsed"`
}

type RouteReport struct {
	From     int64             `json:"from"`
	To       int64             `json:"to"`
	FuelUnit *string           `json:"fuelUnit"`
	Routes   []Route           `json:"routes"`
	Sites    []Site            `json:"sites"`
	Visits   []SiteVisit       `json:"visits"`
	Trips    []RouteTrip       `json:"trips"`
	Totals   RouteReportTotals `json:"totals"`
}

type RouteHistoryLatest struct {
	Imei       string       `json:"imei"`
	Start      int64        `json:"start"`
	End        int64        `json:"end"`
	Distance   float64      `json:"distance"`
	DurationMs int64        `json:"durationMs"`
	Coords     [][2]float64 `json:"coords"`
}

type RouteHistory struct {
	Days          int                 `json:"days"`
	Trips         int                 `json:"trips"`
	AvgDurationMs float64             `json:"avgDurationMs"`
	MinDurationMs int64               `json:"minDurationMs"`
	MaxDurationMs int64               `json:"maxDurationMs"`
	AvgDistance   float64             `json:"avgDistance"`
	AvgStops      float64             `json:"avgStops"`
	AvgFuel       *float64            `json:"avgFuel"`
	FuelUnit      *string             `json:"fuelUnit"`
	Latest        *RouteHistoryLatest `json:"latest"`
	// Paths: the road usually taken pergi & pulang (from the ritase GPS traces).
	Paths *RouteUsualPaths `json:"paths,omitempty"`
}

// UsualPath is the road most ritase in one direction took.
type UsualPath struct {
	Trips         int          `json:"trips"`    // finished ritase in this direction
	Compared      int          `json:"compared"` // latest ritase compared (max 25)
	Follow        int          `json:"follow"`   // of those, how many took (about) this road
	Distance      float64      `json:"distance"` // meter, of the trace drawn
	DurationMs    int64        `json:"durationMs"`
	AvgDurationMs float64      `json:"avgDurationMs"` // all ritase in this direction
	LastEnd       int64        `json:"lastEnd"`
	Coords        [][2]float64 `json:"coords"`
}

type RouteUsualPaths struct {
	Go   *UsualPath `json:"go"`   // loading → unloading
	Back *UsualPath `json:"back"` // unloading → loading
}

type RoadPath struct {
	Distance       float64      `json:"distance"`
	DurationS      float64      `json:"durationS"`
	TruckDurationS float64      `json:"truckDurationS"`
	Coords         [][2]float64 `json:"coords"` // [lat, lng]
}

type RoadRoute struct {
	Paths       []RoadPath `json:"paths"`
	TruckFactor float64    `json:"truckFactor"`
	Source      string     `json:"source"`
	FetchedAt   int64      `json:"fetchedAt"`
}

type RoadQuery struct {
	From string `form:"from" validate:"required,uuid"`
	To   string `form:"to" validate:"required,uuid"`
}

type RouteHistoryQuery struct {
	Days int `form:"days"`
}
