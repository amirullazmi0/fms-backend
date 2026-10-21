package tracker

import (
	"testing"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

func TestEngineAlerts(t *testing.T) {
	var got []string
	add := func(level, kind, text string) { got = append(got, level+"|"+kind) }

	engineAlerts(dto.IoMap{"115": 950.0}, dto.IoMap{"115": 940.0}, add)   // 95 °C: normal
	engineAlerts(dto.IoMap{"115": 1010.0}, dto.IoMap{"115": 990.0}, add)  // naik lewat 100: warn
	engineAlerts(dto.IoMap{"115": 1020.0}, dto.IoMap{"115": 1010.0}, add) // tetap di atas: tidak ulang
	engineAlerts(dto.IoMap{"115": 1060.0}, dto.IoMap{"115": 1020.0}, add) // lewat 105: crit
	engineAlerts(dto.IoMap{"160": 2.0}, dto.IoMap{"160": 0.0}, add)       // DTC muncul
	engineAlerts(dto.IoMap{"160": 2.0}, dto.IoMap{"160": 2.0}, add)       // DTC sama: tidak ulang

	want := []string{"warn|engine_temp", "crit|engine_temp", "warn|engine_dtc"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
