-- OWNER: pemilik usaha, masuk fms-administrator cuma buat lihat laporan & data (read-only).
INSERT INTO "roles" ("name", "is_default", "is_administrator")
SELECT 'OWNER', false, true
WHERE NOT EXISTS (SELECT 1 FROM "roles" WHERE "name" = 'OWNER');

INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p
WHERE r."name" = 'OWNER'
  AND p."name" IN ('report.read', 'device.read', 'site.read', 'route.read', 'driver.read', 'shift_template.read', 'shift.read')
  AND NOT EXISTS (SELECT 1 FROM "role_permissions" rp WHERE rp."role_id" = r."id" AND rp."permission_id" = p."id");
