-- Jenis unit menjadi enum (sebelumnya VARCHAR). Nilai di luar daftar dianggap DT.
CREATE TYPE "unit_type" AS ENUM ('DT', 'EX', 'LV', 'SUPPORT');

UPDATE "devices" SET "unit_type" = 'DT' WHERE "unit_type" NOT IN ('DT', 'EX', 'LV', 'SUPPORT');
ALTER TABLE "devices" ALTER COLUMN "unit_type" DROP DEFAULT;
ALTER TABLE "devices" ALTER COLUMN "unit_type" TYPE "unit_type" USING "unit_type"::"unit_type";
ALTER TABLE "devices" ALTER COLUMN "unit_type" SET DEFAULT 'DT';
