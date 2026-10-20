-- P1-1: tonase. payload_ton = muatan per rit (truck factor, ton). Nama kolom sengaja bukan "truck_factor"
-- karena TRUCK_FACTOR di config sudah dipakai sebagai pengali waktu tempuh OSRM.

ALTER TABLE "devices" ADD COLUMN "payload_ton" DOUBLE PRECISION NOT NULL DEFAULT 20;
-- Jenis unit: DT (dump truck), EX (excavator), LV (light vehicle), SUPPORT (grader, dozer, water/fuel truck).
CREATE TYPE "unit_type" AS ENUM ('DT', 'EX', 'LV', 'SUPPORT');
ALTER TABLE "devices" ADD COLUMN "unit_type" "unit_type" NOT NULL DEFAULT 'DT';

-- Snapshot payload saat ritase dihitung, agar tonase lama tidak berubah bila truck factor diganti.
ALTER TABLE "trips" ADD COLUMN "payload_ton" DOUBLE PRECISION;
