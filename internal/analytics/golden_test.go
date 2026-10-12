package analytics

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

// golden.json is produced by testdata/gen_golden.ts from the demo's own TypeScript
// (fmctest/server/trips.ts + sites.ts). The Go port must give identical results.
type golden struct {
	Sites []dto.Site  `json:"sites"`
	Route []dto.Route `json:"routes"`
	Cases []struct {
		Seed    int             `json:"seed"`
		From    int64           `json:"from"`
		To      int64           `json:"to"`
		Now     int64           `json:"now"`
		Records []dto.AvlRecord `json:"records"`
		Day     json.RawMessage `json:"day"`
		Report  json.RawMessage `json:"report"`
	} `json:"cases"`
	History json.RawMessage `json:"history"`
}

// normalize re-encodes through a generic value so key order / number formatting match.
func normalize(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func rawValue(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// closeEnough compares JSON trees, allowing float rounding noise in the last digits.
func closeEnough(path string, got, want any, t *testing.T) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			t.Errorf("%s: got %v, want %v", path, got, want)
			return false
		}
		ok = true
		for k := range w {
			ok = closeEnough(path+"."+k, g[k], w[k], t) && ok
		}
		return ok
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			t.Errorf("%s: length got %d, want %d", path, len(g), len(w))
			return false
		}
		ok = true
		for i := range w {
			ok = closeEnough(path+"["+itoa(i)+"]", g[i], w[i], t) && ok
		}
		return ok
	case float64:
		g, ok := got.(float64)
		diff := g - w
		if diff < 0 {
			diff = -diff
		}
		scale := w
		if scale < 0 {
			scale = -scale
		}
		if !ok || diff > 1e-9*(1+scale) {
			t.Errorf("%s: got %v, want %v", path, got, want)
			return false
		}
		return true
	default:
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", path, got, want)
			return false
		}
		return true
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestGoldenAgainstDemo(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Skip("testdata/golden.json missing (run testdata/gen_golden.ts)")
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	perTruck := []TruckRecords{}
	for _, c := range g.Cases {
		day := SummarizeDay(c.Records, c.From, c.To)
		closeEnough("day", normalize(t, day), rawValue(t, c.Day), t)

		rep := BuildReport(c.Records, g.Route, g.Sites, c.From, c.To, c.Now)
		closeEnough("report", normalize(t, rep), rawValue(t, c.Report), t)
		if len(rep.Trips) == 0 {
			t.Errorf("seed %d: expected ritase in the synthetic track", c.Seed)
		}
		perTruck = append(perTruck, TruckRecords{Imei: "35000000000000" + itoa(c.Seed), Records: c.Records})
	}
	first, last := g.Cases[0], g.Cases[len(g.Cases)-1]
	h := RouteHistory(g.Route[0], g.Sites, perTruck, first.From, last.To, first.Now, 1)
	closeEnough("history", normalize(t, h), rawValue(t, g.History), t)
}
