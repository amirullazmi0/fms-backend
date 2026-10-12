package teltonika

import (
	"encoding/hex"
	"testing"
)

// Codec 8 example from the Teltonika wiki (1 record, 5 IO elements).
const wikiCodec8 = "000000000000003608010000016B40D8EA30010000000000000000000000000000000105021503010101425E0F01F10000601A014E0000000000000000010000C7CF"

func TestParseWikiCodec8(t *testing.T) {
	frame, _ := hex.DecodeString(wikiCodec8)
	if !CRCOk(frame) {
		t.Fatal("CRC of the wiki sample should be valid")
	}
	p, err := ParseAvl(frame)
	if err != nil {
		t.Fatal(err)
	}
	if p.Codec != Codec8 || p.Count != 1 || len(p.Records) != 1 {
		t.Fatalf("unexpected header: %+v", p)
	}
	r := p.Records[0]
	if r.Time != 1560161086000 || r.Priority != 1 || r.EventIo != 1 {
		t.Fatalf("unexpected record head: %+v", r.AvlHead)
	}
	want := map[string]float64{"21": 3, "1": 1, "66": 24079, "241": 24602, "78": 0}
	for id, v := range want {
		if r.Io[id] != v {
			t.Errorf("io[%s] = %v, want %v", id, r.Io[id], v)
		}
	}
	if r.Valid {
		t.Error("0,0 position must be invalid")
	}
}

func TestBuildAvl8ERoundTrip(t *testing.T) {
	rec := OutRecord{
		Time: 1700000000123, Priority: 1, Lat: -6.2088123, Lng: 106.8456789, Alt: 12, Angle: 270, Sats: 11, Speed: 54, EventIo: 239,
		Io: []OutIo{{239, 1, 1}, {66, 13800, 2}, {72, -15, 4}, {16, 125000000, 4}, {78, 0x010000001234, 8}},
	}
	frame := BuildAvl8E([]OutRecord{rec})
	if !CRCOk(frame) {
		t.Fatal("built frame has a bad CRC")
	}
	p, err := ParseAvl(frame)
	if err != nil {
		t.Fatal(err)
	}
	got := p.Records[0]
	if p.Codec != Codec8E || got.Time != rec.Time || got.Speed != 54 || got.Angle != 270 || got.EventIo != 239 || !got.Valid {
		t.Fatalf("round trip mismatch: %+v", got.AvlHead)
	}
	if got.Lat != -6.2088123 || got.Lng != 106.8456789 {
		t.Fatalf("position mismatch: %v,%v", got.Lat, got.Lng)
	}
	// IO 72 is signed: -15 must come back negative.
	if got.Io["72"] != float64(-15) || got.Io["66"] != float64(13800) || got.Io["78"] != float64(0x010000001234) {
		t.Fatalf("io mismatch: %v", got.Io)
	}
}

func TestCodec12RoundTrip(t *testing.T) {
	frame := BuildCodec12("getinfo", Codec12Command)
	if !CRCOk(frame) {
		t.Fatal("bad CRC")
	}
	typ, text, err := ParseCodec12(frame)
	if err != nil || typ != Codec12Command || text != "getinfo" {
		t.Fatalf("got %d %q %v", typ, text, err)
	}
	// Codec 12 "getinfo" command from the Teltonika wiki.
	wiki, _ := hex.DecodeString("000000000000000F0C010500000007676574696E666F0100004312")
	if hex.EncodeToString(frame) != hex.EncodeToString(wiki) {
		t.Fatalf("frame differs from the wiki sample:\n got %x\nwant %x", frame, wiki)
	}
}

func TestTakeFrame(t *testing.T) {
	frame, _ := hex.DecodeString(wikiCodec8)
	buf := append(append([]byte{}, frame...), frame[:5]...)
	got, rest, ok, err := TakeFrame(buf)
	if err != nil || !ok || len(got) != len(frame) || len(rest) != 5 {
		t.Fatalf("TakeFrame: ok=%v err=%v len=%d rest=%d", ok, err, len(got), len(rest))
	}
	if _, _, ok, _ := TakeFrame(rest); ok {
		t.Fatal("partial frame must wait for more bytes")
	}
}
