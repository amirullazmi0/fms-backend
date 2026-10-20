-- P2-4: data unit. Merek & model unit, model tracker (FMC150 / FMC650), kapasitas bucket excavator (m³).
ALTER TABLE "devices" ADD COLUMN "brand" VARCHAR(60);
ALTER TABLE "devices" ADD COLUMN "model" VARCHAR(60);
ALTER TABLE "devices" ADD COLUMN "tracker_model" VARCHAR(30);
ALTER TABLE "devices" ADD COLUMN "bucket_m3" DOUBLE PRECISION;

-- Riwayat pemasangan tracker ke unit (tracker bisa dipindah ke unit lain). removed_at NULL = masih terpasang.
CREATE TABLE "device_installs" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "imei" TEXT NOT NULL,
    "plate" VARCHAR(60) NOT NULL,
    "name" VARCHAR(60),
    "installed_at" TIMESTAMPTZ(3) NOT NULL,
    "removed_at" TIMESTAMPTZ(3),
    "created_by" UUID,
    CONSTRAINT "device_installs_pkey" PRIMARY KEY ("id")
);
CREATE INDEX "device_installs_imei" ON "device_installs"("imei", "installed_at");
ALTER TABLE "device_installs" ADD CONSTRAINT "device_installs_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION;

-- Unit yang sudah punya plat: pemasangan awal sejak tracker pertama terlihat.
INSERT INTO "device_installs" ("imei", "plate", "name", "installed_at")
SELECT "imei", "plate", "name", "first_seen" FROM "devices" WHERE "plate" IS NOT NULL AND "plate" <> '' AND "deleted_at" IS NULL;
