# fms-backend

## Koneksi alat (tracker → backend)

Alat tidak memakai endpoint HTTP. Alat membuka koneksi **TCP** langsung ke backend:

| Item | Nilai |
|---|---|
| Host (staging) | `fms-backend.apps.arulize.com` |
| Port | `3101` (`DEVICE_PORT`), TCP (bukan UDP) |

Satu port melayani **dua format**. Server melihat byte pertama yang masuk:

- `{` atau `[` → **jalur JSON** (tracker non-Teltonika / sistem pihak ketiga)
- selain itu → **jalur Teltonika** (FMC150 dan keluarga FMB/FMC)

Kode: `internal/tracker/conn.go` (Teltonika) dan `internal/tracker/jsonconn.go` (JSON).
Swagger integrasi tracker: `/swagger-fmc` (spec di `internal/handler/docs/fmc-openapi.yaml`, ikut di-update kalau protokol berubah).

### Jalur Teltonika (Codec 8 / 8E / 12)

1. Alat kirim handshake: 2 byte panjang + IMEI 15 digit (ASCII).
2. Server balas `01` (diterima) atau `00` (ditolak, mis. IMEI bukan 15 digit).
3. Alat kirim paket AVL Codec 8 / 8E. Server simpan ke database dulu, baru balas ACK 4 byte (jumlah record).
   Tanpa ACK, alat akan mengirim ulang.
4. Perintah dari dashboard dikirim lewat Codec 12 di koneksi yang sama.

Data yang bukan protokol Teltonika (dan bukan JSON) ditolak dengan log `Tolak <ip>: bukan protokol Teltonika`
dan koneksi diputus (di sisi pengirim muncul sebagai `connection reset` / `WinError 10054`).

### Jalur JSON

Untuk alat atau sistem yang tidak bisa bicara Teltonika. Kirim objek JSON langsung lewat TCP,
satu atau beberapa objek per koneksi (boleh dipisah newline). Boleh buka-tutup koneksi tiap kirim.

```json
{"data":{"epoch":1794556916,"device_id":50010,"latitude":-6.190866,"longitude":106.764735,
         "altitude":62.1,"speed":0.06,"io1":0,"io2":1,"io3":1,"io4":1,
         "flag":"1","rs232":"","rs485":""}}
```

Bentuk lain yang juga diterima: `"data"` berupa array, objek tanpa pembungkus `data`, atau array di level atas.

| Field | Wajib | Dipetakan ke |
|---|---|---|
| `device_id` | ya | ID alat (pengganti IMEI). Angka atau string, `A-Z a-z 0-9 _ -`, maks 32 karakter |
| `epoch` | tidak | Waktu GPS. Detik atau milidetik (otomatis). Kosong = waktu server |
| `latitude`, `longitude` | ya | Posisi. `0,0` dianggap tidak valid (tidak menggeser posisi di peta) |
| `altitude` | tidak | Ketinggian (m, dibulatkan) |
| `speed` | tidak | Kecepatan (km/h, dibulatkan) |
| `angle` | tidak | Arah (derajat) |
| `satellites` | tidak | Jumlah satelit |
| `io1`–`io4` | tidak | DIN1–DIN4 (AVL ID 1–4) |
| `flag`, `rs232`, `rs485` | tidak | Belum disimpan |

Balasan server, satu baris per objek:

```json
{"status":"ok","saved":1}
{"status":"error","error":"device_id tidak valid ..."}
```

`saved` bisa `0` kalau record yang sama (alat + `epoch` sama) sudah ada di database: kiriman ulang aman.

Perilaku alat JSON:

- Alat baru otomatis terdaftar saat data pertama masuk (event "Alat baru terdaftar (JSON)").
- Di API HTTP, `{imei}` = `device_id` alat JSON (mis. `GET /api/devices/50010`). Validasi ID alat di HTTP
  memakai tag `device_id` (`pkg/validator`): IMEI 15 digit atau `A-Z a-z 0-9 _ -` maks 32 karakter.
  Handshake Teltonika tetap wajib IMEI 15 digit.
- Status online **tidak mengikuti socket**: alat online selama data masuk, offline setelah 3 menit tanpa data
  (`jsonOfflineAfter`).
- Perintah dari dashboard (Codec 12) tidak bisa dikirim ke alat JSON.
- Belum ada pemetaan ke Ignition (AVL 239). Kalau salah satu `io` adalah ACC/ignition, petakan di `jsonRecord.toAvl`,
  karena trip dan jam mesin bergantung ke sinyal itu.

### Tes cepat

```bash
# JSON (bash, butuh netcat)
echo '{"data":{"device_id":"TEST1","latitude":-6.19,"longitude":106.76,"speed":10}}' | nc fms-backend.apps.arulize.com 3101

# Teltonika: pakai simulator
go run ./cmd/simulator
```
