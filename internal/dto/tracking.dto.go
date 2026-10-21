package dto

// Live tracking types. Field names match fmctest/server/types.ts so the dashboard
// logic ports over unchanged. All times are epoch milliseconds.

// IoMap is AVL IO ID -> value. Numbers are float64, 8-byte values beyond
// 2^53 and NX (variable length) values are strings.
type IoMap map[string]any

// AvlHead is an AVL record without its IO values.
type AvlHead struct {
	Time     int64   `json:"time"`
	Priority int     `json:"priority"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Alt      int     `json:"alt"`
	Angle    int     `json:"angle"`
	Sats     int     `json:"sats"`
	Speed    int     `json:"speed"`
	EventIo  int     `json:"eventIo"`
	Valid    bool    `json:"valid"`
	// ReceivedAt: when the server stored it (Time is the GPS time). 0 = not read from the database.
	ReceivedAt int64 `json:"receivedAt,omitempty"`
}

type AvlRecord struct {
	AvlHead
	Io IoMap `json:"io"`
}

type HistoryPoint struct {
	T     int64   `json:"t"`
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	V     bool    `json:"v"`
	Speed int     `json:"speed"`
	Angle int     `json:"angle"`
	Sats  int     `json:"sats"`
	Ign   any     `json:"ign,omitempty"`
	Ext   any     `json:"ext,omitempty"`
}

type DevicePos struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Angle int     `json:"angle"`
	Time  int64   `json:"time"`
}

type Device struct {
	Imei       string  `json:"imei"`
	Name       *string `json:"name"`
	Plate      *string `json:"plate"`
	Vehicle    *string `json:"vehicle"`
	PayloadTon float64 `json:"payloadTon"` // truck factor: ton per rit
	UnitType   string  `json:"unitType"`   // DT | EX | LV | SUPPORT
	// P2-4: merek & model unit, model tracker (FMC150 / FMC650 / ...), kapasitas bucket (excavator, m³).
	Brand        *string          `json:"brand"`
	Model        *string          `json:"model"`
	TrackerModel *string          `json:"trackerModel"`
	BucketM3     *float64         `json:"bucketM3"`
	Online       bool             `json:"online"`
	Remote       *string          `json:"remote"`
	ConnectedAt  *int64           `json:"connectedAt"`
	LastSeen     *int64           `json:"lastSeen"`
	FirstSeen    *int64           `json:"firstSeen"`
	Last         *AvlHead         `json:"last"`
	Pos          *DevicePos       `json:"pos"`
	Io           IoMap            `json:"io"`
	IoTime       map[string]int64 `json:"ioTime"`
	Records      int              `json:"records"`
}

type DeviceDetail struct {
	Device
	History []HistoryPoint `json:"history"`
}

type UpdateDeviceRequest struct {
	Name    *string `json:"name" validate:"omitempty,max=60"`
	Plate   *string `json:"plate" validate:"omitempty,max=60"`
	Vehicle *string `json:"vehicle" validate:"omitempty,max=60"`
	// PayloadTon: truck factor (ton per rit).
	PayloadTon *float64 `json:"payloadTon" validate:"omitempty,gt=0,lte=500"`
	UnitType   *string  `json:"unitType" validate:"omitempty,oneof=DT EX LV SUPPORT"`
	// "" clears brand / model / trackerModel; bucketM3 0 clears it.
	Brand        *string  `json:"brand" validate:"omitempty,max=60"`
	Model        *string  `json:"model" validate:"omitempty,max=60"`
	TrackerModel *string  `json:"trackerModel" validate:"omitempty,max=30"`
	BucketM3     *float64 `json:"bucketM3" validate:"omitempty,gte=0,lte=100"`
}

type CommandRequest struct {
	Cmd string `json:"cmd" validate:"required,max=500"`
}

type CommandResponse struct {
	Response string `json:"response"`
}

type FleetEvent struct {
	Time  int64  `json:"time"`
	Imei  string `json:"imei"`
	Level string `json:"level"` // info | warn | crit
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

type LogEntry struct {
	ID    int64   `json:"id"`
	Time  int64   `json:"time"`
	Kind  string  `json:"kind"` // conn | raw | data | ack | cmd | error
	Imei  *string `json:"imei"`
	Level string  `json:"level"` // info | warn | error
	Text  string  `json:"text"`
}

type CommandLine struct {
	Imei string `json:"imei"`
	Dir  string `json:"dir"` // in | out | err
	Text string `json:"text"`
	Time int64  `json:"time"`
}

type TunnelInfo struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Provider string `json:"provider"`
}

type DBCounts struct {
	Devices int `json:"devices"`
	Records int `json:"records"`
	Events  int `json:"events"`
}

type ServerStatus struct {
	StartedAt int64       `json:"startedAt"`
	Port      int         `json:"port"`
	LanIps    []string    `json:"lanIps"`
	Tunnel    *TunnelInfo `json:"tunnel"`
	// TunnelEnabled: TUNNEL=true. false = deployed, devices dial the server directly.
	TunnelEnabled bool     `json:"tunnelEnabled"`
	DevicePort    int      `json:"devicePort"`
	DB            DBCounts `json:"db"`
	Connected     int      `json:"connected"`
	Devices       int      `json:"devices"`
}

type DeviceMeta struct {
	Name    *string
	Plate   *string
	Vehicle *string
	// nil = keep the stored value
	PayloadTon   *float64
	UnitType     *string
	Brand        *string
	Model        *string
	TrackerModel *string
	BucketM3     *float64
}

// DeviceRow is a devices table row plus its record count.
type DeviceRow struct {
	Imei            string
	Name            *string
	Plate           *string
	Vehicle         *string
	PayloadTon      float64
	UnitType        string
	Brand           *string
	Model           *string
	TrackerModel    *string
	BucketM3        *float64
	FirstSeen       int64
	LastSeen        *int64
	LastConnectedAt *int64
	LastRemote      *string
	Records         int
}

type PlaceResponse struct {
	Place *string `json:"place"`
}

type DayQuery struct {
	From int64 `form:"from"`
	To   int64 `form:"to"`
}

type RecordsQuery struct {
	From  int64 `form:"from"`
	To    int64 `form:"to"`
	Limit int   `form:"limit"`
}

// RawRecordsQuery pages through the stored records, newest first.
type RawRecordsQuery struct {
	From  int64 `form:"from"`
	To    int64 `form:"to"`
	Page  int   `form:"page"`
	Limit int   `form:"limit"`
}

// DeviceInstall: satu periode tracker (IMEI) terpasang di satu unit (plat). RemovedAt nil = masih terpasang.
type DeviceInstall struct {
	ID          string  `json:"id"`
	Imei        string  `json:"imei"`
	Plate       string  `json:"plate"`
	Name        *string `json:"name"`
	InstalledAt int64   `json:"installedAt"`
	RemovedAt   *int64  `json:"removedAt"`
}
