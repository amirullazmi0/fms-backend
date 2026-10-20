-- P2-5: fleet = rute + alat muat (excavator). Satu baris = satu periode excavator bertugas di rute;
-- end_at NULL = masih bertugas. Riwayatnya dipakai menghitung match factor periode lalu.
CREATE TABLE "fleet_loaders" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "route_id" UUID NOT NULL,
    "imei" TEXT NOT NULL,
    "productivity_tph" DOUBLE PRECISION,
    "start_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "end_at" TIMESTAMPTZ(3),
    "created_by" UUID,
    CONSTRAINT "fleet_loaders_pkey" PRIMARY KEY ("id")
);
CREATE INDEX "fleet_loaders_route" ON "fleet_loaders"("route_id", "start_at");
CREATE UNIQUE INDEX "fleet_loaders_route_open" ON "fleet_loaders"("route_id") WHERE "end_at" IS NULL;
ALTER TABLE "fleet_loaders" ADD CONSTRAINT "fleet_loaders_route_id_fkey" FOREIGN KEY ("route_id") REFERENCES "routes"("id") ON DELETE CASCADE ON UPDATE NO ACTION;
ALTER TABLE "fleet_loaders" ADD CONSTRAINT "fleet_loaders_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION;

INSERT INTO "permissions" ("name", "description")
SELECT 'dispatch.manage', 'Atur excavator per fleet dan pindahkan unit antar fleet'
WHERE NOT EXISTS (SELECT 1 FROM "permissions" WHERE "name" = 'dispatch.manage');
INSERT INTO "role_permissions" ("role_id", "permission_id")
SELECT r."id", p."id" FROM "roles" r CROSS JOIN "permissions" p
WHERE r."name" IN ('ADMIN', 'SUPERVISOR') AND p."name" = 'dispatch.manage'
  AND NOT EXISTS (SELECT 1 FROM "role_permissions" rp WHERE rp."role_id" = r."id" AND rp."permission_id" = p."id");
