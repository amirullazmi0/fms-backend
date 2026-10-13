package dto

// Site is a geofence point. Its loading/unloading role comes from routes.
type Site struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Lat       float64  `json:"lat"`
	Lng       float64  `json:"lng"`
	Radius    int      `json:"radius"` // meter
	Note      *string  `json:"note"`
	Imeis     []string `json:"imeis"` // trucks on duty on a route through this site
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}

type UpsertSiteRequest struct {
	Name   string   `json:"name" validate:"required,max=60"`
	Lat    *float64 `json:"lat" validate:"required,min=-90,max=90"`
	Lng    *float64 `json:"lng" validate:"required,min=-180,max=180"`
	Radius *int     `json:"radius" validate:"omitempty,min=5,max=1000"`
	Note   *string  `json:"note" validate:"omitempty,max=200"`
}

// OnDuty is who is running a route: phase active (shift running), finishing (shift stopped,
// last ritase still heading to unloading) or done (shift finished in the last 12 h).
type OnDuty struct {
	ShiftID    string `json:"shiftId"`
	ShiftName  string `json:"shiftName"`
	DriverID   string `json:"driverId"`
	DriverName string `json:"driverName"`
	Imei       string `json:"imei"`
	Phase      string `json:"phase"`
	ClosedAt   *int64 `json:"closedAt"`
}

// Route is one loading site + one unloading site.
type Route struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	LoadingID   string   `json:"loadingId"`
	UnloadingID string   `json:"unloadingId"`
	Note        *string  `json:"note"`
	OnDuty      []OnDuty `json:"onDuty"`
	RecentDone  []OnDuty `json:"recentDone,omitempty"` // trucks done with a shift of this route (last 12 h)
	Imeis       []string `json:"imeis"`
	CreatedAt   int64    `json:"createdAt"`
	UpdatedAt   int64    `json:"updatedAt"`
}

type UpsertRouteRequest struct {
	Name        string  `json:"name" validate:"required,max=60"`
	LoadingID   string  `json:"loadingId" validate:"required,uuid"`
	UnloadingID string  `json:"unloadingId" validate:"required,uuid,nefield=LoadingID"`
	Note        *string `json:"note" validate:"omitempty,max=200"`
}
