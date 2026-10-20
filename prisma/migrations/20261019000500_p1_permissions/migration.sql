INSERT INTO "permissions" ("name", "description")
SELECT v.name, v.description FROM (VALUES
    ('target.manage', 'Atur target produksi per rute'),
    ('delay_code.manage', 'Atur master delay code'),
    ('mdt.manage', 'Pairing / cabut tablet MDT dan atur PIN operator'),
    ('unit_status.write', 'Input status unit (working / standby / breakdown) manual')
) AS v(name, description)
WHERE NOT EXISTS (SELECT 1 FROM "permissions" p WHERE p."name" = v.name);

INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p
WHERE (
    (r."name" = 'ADMIN' AND p."name" IN ('target.manage', 'delay_code.manage', 'mdt.manage', 'unit_status.write'))
    OR (r."name" = 'SUPERVISOR' AND p."name" IN ('mdt.manage', 'unit_status.write'))
    OR (r."name" = 'OWNER' AND p."name" IN ('target.manage'))
)
  AND NOT EXISTS (SELECT 1 FROM "role_permissions" rp WHERE rp."role_id" = r."id" AND rp."permission_id" = p."id");
