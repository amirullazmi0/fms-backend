-- P1-5 / P1-6: zona antre & zona spot di tiap titik, plus ambang alert antrean.

CREATE TYPE "site_zone_kind" AS ENUM ('queue', 'spot');

CREATE TABLE "site_zones" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "site_id" UUID NOT NULL,
    "kind" "site_zone_kind" NOT NULL,
    "lat" DOUBLE PRECISION NOT NULL,
    "lng" DOUBLE PRECISION NOT NULL,
    "radius_m" INTEGER NOT NULL DEFAULT 20,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "deleted_at" TIMESTAMPTZ(3),
    "created_by" UUID,
    "updated_by" UUID,
    "deleted_by" UUID,

    CONSTRAINT "site_zones_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "site_zones_site_id_fkey" FOREIGN KEY ("site_id") REFERENCES "sites"("id") ON DELETE CASCADE ON UPDATE NO ACTION
);

CREATE INDEX "site_zones_site_id" ON "site_zones"("site_id");

-- Alert: >= queue_alert_trucks unit antre lebih dari queue_alert_min menit.
ALTER TABLE "sites" ADD COLUMN "queue_alert_trucks" INTEGER NOT NULL DEFAULT 3;
ALTER TABLE "sites" ADD COLUMN "queue_alert_min" INTEGER NOT NULL DEFAULT 15;

-- Pecahan waktu di titik: antre, spotting + muat/bongkar (zona spot), sisanya di radius inti.
ALTER TABLE "site_visits" ADD COLUMN "queue_ms" BIGINT;
ALTER TABLE "site_visits" ADD COLUMN "spot_ms" BIGINT;
ALTER TABLE "site_visits" ADD COLUMN "work_ms" BIGINT;
