-- users.is_administrator: superuser. Lolos semua cek role & permission, dan boleh masuk fms-administrator.
-- (roles.is_administrator tetap = role itu boleh masuk fms-administrator, mis. OWNER yang cuma lihat laporan.)
ALTER TABLE "users" ADD COLUMN "is_administrator" BOOLEAN NOT NULL DEFAULT false;

-- User ADMIN yang udah ada jadi administrator.
UPDATE "users" u SET "is_administrator" = true
FROM "roles" r WHERE r."id" = u."role_id" AND r."name" = 'ADMIN';
