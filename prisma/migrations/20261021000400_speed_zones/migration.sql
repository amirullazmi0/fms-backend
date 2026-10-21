-- P3-3: zona batas kecepatan (turunan, crossing, area tongkang, ...). Unit di dalam radius zona
-- melebihi limit_kmh = overspeed (alert live + laporan per operator).
CREATE TYPE "speed_zone_kind" AS ENUM ('downhill', 'crossing', 'barge', 'other');

CREATE TABLE "speed_zones" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" VARCHAR(60) NOT NULL,
    "kind" "speed_zone_kind" NOT NULL DEFAULT 'other',
    "lat" DOUBLE PRECISION NOT NULL,
    "lng" DOUBLE PRECISION NOT NULL,
    "radius_m" INTEGER NOT NULL DEFAULT 100,
    "limit_kmh" INTEGER NOT NULL,
    "active" BOOLEAN NOT NULL DEFAULT true,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "deleted_at" TIMESTAMPTZ(3),
    "created_by" UUID,
    "updated_by" UUID,
    "deleted_by" UUID,
    CONSTRAINT "speed_zones_pkey" PRIMARY KEY ("id")
);

INSERT INTO "permissions" ("name", "description")
SELECT 'speed_zone.manage', 'Atur zona batas kecepatan'
WHERE NOT EXISTS (SELECT 1 FROM "permissions" WHERE "name" = 'speed_zone.manage');
INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p
WHERE r."name" IN ('ADMIN', 'SUPERVISOR') AND p."name" = 'speed_zone.manage'
  AND NOT EXISTS (SELECT 1 FROM "role_permissions" rp WHERE rp."role_id" = r."id" AND rp."permission_id" = p."id");
