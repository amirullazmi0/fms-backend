package dto

// ---------- Fleet, match factor & panel dispatcher (P2-5 … P2-7) ----------

// FleetLoader: the excavator working a route (fleet) in a period. EndAt nil = still there.
type FleetLoader struct {
	ID              string   `json:"id"`
	RouteID         string   `json:"routeId"`
	Imei            string   `json:"imei"`
	Label           string   `json:"label"`
	ProductivityTph *float64 `json:"productivityTph"` // ton per jam (kapasitas alat muat)
	StartAt         int64    `json:"startAt"`
	EndAt           *int64   `json:"endAt"`
}

// SetLoaderRequest: put an excavator on the fleet (imei) or take it off (imei empty).
type SetLoaderRequest struct {
	Imei            string   `json:"imei" validate:"omitempty,len=15,numeric"`
	ProductivityTph *float64 `json:"productivityTph" validate:"omitempty,gt=0,lte=10000"`
}

// FleetTruck: one unit on duty in the fleet, with what it is doing now.
type FleetTruck struct {
	Imei       string  `json:"imei"`
	Label      string  `json:"label"`
	DriverName string  `json:"driverName"`
	Category   string  `json:"category"`           // working | standby_planned | standby_unplanned | breakdown | "" (no status)
	Status     *string `json:"status"`             // delay code name
	AtFront    bool    `json:"atFront"`            // inside the front loading radius, standing still
	GpsAgeMs   *int64  `json:"gpsAgeMs"`           // age of the last GPS position
	Inactive   *string `json:"inactive,omitempty"` // reason it does not produce now (nil = active)
}

// Fleet: live state of one route + its excavator.
//
// Match factor MF = (DT aktif × waktu muat per DT) ÷ (jumlah excavator × cycle time DT).
// < 0,9 undermatch (excavator menunggu DT), 0,9–1,1 match, > 1,1 overmatch (DT antre di front).
type Fleet struct {
	RouteID       string       `json:"routeId"`
	RouteName     string       `json:"routeName"`
	LoadingName   string       `json:"loadingName"`
	UnloadingName string       `json:"unloadingName"`
	Loader        *FleetLoader `json:"loader"`
	Trucks        []FleetTruck `json:"trucks"`
	Active        int          `json:"active"`        // DT producing now (not breakdown / standby / no data)
	LoadTimeMs    float64      `json:"loadTimeMs"`    // waktu muat per DT (spot + muat di front)
	CycleMs       float64      `json:"cycleMs"`       // cycle time DT
	BasisWindow   string       `json:"basisWindow"`   // "3 jam" | "7 hari" | "perkiraan"
	MatchFactor   *float64     `json:"matchFactor"`   // nil = belum ada DT aktif / data
	Match         string       `json:"match"`         // under | match | over | ""
	IdealTrucks   *float64     `json:"idealTrucks"`   // DT agar MF = 1
	Queue         int          `json:"queue"`         // DT antre di front (di radius, diam, selain yang dimuat)
	HangingMs     *int64       `json:"hangingMs"`     // excavator menunggu DT, 60 menit terakhir
	HangingWindow int64        `json:"hangingWindow"` // panjang jendela hanging (ms)
}

// FleetMove: suggestion to move one DT from one fleet to another to balance MF.
type FleetMove struct {
	Imei          string   `json:"imei"`
	Label         string   `json:"label"`
	FromRouteID   string   `json:"fromRouteId"`
	FromRouteName string   `json:"fromRouteName"`
	ToRouteID     string   `json:"toRouteId"`
	ToRouteName   string   `json:"toRouteName"`
	FromMfAfter   *float64 `json:"fromMfAfter"`
	ToMfAfter     *float64 `json:"toMfAfter"`
	Text          string   `json:"text"`
}

// MoveUnitRequest: pindahkan unit (DT) ke fleet lain.
type MoveUnitRequest struct {
	Imei      string `json:"imei" validate:"required,len=15,numeric"`
	ToRouteID string `json:"toRouteId" validate:"required,uuid"`
}

type DispatchBoard struct {
	At     int64       `json:"at"`
	Fleets []Fleet     `json:"fleets"`
	Moves  []FleetMove `json:"moves"`
}

// StayInterval: a unit inside a site radius (site_visits), for the excavator hanging time.
type StayInterval struct {
	Start int64
	End   int64
}
