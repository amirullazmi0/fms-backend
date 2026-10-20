-- Status unit & delay code (dasar PA/UA). Sumber: MDT di kabin, input supervisor, deteksi otomatis, radio DMR.

CREATE TYPE "unit_status" AS ENUM ('working', 'standby', 'breakdown');
CREATE TYPE "delay_category" AS ENUM ('working', 'standby_planned', 'standby_unplanned', 'breakdown');
CREATE TYPE "unit_status_source" AS ENUM ('auto', 'supervisor', 'dmr', 'mdt');

CREATE TABLE "delay_codes" (
    "code" VARCHAR(20) NOT NULL,
    "name" VARCHAR(100) NOT NULL,
    "category" "delay_category" NOT NULL,
    "icon" VARCHAR(40),
    "sort" INTEGER NOT NULL DEFAULT 0,
    "active" BOOLEAN NOT NULL DEFAULT true,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "created_by" UUID,
    "updated_by" UUID,

    CONSTRAINT "delay_codes_pkey" PRIMARY KEY ("code")
);

-- id dibuat di tablet MDT (uuid) supaya kiriman ulang dari antrean offline nggak dobel.
CREATE TABLE "unit_status_logs" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "imei" TEXT NOT NULL,
    "driver_id" UUID,
    "assignment_id" UUID,
    "status" "unit_status" NOT NULL,
    "delay_code" VARCHAR(20),
    "start_at" TIMESTAMPTZ(3) NOT NULL,
    "end_at" TIMESTAMPTZ(3),
    "source" "unit_status_source" NOT NULL,
    "note" TEXT,
    "client_at" TIMESTAMPTZ(3),
    "received_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "created_by" UUID,

    CONSTRAINT "unit_status_logs_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "unit_status_logs_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION,
    CONSTRAINT "unit_status_logs_driver_id_fkey" FOREIGN KEY ("driver_id") REFERENCES "drivers"("id") ON DELETE SET NULL ON UPDATE NO ACTION,
    CONSTRAINT "unit_status_logs_assignment_id_fkey" FOREIGN KEY ("assignment_id") REFERENCES "assignments"("id") ON DELETE SET NULL ON UPDATE NO ACTION,
    CONSTRAINT "unit_status_logs_delay_code_fkey" FOREIGN KEY ("delay_code") REFERENCES "delay_codes"("code") ON DELETE SET NULL ON UPDATE CASCADE
);

CREATE INDEX "unit_status_logs_imei_start" ON "unit_status_logs"("imei", "start_at");
CREATE INDEX "unit_status_logs_start_at" ON "unit_status_logs"("start_at");

INSERT INTO "delay_codes" ("code", "name", "category", "icon", "sort")
SELECT v.code, v.name, v.category::"delay_category", v.icon, v.sort FROM (VALUES
    ('HAUL',    'Hauling',                  'working',           'truck',           10),
    ('LOAD',    'Loading',                  'working',           'download',        11),
    ('DUMP',    'Dumping',                  'working',           'upload',          12),
    ('RETURN',  'Return (Kosongan)',        'working',           'undo',            13),
    ('P2H',     'P2H',                      'standby_planned',   'clipboard-check', 20),
    ('ISHOMA',  'Ishoma',                   'standby_planned',   'utensils',        21),
    ('SHOLAT',  'Sholat',                   'standby_planned',   'moon',            22),
    ('GSHIFT',  'Ganti Shift',              'standby_planned',   'repeat',          23),
    ('STALK',   'Safety Talk',              'standby_planned',   'shield',          24),
    ('Q_LOAD',  'Antre Loading',            'standby_unplanned', 'list-ordered',    30),
    ('Q_DUMP',  'Antre Dumping / Tongkang', 'standby_unplanned', 'list-ordered',    31),
    ('NOLOAD',  'Tunggu Loader',            'standby_unplanned', 'hourglass',       32),
    ('REFUEL',  'Refueling',                'standby_unplanned', 'fuel',            33),
    ('RAIN',    'Hujan / Jalan Licin',      'standby_unplanned', 'cloud-rain',      34),
    ('ROAD',    'Jalan Rusak',              'standby_unplanned', 'construction',    35),
    ('WAIT',    'Tunggu Instruksi',         'standby_unplanned', 'radio',           36),
    ('BD_ENG',  'BD Mesin',                 'breakdown',         'cog',             40),
    ('BD_TIRE', 'BD Ban',                   'breakdown',         'circle-dot',      41),
    ('BD_HYD',  'BD Hidrolik',              'breakdown',         'droplet',         42),
    ('BD_ELEC', 'BD Kelistrikan',           'breakdown',         'zap',             43),
    ('BD_OTH',  'BD Lainnya',               'breakdown',         'wrench',          44)
) AS v(code, name, category, icon, sort)
WHERE NOT EXISTS (SELECT 1 FROM "delay_codes" d WHERE d."code" = v.code);
