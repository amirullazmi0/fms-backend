package teltonika

// AVL IO ID -> name, unit, scale (FMB/FMC family). Check against the FMC150
// "Data Sending Parameters ID" table before relying on it in production.
// Size = byte size (used by the simulator), Scale = multiplier for the raw value.

type IoDef struct {
	Name   string            `json:"name"`
	Group  string            `json:"group"`
	Size   int               `json:"size"`
	Unit   string            `json:"unit,omitempty"`
	Scale  float64           `json:"scale,omitempty"`
	Kind   string            `json:"kind,omitempty"` // bool | hex | id
	Enum   map[string]string `json:"enum,omitempty"`
	Signed bool              `json:"signed,omitempty"`
}

var IO = map[int]IoDef{
	// Device status
	239: {Name: "Ignition", Group: "status", Size: 1, Kind: "bool"},
	240: {Name: "Movement", Group: "status", Size: 1, Kind: "bool"},
	80:  {Name: "Data Mode", Group: "status", Size: 1, Enum: map[string]string{"0": "Home, diam", "1": "Home, jalan", "2": "Roaming, diam", "3": "Roaming, jalan", "4": "Unknown, diam", "5": "Unknown, jalan"}},
	200: {Name: "Sleep Mode", Group: "status", Size: 1, Enum: map[string]string{"0": "Tidak tidur", "1": "GPS sleep", "2": "Deep sleep", "3": "Online sleep", "4": "Ultra sleep"}},
	21:  {Name: "GSM Signal", Group: "status", Size: 1, Unit: "/5"},
	241: {Name: "Operator GSM", Group: "status", Size: 4, Kind: "id", Enum: map[string]string{"51010": "Telkomsel (51010)", "51001": "Indosat (51001)", "51021": "Indosat (51021)", "51011": "XL (51011)", "51089": "Tri (51089)", "51009": "Smartfren (51009)", "51028": "Smartfren (51028)"}},
	205: {Name: "GSM Cell ID", Group: "status", Size: 2, Kind: "id"},
	206: {Name: "GSM Area Code", Group: "status", Size: 2, Kind: "id"},

	// GNSS
	69:  {Name: "GNSS Status", Group: "gnss", Size: 1, Enum: map[string]string{"0": "Mati", "1": "Fix", "2": "Belum fix", "3": "Sleep"}},
	181: {Name: "GNSS PDOP", Group: "gnss", Size: 2, Scale: 0.1},
	182: {Name: "GNSS HDOP", Group: "gnss", Size: 2, Scale: 0.1},
	24:  {Name: "Speed (GPS)", Group: "gnss", Size: 2, Unit: "km/h"},
	16:  {Name: "Total Odometer", Group: "gnss", Size: 4, Scale: 0.001, Unit: "km"},
	199: {Name: "Trip Odometer", Group: "gnss", Size: 4, Scale: 0.001, Unit: "km"},

	// Power
	66:  {Name: "External Voltage", Group: "power", Size: 2, Scale: 0.001, Unit: "V"},
	67:  {Name: "Battery Voltage", Group: "power", Size: 2, Scale: 0.001, Unit: "V"},
	68:  {Name: "Battery Current", Group: "power", Size: 2, Unit: "mA"},
	113: {Name: "Battery Level", Group: "power", Size: 1, Unit: "%"},

	// Physical I/O
	1:   {Name: "DIN1", Group: "io", Size: 1, Kind: "bool"},
	2:   {Name: "DIN2", Group: "io", Size: 1, Kind: "bool"},
	3:   {Name: "DIN3", Group: "io", Size: 1, Kind: "bool"},
	9:   {Name: "AIN1", Group: "io", Size: 2, Scale: 0.001, Unit: "V"},
	6:   {Name: "AIN2", Group: "io", Size: 2, Scale: 0.001, Unit: "V"},
	179: {Name: "DOUT1", Group: "io", Size: 1, Kind: "bool"},
	180: {Name: "DOUT2", Group: "io", Size: 1, Kind: "bool"},
	78:  {Name: "iButton", Group: "io", Size: 8, Kind: "hex"},
	72:  {Name: "Dallas Temperature 1", Group: "io", Size: 4, Scale: 0.1, Unit: "°C", Signed: true},

	// Driving behaviour & events
	253: {Name: "Green Driving Type", Group: "driving", Size: 1, Enum: map[string]string{"1": "Akselerasi kasar", "2": "Pengereman kasar", "3": "Belok kasar"}},
	254: {Name: "Green Driving Value", Group: "driving", Size: 2, Scale: 0.01, Unit: "G"},
	255: {Name: "Overspeeding", Group: "driving", Size: 1, Unit: "km/h"},
	251: {Name: "Idling", Group: "driving", Size: 1, Kind: "bool"},
	250: {Name: "Trip", Group: "driving", Size: 1, Enum: map[string]string{"0": "Selesai", "1": "Mulai"}},
	247: {Name: "Crash Detection", Group: "driving", Size: 1},
	246: {Name: "Towing", Group: "driving", Size: 1, Kind: "bool"},
	252: {Name: "Unplug", Group: "driving", Size: 1, Kind: "bool"},
	249: {Name: "Jamming", Group: "driving", Size: 1, Kind: "bool"},
	17:  {Name: "Axis X", Group: "driving", Size: 2, Unit: "mG", Signed: true},
	18:  {Name: "Axis Y", Group: "driving", Size: 2, Unit: "mG", Signed: true},
	19:  {Name: "Axis Z", Group: "driving", Size: 2, Unit: "mG", Signed: true},

	// CAN (depends on vehicle and Program Number)
	81:  {Name: "CAN Vehicle Speed", Group: "can", Size: 1, Unit: "km/h"},
	82:  {Name: "CAN Accelerator Pedal", Group: "can", Size: 1, Unit: "%"},
	83:  {Name: "CAN Fuel Consumed", Group: "can", Size: 4, Scale: 0.1, Unit: "L"},
	84:  {Name: "CAN Fuel Level", Group: "can", Size: 2, Scale: 0.1, Unit: "L"},
	85:  {Name: "CAN Engine RPM", Group: "can", Size: 2, Unit: "rpm"},
	87:  {Name: "CAN Total Mileage", Group: "can", Size: 4, Scale: 0.001, Unit: "km"},
	89:  {Name: "CAN Fuel Level %", Group: "can", Size: 1, Unit: "%"},
	110: {Name: "CAN Fuel Rate", Group: "can", Size: 2, Scale: 0.1, Unit: "L/h"},
	115: {Name: "CAN Engine Temperature", Group: "can", Size: 2, Scale: 0.1, Unit: "°C", Signed: true},
	100: {Name: "CAN Program Number", Group: "can", Size: 4, Kind: "id"},
}

var Groups = map[string]string{
	"status":  "Status alat",
	"gnss":    "GPS",
	"power":   "Power",
	"io":      "Input/Output",
	"driving": "Perilaku & event",
	"can":     "CAN kendaraan",
}

func isSigned(id int) bool {
	d, ok := IO[id]
	return ok && d.Signed
}
