-- Data jalan (dulu file di data/roads): jalur yang udah dihitung + data jalan OSM per area.
-- Bisa dilihat & dihapus dari fms-administrator (menu Data jalan). Dihapus = dihitung ulang.

-- CreateTable
CREATE TABLE "road_paths" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "key" VARCHAR(32) NOT NULL,
    "from_lat" DOUBLE PRECISION NOT NULL,
    "from_lng" DOUBLE PRECISION NOT NULL,
    "to_lat" DOUBLE PRECISION NOT NULL,
    "to_lng" DOUBLE PRECISION NOT NULL,
    "coords" JSONB NOT NULL,
    "point_count" INTEGER NOT NULL,
    "distance_m" DOUBLE PRECISION NOT NULL,
    "duration_s" DOUBLE PRECISION NOT NULL,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "last_used_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "road_paths_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "road_paths_key_key" ON "road_paths"("key");
CREATE INDEX "road_paths_created_at" ON "road_paths"("created_at");

-- CreateTable
CREATE TABLE "road_areas" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "key" VARCHAR(120) NOT NULL,
    "kind" VARCHAR(10) NOT NULL,
    "south" DOUBLE PRECISION NOT NULL,
    "west" DOUBLE PRECISION NOT NULL,
    "north" DOUBLE PRECISION NOT NULL,
    "east" DOUBLE PRECISION NOT NULL,
    "elements" JSONB NOT NULL,
    "element_count" INTEGER NOT NULL,
    "size_bytes" INTEGER NOT NULL,
    "fetched_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "road_areas_pkey" PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "road_areas_key_key" ON "road_areas"("key");

INSERT INTO "permissions" ("name", "description")
SELECT v.name, v.description FROM (VALUES
    ('road.read', 'Lihat data jalan (jalur yang udah dihitung & data jalan per area)'),
    ('road.delete', 'Hapus data jalan (dihitung ulang waktu dibutuhin)')
) AS v(name, description)
WHERE NOT EXISTS (SELECT 1 FROM "permissions" p WHERE p."name" = v.name);

INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p
WHERE r."name" = 'ADMIN'
  AND p."name" IN ('road.read', 'road.delete')
  AND NOT EXISTS (SELECT 1 FROM "role_permissions" rp WHERE rp."role_id" = r."id" AND rp."permission_id" = p."id");
