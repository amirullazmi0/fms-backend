-- P3-8: view SQL read-only untuk analyst / Power BI. Semua di skema "analyst" dengan nama kolom
-- bahasa Indonesia, waktu dalam timestamptz (atur zona di klien). Role fms_readonly (NOLOGIN)
-- hanya boleh SELECT di skema ini. Buat login analyst:
--   CREATE USER analyst_bi WITH PASSWORD '...' IN ROLE fms_readonly;
CREATE SCHEMA IF NOT EXISTS "analyst";

CREATE OR REPLACE VIEW "analyst"."unit" AS
SELECT d.imei, COALESCE(NULLIF(d.plate, ''), NULLIF(d.name, ''), d.imei) AS unit, d.plate AS plat, d.name AS nama,
    d.unit_type::text AS jenis_unit, d.brand AS merek, d.model, d.tracker_model AS model_tracker,
    d.payload_ton AS truck_factor_ton, d.bucket_m3 AS kapasitas_bucket_m3, d.first_seen AS pertama_terlihat, d.last_seen AS terakhir_terlihat
FROM devices d WHERE d.deleted_at IS NULL;

CREATE OR REPLACE VIEW "analyst"."ritase" AS
SELECT t.id, t.depart_at AS berangkat, t.arrive_at AS tiba, s.date AS tanggal_shift, s.name AS shift,
    t.route_name AS rute, CASE t.leg WHEN 'go' THEN 'hauling' ELSE 'return' END AS arah,
    COALESCE(NULLIF(d.plate, ''), NULLIF(d.name, ''), t.imei) AS unit, t.imei, u.name AS operator,
    t.loading_name AS front_loading, t.unloading_name AS dumping,
    t.duration_ms / 60000.0 AS durasi_menit, t.distance_m / 1000.0 AS jarak_km, t.stops AS berhenti,
    t.max_speed AS kecepatan_maks, t.avg_speed AS kecepatan_rata, t.load_dwell_ms / 60000.0 AS lama_di_front_menit,
    t.unload_dwell_ms / 60000.0 AS lama_di_dumping_menit, t.payload_ton AS tonase, t.fuel_used AS bbm_terpakai,
    t.status::text AS status
FROM trips t
LEFT JOIN shifts s ON s.id = t.shift_id
LEFT JOIN devices d ON d.imei = t.imei
LEFT JOIN users u ON u.id = t.driver_id;

CREATE OR REPLACE VIEW "analyst"."status_unit" AS
SELECT l.id, COALESCE(NULLIF(d.plate, ''), NULLIF(d.name, ''), l.imei) AS unit, l.imei, u.name AS operator,
    l.status::text AS status, l.delay_code AS kode_delay, c.name AS nama_delay, c.category::text AS kategori,
    l.start_at AS mulai, l.end_at AS selesai,
    EXTRACT(EPOCH FROM COALESCE(l.end_at, now()) - l.start_at) / 60.0 AS durasi_menit, l.source::text AS sumber, l.note AS catatan
FROM unit_status_logs l
LEFT JOIN devices d ON d.imei = l.imei
LEFT JOIN users u ON u.id = l.driver_id
LEFT JOIN delay_codes c ON c.code = l.delay_code;

CREATE OR REPLACE VIEW "analyst"."kunjungan_titik" AS
SELECT v.id, COALESCE(NULLIF(d.plate, ''), NULLIF(d.name, ''), v.imei) AS unit, v.imei, u.name AS operator,
    r.name AS rute, v.site_name AS titik, CASE v.role WHEN 'loading' THEN 'front_loading' ELSE 'dumping' END AS peran,
    v.arrive_at AS masuk, v.depart_at AS keluar, v.dwell_ms / 60000.0 AS lama_menit,
    v.queue_ms / 60000.0 AS antre_menit, v.spot_ms / 60000.0 AS spotting_menit, v.work_ms / 60000.0 AS muat_bongkar_menit,
    v.status::text AS status
FROM site_visits v
LEFT JOIN devices d ON d.imei = v.imei
LEFT JOIN users u ON u.id = v.driver_id
LEFT JOIN routes r ON r.id = v.route_id;

CREATE OR REPLACE VIEW "analyst"."penugasan" AS
SELECT a.id, s.date AS tanggal_shift, s.name AS shift, r.name AS rute,
    COALESCE(NULLIF(d.plate, ''), NULLIF(d.name, ''), a.imei) AS unit, a.imei, u.name AS operator,
    COALESCE(a.start_at, s.started_at) AS mulai, COALESCE(LEAST(a.end_at, s.closed_at), a.end_at, s.closed_at) AS selesai
FROM assignments a
JOIN shifts s ON s.id = a.shift_id AND s.deleted_at IS NULL
LEFT JOIN routes r ON r.id = a.route_id
LEFT JOIN devices d ON d.imei = a.imei
LEFT JOIN users u ON u.id = a.driver_id;

CREATE OR REPLACE VIEW "analyst"."target_produksi" AS
SELECT r.name AS rute, t.rit_per_hour AS ritase_per_jam, t.ton_per_hour AS tonase_per_jam,
    t.rit_per_shift AS ritase_per_shift, t.ton_per_shift AS tonase_per_shift, t.valid_from AS berlaku_mulai
FROM production_targets t JOIN routes r ON r.id = t.route_id
WHERE t.deleted_at IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fms_readonly') THEN
        CREATE ROLE fms_readonly NOLOGIN;
    END IF;
END
$$;
GRANT USAGE ON SCHEMA "analyst" TO fms_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA "analyst" TO fms_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA "analyst" GRANT SELECT ON TABLES TO fms_readonly;
