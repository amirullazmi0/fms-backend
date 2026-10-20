package tracker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amirullazmi0/fms-backend/internal/dto"
	"github.com/amirullazmi0/fms-backend/pkg/teltonika"
)

// truthy mirrors JavaScript truthiness for IO values.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case float64:
		return x != 0
	case string:
		return x != ""
	case bool:
		return x
	}
	return true
}

func num(io dto.IoMap, id string) float64 {
	switch x := io[id].(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

// jsString formats an IO value like a JS template literal.
func jsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "undefined"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		return x
	}
	return fmt.Sprint(v)
}

// changed: id present now and before, and different.
func changed(io, prev dto.IoMap, id string) bool {
	a, ok1 := io[id]
	b, ok2 := prev[id]
	return ok1 && ok2 && a != b
}

// ibuttonHex shows an iButton ID (8 bytes) as 16 hex digits.
func ibuttonHex(v any) string {
	if f, ok := v.(float64); ok {
		s := strings.ToUpper(strconv.FormatUint(uint64(f), 16))
		return strings.Repeat("0", max(0, 16-len(s))) + s
	}
	return strings.ToUpper(jsString(v))
}

// detectEvents derives fleet events from one new record. Caller holds t.mu.
func (t *Tracker) detectEvents(d *deviceState, prevIo dto.IoMap, rec dto.AvlRecord) {
	add := func(level, kind, text string) {
		t.pushEvent(dto.FleetEvent{Time: rec.Time, Imei: d.Imei, Level: level, Kind: kind, Text: text})
	}
	io := rec.Io

	if changed(io, prevIo, "239") {
		if truthy(io["239"]) {
			add("info", "ignition", "Ignition ON")
		} else {
			add("info", "ignition", "Ignition OFF")
		}
	}
	if changed(io, prevIo, "2") {
		if truthy(io["2"]) {
			add("info", "din2", "Input 2 aktif (mis. pintu bak dibuka)")
		} else {
			add("info", "din2", "Input 2 nonaktif (mis. pintu bak ditutup)")
		}
	}
	if changed(io, prevIo, "3") {
		if truthy(io["3"]) {
			add("info", "din3", "Input 3 aktif")
		} else {
			add("info", "din3", "Input 3 nonaktif")
		}
	}
	if changed(io, prevIo, "179") {
		if truthy(io["179"]) {
			add("warn", "dout1", "Pemutus mesin AKTIF (DOUT1 ON)")
		} else {
			add("info", "dout1", "Pemutus mesin dilepas (DOUT1 OFF)")
		}
	}
	if v, ok := io["78"]; ok && v != prevIo["78"] && num(io, "78") != 0 {
		add("info", "ibutton", "Operator tap iButton "+ibuttonHex(v))
	}

	switch rec.EventIo {
	case 253:
		typ := teltonika.IO[253].Enum[jsString(num(io, "253"))]
		if typ == "" {
			typ = "Green driving " + jsString(io["253"])
		}
		g := ""
		if io["254"] != nil {
			g = fmt.Sprintf(" (%.2f G)", num(io, "254")*0.01)
		}
		add("warn", "green", typ+g)
	case 255:
		v := io["255"]
		if v == nil {
			v = float64(rec.Speed)
		}
		add("warn", "overspeed", "Overspeed "+jsString(v)+" km/h")
	case 247:
		if num(io, "247") != 0 {
			add("crit", "crash", "Terdeteksi tabrakan")
		}
	case 246:
		if num(io, "246") != 0 {
			add("crit", "towing", "Unit diderek")
		}
	case 252:
		if num(io, "252") != 0 {
			add("crit", "unplug", "Alat dicabut dari power")
		}
	case 249:
		if num(io, "249") != 0 {
			add("crit", "jamming", "Sinyal GSM di-jamming")
		}
	case 251:
		if num(io, "251") != 0 {
			add("warn", "idling", "Idling, mesin nyala tapi diam")
		}
	}
}

// describeRecord is the one-line log text of a record.
func describeRecord(rec dto.AvlRecord, codec byte) string {
	ids := make([]int, 0, len(rec.Io))
	for id := range rec.Io {
		n, _ := strconv.Atoi(id)
		ids = append(ids, n)
	}
	sortInts(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		name := strconv.Itoa(id)
		if def, ok := teltonika.IO[id]; ok {
			name = strings.Join(strings.Fields(def.Name), "")
		}
		parts = append(parts, name+"="+jsString(rec.Io[strconv.Itoa(id)]))
	}
	pos := "no-fix"
	if rec.Valid {
		pos = fmt.Sprintf("%.6f,%.6f", rec.Lat, rec.Lng)
	}
	codecName := "8"
	if codec == teltonika.Codec8E {
		codecName = "8E"
	}
	return fmt.Sprintf("Codec%s %s pos=%s spd=%d ang=%d sat=%d pri=%d evt=%d | %s",
		codecName, isoTime(rec.Time), pos, rec.Speed, rec.Angle, rec.Sats, rec.Priority, rec.EventIo, strings.Join(parts, " "))
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
