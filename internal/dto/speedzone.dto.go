package dto

// ---------- Zona batas kecepatan & overspeed (P3-3) ----------

type SpeedZone struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"` // downhill | crossing | barge | other
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Radius   int     `json:"radius"` // meter
	LimitKmh int     `json:"limitKmh"`
	Active   bool    `json:"active"`
}

type UpsertSpeedZoneRequest struct {
	Name     string  `json:"name" validate:"required,max=60"`
	Kind     string  `json:"kind" validate:"required,oneof=downhill crossing barge other"`
	Lat      float64 `json:"lat" validate:"gte=-90,lte=90"`
	Lng      float64 `json:"lng" validate:"gte=-180,lte=180"`
	Radius   int     `json:"radius" validate:"required,gte=10,lte=5000"`
	LimitKmh int     `json:"limitKmh" validate:"required,gte=5,lte=150"`
	Active   *bool   `json:"active"`
}

// OverspeedPoint: one GPS record faster than the limit of the zone it is in.
type OverspeedPoint struct {
	Imei       string
	Time       int64
	Speed      int
	ZoneID     string
	ZoneName   string
	LimitKmh   int
	DriverName *string
}

// Overspeed: one episode (consecutive records of one unit in one zone above the limit).
type Overspeed struct {
	Imei       string  `json:"imei"`
	Unit       string  `json:"unit"`
	DriverName *string `json:"driverName"`
	ZoneID     string  `json:"zoneId"`
	ZoneName   string  `json:"zoneName"`
	LimitKmh   int     `json:"limitKmh"`
	MaxSpeed   int     `json:"maxSpeed"`
	StartAt    int64   `json:"startAt"`
	EndAt      int64   `json:"endAt"`
	Points     int     `json:"points"`
}

// OverspeedDriver: per operator: how often and how much over the limit.
type OverspeedDriver struct {
	DriverName string `json:"driverName"`
	Episodes   int    `json:"episodes"`
	MaxOver    int    `json:"maxOver"` // km/jam di atas batas, terbesar
	TotalMs    int64  `json:"totalMs"`
}

type OverspeedReport struct {
	From      int64             `json:"from"`
	To        int64             `json:"to"`
	Episodes  []Overspeed       `json:"episodes"`
	Drivers   []OverspeedDriver `json:"drivers"`
	Truncated bool              `json:"truncated"`
}
