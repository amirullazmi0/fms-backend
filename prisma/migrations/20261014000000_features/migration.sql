-- Fitur & konfigurasi yang bisa dinyalakan/dimatikan dari fms-administrator (menu Fitur).
-- key = nama fitur (konstanta model.Feature*), config = setelan fitur (JSON).

-- CreateTable
CREATE TABLE "features" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "key" VARCHAR(100) NOT NULL,
    "name" VARCHAR(150) NOT NULL,
    "description" TEXT,
    "enabled" BOOLEAN NOT NULL DEFAULT false,
    "config" JSONB NOT NULL DEFAULT '{}',
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "deleted_at" TIMESTAMPTZ(3),
    "created_by" UUID,
    "updated_by" UUID,
    "deleted_by" UUID,

    CONSTRAINT "features_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "features_key_key" ON "features"("key");

-- Simulasi: truk FMC150 palsu dijalankan backend sendiri (tombol "Jalankan simulasi" di sidebar fms-administrator).
INSERT INTO "features" ("key", "name", "description", "enabled", "config")
SELECT 'simulation', 'Simulasi truk', 'Truk FMC150 palsu yang konek ke server, buat demo dan uji tanpa alat asli. Truk yang ditugasin di shift aktif jalan sesuai rutenya.', true, '{"devices": 2, "interval": 3, "loop": false}'
WHERE NOT EXISTS (SELECT 1 FROM "features" WHERE "key" = 'simulation');

INSERT INTO "permissions" ("name", "description")
SELECT v.name, v.description FROM (VALUES
    ('feature.update', 'Nyalain / matiin fitur dan ubah konfigurasinya'),
    ('simulation.run', 'Jalankan / hentikan simulasi truk')
) AS v(name, description)
WHERE NOT EXISTS (SELECT 1 FROM "permissions" p WHERE p."name" = v.name);

INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p
WHERE r."name" = 'ADMIN'
  AND p."name" IN ('feature.update', 'simulation.run')
  AND NOT EXISTS (SELECT 1 FROM "role_permissions" rp WHERE rp."role_id" = r."id" AND rp."permission_id" = p."id");
