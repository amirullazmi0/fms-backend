package dto

import "slices"

// Site is a geofence point. Its loading/unloading role comes from routes.
type Site struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Lat    float64  `json:"lat"`
	Lng    float64  `json:"lng"`
	Radius int      `json:"radius"` // meter
	Note   *string  `json:"note"`
	Imeis  []string `json:"imeis"` // trucks on duty on a route through this site
	// Queue alert: >= QueueAlertTrucks units queuing longer than QueueAlertMin minutes.
	// omitempty: sites inside ritase reports keep their old JSON shape (golden test).
	QueueAlertTrucks int        `json:"queueAlertTrucks,omitempty"`
	QueueAlertMin    int        `json:"queueAlertMin,omitempty"`
	Zones            []SiteZone `json:"zones,omitempty"`
	CreatedAt        int64      `json:"createdAt"`
	UpdatedAt        int64      `json:"updatedAt"`
}

// SiteZone is a circle inside / next to a site: the queue line or the spot (loading / dumping position).
type SiteZone struct {
	ID     string  `json:"id"`
	Kind   string  `json:"kind"` // queue | spot
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Radius int     `json:"radius"` // meter
}

type SiteZoneInput struct {
	Kind   string   `json:"kind" validate:"required,oneof=queue spot"`
	Lat    *float64 `json:"lat" validate:"required,min=-90,max=90"`
	Lng    *float64 `json:"lng" validate:"required,min=-180,max=180"`
	Radius int      `json:"radius" validate:"min=3,max=1000"`
}

// UpdateSiteZonesRequest replaces all zones of a site and its queue alert thresholds.
type UpdateSiteZonesRequest struct {
	QueueAlertTrucks int             `json:"queueAlertTrucks" validate:"min=1,max=100"`
	QueueAlertMin    int             `json:"queueAlertMin" validate:"min=1,max=600"`
	Zones            []SiteZoneInput `json:"zones" validate:"max=20,dive"`
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
	// UnloadingID: the stockpile / jetty the unit hauls to (null = the route's main one).
	UnloadingID *string `json:"unloadingId,omitempty"`
}

// Route is one loading site (pit) + one or more unloading sites (stockpile / jetty).
type Route struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	LoadingID   string `json:"loadingId"`
	UnloadingID string `json:"unloadingId"` // main unloading site
	// UnloadingIDs: every unloading site, the main one first. A ritase ends at any of them.
	UnloadingIDs []string `json:"unloadingIds,omitempty"`
	Note         *string  `json:"note"`
	OnDuty       []OnDuty `json:"onDuty"`
	RecentDone   []OnDuty `json:"recentDone,omitempty"` // trucks done with a shift of this route (last 12 h)
	Imeis        []string `json:"imeis"`
	CreatedAt    int64    `json:"createdAt"`
	UpdatedAt    int64    `json:"updatedAt"`
}

type UpsertRouteRequest struct {
	Name        string `json:"name" validate:"required,max=60"`
	LoadingID   string `json:"loadingId" validate:"required,uuid"`
	UnloadingID string `json:"unloadingId" validate:"omitempty,uuid"`
	// UnloadingIDs: every unloading site, the main one first (replaces UnloadingID when set).
	UnloadingIDs []string `json:"unloadingIds" validate:"omitempty,max=10,dive,uuid"`
	Note         *string  `json:"note" validate:"omitempty,max=200"`
}

// Unloadings: the unloading sites of the request, the main one first.
func (b UpsertRouteRequest) Unloadings() []string {
	if len(b.UnloadingIDs) > 0 {
		return b.UnloadingIDs
	}
	if b.UnloadingID == "" {
		return nil
	}
	return []string{b.UnloadingID}
}

// Unloadings: the unloading sites of the route, the main one first.
func (r Route) Unloadings() []string {
	if len(r.UnloadingIDs) > 0 {
		return r.UnloadingIDs
	}
	return []string{r.UnloadingID}
}

// HasUnloading: id is one of the route's unloading sites.
func (r Route) HasUnloading(id string) bool {
	return slices.Contains(r.Unloadings(), id)
}

// SiteIDs: the loading site and every unloading site.
func (r Route) SiteIDs() []string {
	return append([]string{r.LoadingID}, r.Unloadings()...)
}
