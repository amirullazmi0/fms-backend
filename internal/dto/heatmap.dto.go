package dto

// ---------- Heat map kecepatan (P3-2) ----------

// HeatmapQuery: period, optional route, muatan (loaded = hauling, empty = return, "" = semua).
type HeatmapQuery struct {
	From  int64  `form:"from"`
	To    int64  `form:"to"`
	Route string `form:"route" validate:"omitempty,uuid"`
	Load  string `form:"load" validate:"omitempty,oneof=loaded empty"`
}

// HeatCell: one road cell (±22 m) with the unit speeds measured in it during ritase, outside the
// front loading / dumping radius.
type HeatCell struct {
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Points   int     `json:"points"`
	AvgSpeed float64 `json:"avgSpeed"` // km/jam
	MaxSpeed int     `json:"maxSpeed"`
}

// ShockPoint: accelerometer event (harsh brake / accel / corner, crash): jalan rusak / cara mengemudi.
type ShockPoint struct {
	Lat  float64  `json:"lat"`
	Lng  float64  `json:"lng"`
	Time int64    `json:"time"`
	Imei string   `json:"imei"`
	Kind string   `json:"kind"` // accel | brake | corner | crash
	G    *float64 `json:"g"`
}

type HeatmapReport struct {
	From      int64        `json:"from"`
	To        int64        `json:"to"`
	CellDeg   float64      `json:"cellDeg"`
	Cells     []HeatCell   `json:"cells"`
	Shocks    []ShockPoint `json:"shocks"`
	Truncated bool         `json:"truncated"` // too many cells: only the busiest are returned
}
