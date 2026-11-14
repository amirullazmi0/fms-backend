package tracker

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeJSONPayload(t *testing.T) {
	raw := `{"data":{"epoch":1794556916,"device_id":50010,"latitude":-6.190866,"longitude":106.764735,"altitude":62.1,"speed":0.06,"io1":0,"io2":1,"io3":1,"io4":1,"flag":"1","rs232":"","rs485":""}}`
	items, err := decodeJSONPayload(json.RawMessage(raw))
	if err != nil || len(items) != 1 {
		t.Fatalf("decode: %v %d", err, len(items))
	}
	id, ok := jsonDeviceID(items[0].DeviceID)
	if !ok || id != "50010" {
		t.Fatalf("device id %q %v", id, ok)
	}
	rec, err := items[0].toAvl(time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Time != 1794556916000 || !rec.Valid || rec.Alt != 62 || rec.Speed != 0 {
		t.Fatalf("record %+v", rec.AvlHead)
	}
	if rec.Io["1"] != 0.0 || rec.Io["4"] != 1.0 {
		t.Fatalf("io %+v", rec.Io)
	}

	for _, in := range []string{
		`{"data":[{"device_id":"A1","latitude":1,"longitude":2},{"device_id":"A1","latitude":1,"longitude":3}]}`,
		`[{"device_id":"A1","latitude":1,"longitude":2},{"device_id":"A1","latitude":1,"longitude":3}]`,
	} {
		items, err := decodeJSONPayload(json.RawMessage(in))
		if err != nil || len(items) != 2 {
			t.Fatalf("%s: %v %d", in, err, len(items))
		}
	}
	if _, ok := jsonDeviceID("../x"); ok {
		t.Fatal("bad id accepted")
	}
}
