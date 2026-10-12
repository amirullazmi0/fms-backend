-- Role & permission (pola presiden-solusi): role bisa diatur dari fms-administrator, akses dicek per permission.
-- is_administrator = role ini boleh masuk aplikasi fms-administrator (atur role, permission, user).

-- CreateTable
CREATE TABLE "roles" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" VARCHAR(100) NOT NULL,
    "is_default" BOOLEAN NOT NULL DEFAULT false,
    "is_administrator" BOOLEAN NOT NULL DEFAULT false,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "deleted_at" TIMESTAMPTZ(3),
    "created_by" UUID,
    "updated_by" UUID,
    "deleted_by" UUID,

    CONSTRAINT "roles_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "permissions" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" VARCHAR(100) NOT NULL,
    "description" TEXT,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "deleted_at" TIMESTAMPTZ(3),

    CONSTRAINT "permissions_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "role_permissions" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "role_id" UUID NOT NULL,
    "permission_id" UUID NOT NULL,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "deleted_at" TIMESTAMPTZ(3),
    "created_by" UUID,
    "updated_by" UUID,
    "deleted_by" UUID,

    CONSTRAINT "role_permissions_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "roles_name_key" ON "roles"("name");
CREATE UNIQUE INDEX "idx_roles_single_default" ON "roles" ("is_default") WHERE "is_default" = true;
CREATE UNIQUE INDEX "permissions_name_key" ON "permissions"("name");
CREATE INDEX "idx_role_permission" ON "role_permissions"("role_id", "permission_id");

ALTER TABLE "role_permissions" ADD CONSTRAINT "role_permissions_role_id_fkey" FOREIGN KEY ("role_id") REFERENCES "roles"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
ALTER TABLE "role_permissions" ADD CONSTRAINT "role_permissions_permission_id_fkey" FOREIGN KEY ("permission_id") REFERENCES "permissions"("id") ON DELETE RESTRICT ON UPDATE CASCADE;

-- Katalog permission. Nama = konstanta di internal/model/permission.go (dipakai guard route).
INSERT INTO "permissions" ("name", "description") VALUES
    ('user.read_all', 'Lihat daftar user'),
    ('user.create', 'Bikin user'),
    ('user.update', 'Ubah nama / password / status user'),
    ('user.update_role', 'Ganti role user (minimal satu role wajib punya ini)'),
    ('user.delete', 'Hapus user'),
    ('role.read', 'Lihat role dan katalog permission'),
    ('role.create', 'Bikin role'),
    ('role.update', 'Ubah nama / permission / default role'),
    ('role.delete', 'Hapus role'),
    ('device.read', 'Live map, detail truk, perjalanan, ritase, event, status server'),
    ('device.update', 'Ubah nama / plat / kendaraan truk'),
    ('device.delete', 'Hapus truk'),
    ('device.command', 'Kirim perintah GPRS ke alat (mis. pemutus mesin)'),
    ('log.read', 'Log alat dan riwayat perintah GPRS'),
    ('site.read', 'Lihat titik'),
    ('site.create', 'Bikin titik'),
    ('site.update', 'Ubah titik'),
    ('site.delete', 'Hapus titik'),
    ('route.read', 'Lihat rute, jalur jalan, histori rute'),
    ('route.create', 'Bikin rute'),
    ('route.update', 'Ubah rute'),
    ('route.delete', 'Hapus rute'),
    ('driver.read', 'Lihat driver'),
    ('driver.create', 'Bikin driver'),
    ('driver.update', 'Ubah driver'),
    ('driver.delete', 'Hapus / nonaktifkan driver'),
    ('shift_template.read', 'Lihat template shift'),
    ('shift_template.create', 'Bikin template shift'),
    ('shift_template.update', 'Ubah template shift'),
    ('shift_template.delete', 'Hapus template shift'),
    ('shift.read', 'Lihat shift dan penugasan'),
    ('shift.create', 'Bikin shift'),
    ('shift.update', 'Ubah / perpanjang shift'),
    ('shift.delete', 'Hapus shift yang belum mulai'),
    ('shift.operate', 'Mulai, tutup, buka lagi shift'),
    ('assignment.manage', 'Atur penugasan driver–truk di shift'),
    ('report.read', 'Laporan ritase');

-- Role bawaan, sama dengan role demo.
INSERT INTO "roles" ("name", "is_default", "is_administrator") VALUES ('ADMIN', false, true), ('SUPERVISOR', false, false), ('VIEWER', true, false);

-- ADMIN: semua.
INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p WHERE r."name" = 'ADMIN';

-- SUPERVISOR: semua baca + atur shift, driver, template, penugasan, info truk, perintah GPRS.
INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p
WHERE r."name" = 'SUPERVISOR' AND (
    p."name" IN ('device.read', 'device.update', 'device.command', 'log.read', 'site.read', 'route.read', 'report.read', 'assignment.manage')
    OR p."name" LIKE 'driver.%' OR p."name" LIKE 'shift_template.%' OR p."name" LIKE 'shift.%'
);

-- VIEWER: cuma baca.
INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p
WHERE r."name" = 'VIEWER'
  AND p."name" IN ('device.read', 'site.read', 'route.read', 'driver.read', 'shift_template.read', 'shift.read', 'report.read');

-- users.role (teks) -> users.role_id.
ALTER TABLE "users" ADD COLUMN "role_id" UUID;
UPDATE "users" u SET "role_id" = r."id" FROM "roles" r WHERE r."name" = UPPER(u."role");
UPDATE "users" SET "role_id" = (SELECT "id" FROM "roles" WHERE "name" = 'VIEWER') WHERE "role_id" IS NULL;
ALTER TABLE "users" ALTER COLUMN "role_id" SET NOT NULL;
ALTER TABLE "users" DROP COLUMN "role";
ALTER TABLE "users" ADD CONSTRAINT "users_role_id_fkey" FOREIGN KEY ("role_id") REFERENCES "roles"("id") ON DELETE RESTRICT ON UPDATE CASCADE;
CREATE INDEX "idx_user_role" ON "users"("role_id");
