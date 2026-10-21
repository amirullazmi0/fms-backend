package dto

// ---------- Log breakdown & durasi perbaikan (P3-5) ----------

// BreakdownLog: one breakdown status log row clipped to nothing (raw from unit_status_logs).
type BreakdownLog struct {
	Imei       string
	Unit       string
	Code       *string
	CodeName   *string
	DriverName *string
	Note       *string
	Source     string
	StartAt    int64
	EndAt      *int64
}

// Breakdown: one breakdown episode (consecutive BD logs of a unit merged; BD mesin → BD hidrolik
// without returning to working is one repair). EndAt nil = unit masih breakdown.
type Breakdown struct {
	Imei       string   `json:"imei"`
	Unit       string   `json:"unit"`
	Codes      []string `json:"codes"` // nama delay code BD, berurutan
	DriverName *string  `json:"driverName"`
	Note       *string  `json:"note"`
	StartAt    int64    `json:"startAt"`
	EndAt      *int64   `json:"endAt"`
	DurationMs int64    `json:"durationMs"` // sampai sekarang bila masih breakdown
}

// BreakdownUnit: per unit. MTTR = rata-rata durasi perbaikan breakdown yang sudah selesai.
type BreakdownUnit struct {
	Imei    string `json:"imei"`
	Unit    string `json:"unit"`
	Count   int    `json:"count"`
	TotalMs int64  `json:"totalMs"`
	MttrMs  *int64 `json:"mttrMs"`
	OpenNow bool   `json:"openNow"`
}

// BreakdownCode: per jenis breakdown (delay code).
type BreakdownCode struct {
	Name    string `json:"name"`
	Count   int    `json:"count"`
	TotalMs int64  `json:"totalMs"`
}

type BreakdownReport struct {
	From     int64           `json:"from"`
	To       int64           `json:"to"`
	Episodes []Breakdown     `json:"episodes"`
	Units    []BreakdownUnit `json:"units"`
	Codes    []BreakdownCode `json:"codes"`
	MttrMs   *int64          `json:"mttrMs"` // semua unit
	Open     int             `json:"open"`   // unit masih breakdown
}
