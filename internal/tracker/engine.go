package tracker

import (
	"fmt"

	"github.com/amirullazmi0/fms-backend/internal/dto"
)

// Kesehatan mesin dari CAN (P3-5). Alert dibuat saat nilai MELEWATI ambang (bukan setiap record),
// jadi satu kejadian = satu alert. Tekanan oli tidak standar di LVCAN FMC150 (tergantung Program
// Number unit), jadi belum dibaca.
const (
	engineTempWarnC = 100.0
	engineTempCritC = 105.0
)

// engineAlerts: suhu mesin (IO 115, ×0,1 °C) dan jumlah kode error OBD (IO 160). Caller holds t.mu.
func engineAlerts(io, prev dto.IoMap, add func(level, kind, text string)) {
	if _, ok := io["115"]; ok {
		cur := num(io, "115") * 0.1
		before := -1000.0
		if _, had := prev["115"]; had {
			before = num(prev, "115") * 0.1
		}
		switch {
		case cur >= engineTempCritC && before < engineTempCritC:
			add("crit", "engine_temp", fmt.Sprintf("Suhu mesin kritis %.0f °C (batas %.0f °C), hentikan unit", cur, engineTempCritC))
		case cur >= engineTempWarnC && before < engineTempWarnC:
			add("warn", "engine_temp", fmt.Sprintf("Suhu mesin tinggi %.0f °C (batas %.0f °C)", cur, engineTempWarnC))
		}
	}
	if _, ok := io["160"]; ok {
		n := num(io, "160")
		if n > 0 && n != num(prev, "160") {
			add("warn", "engine_dtc", fmt.Sprintf("Kode error mesin (DTC) aktif: %.0f kode, cek di bengkel", n))
		}
	}
}
