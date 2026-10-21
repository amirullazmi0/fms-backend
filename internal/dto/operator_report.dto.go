package dto

// ---------- Rapor operator (P3-6) & laporan idle (P3-7) ----------

// AssignmentStats: raw numbers of one assignment window in the period (repository → usecase).
type AssignmentStats struct {
	AssignmentID string
	DriverID     string
	DriverName   string
	Imei         string
	PlannedStart int64
	PlannedEnd   int64
	WindowStart  int64 // start of the assignment inside the period
	WindowEnd    int64
	IdleS        float64  // mesin menyala, unit diam (detik)
	IdleRateLph  *float64 // CAN fuel rate saat idle (L/jam), nil = tidak ada CAN
	FirstMove    *int64   // record pertama dengan unit bergerak
	LastMove     *int64
	Overspeed    int // event overspeed Teltonika
	Harsh        int // green driving (rem / akselerasi / belok keras)
	Ritase       int // ritase hauling selesai
	CycleMs      *float64
	DumpMs       *float64 // rata-rata lama di dumping
}

// OperatorScore: rapor satu operator dalam periode.
type OperatorScore struct {
	DriverID    string   `json:"driverId"`
	DriverName  string   `json:"driverName"`
	Shifts      int      `json:"shifts"` // jumlah penugasan
	Ritase      int      `json:"ritase"`
	CycleMs     *float64 `json:"cycleMs"`
	DumpMs      *float64 `json:"dumpMs"`
	IdleMs      int64    `json:"idleMs"`
	Overspeed   int      `json:"overspeed"`
	Harsh       int      `json:"harsh"`
	LateStarts  int      `json:"lateStarts"`  // mulai bergerak > 15 mnt setelah jadwal
	LateStartMs int64    `json:"lateStartMs"` // total keterlambatan
	EarlyEnds   int      `json:"earlyEnds"`   // berhenti bergerak > 15 mnt sebelum jadwal selesai
	EarlyEndMs  int64    `json:"earlyEndMs"`
	Score       int      `json:"score"` // 0–100, makin tinggi makin baik
}

// IdleUnit: laporan idle per unit: mesin hidup tetapi diam, dan perkiraan BBM terbuang.
type IdleUnit struct {
	Imei       string  `json:"imei"`
	Unit       string  `json:"unit"`
	IdleMs     int64   `json:"idleMs"`
	FuelL      float64 `json:"fuelL"`
	FromCan    bool    `json:"fromCan"`    // BBM dari CAN fuel rate; false = perkiraan
	ShareOfJob float64 `json:"shareOfJob"` // persen idle dari waktu bertugas
}

type OperatorReport struct {
	From      int64           `json:"from"`
	To        int64           `json:"to"`
	Operators []OperatorScore `json:"operators"`
	Idle      []IdleUnit      `json:"idle"`
	IdleLph   float64         `json:"idleLph"` // konsumsi idle yang dipakai bila tanpa CAN
}
