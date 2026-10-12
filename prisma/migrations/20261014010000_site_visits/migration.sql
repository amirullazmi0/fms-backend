-- Waktu truk di titik loading / unloading (antri, muat, bongkar). Satu baris = satu kali truk
-- masuk radius titik di rute penugasannya. depart_at NULL = truk masih di dalam (status inside).
CREATE TYPE "site_visit_role" AS ENUM ('loading', 'unloading');
CREATE TYPE "site_visit_status" AS ENUM ('inside', 'done');

-- CreateTable
CREATE TABLE "site_visits" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "assignment_id" UUID,
    "shift_id" UUID,
    "driver_id" UUID,
    "imei" TEXT NOT NULL,
    "route_id" UUID,
    "site_id" UUID NOT NULL,
    "site_name" TEXT NOT NULL,
    "role" "site_visit_role" NOT NULL,
    "arrive_at" TIMESTAMPTZ(3) NOT NULL,
    "depart_at" TIMESTAMPTZ(3),
    "dwell_ms" BIGINT NOT NULL,
    "status" "site_visit_status" NOT NULL,
    "computed_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "site_visits_pkey" PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX "site_visits_imei_site_arrive" ON "site_visits"("imei", "site_id", "arrive_at");
CREATE INDEX "site_visits_arrive_at" ON "site_visits"("arrive_at");
CREATE INDEX "site_visits_shift_id" ON "site_visits"("shift_id");
CREATE INDEX "site_visits_assignment_id" ON "site_visits"("assignment_id");

ALTER TABLE "site_visits" ADD CONSTRAINT "site_visits_assignment_id_fkey" FOREIGN KEY ("assignment_id") REFERENCES "assignments"("id") ON DELETE SET NULL ON UPDATE NO ACTION;
ALTER TABLE "site_visits" ADD CONSTRAINT "site_visits_shift_id_fkey" FOREIGN KEY ("shift_id") REFERENCES "shifts"("id") ON DELETE SET NULL ON UPDATE NO ACTION;
ALTER TABLE "site_visits" ADD CONSTRAINT "site_visits_driver_id_fkey" FOREIGN KEY ("driver_id") REFERENCES "drivers"("id") ON DELETE SET NULL ON UPDATE NO ACTION;
ALTER TABLE "site_visits" ADD CONSTRAINT "site_visits_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION;
ALTER TABLE "site_visits" ADD CONSTRAINT "site_visits_route_id_fkey" FOREIGN KEY ("route_id") REFERENCES "routes"("id") ON DELETE SET NULL ON UPDATE NO ACTION;
ALTER TABLE "site_visits" ADD CONSTRAINT "site_visits_site_id_fkey" FOREIGN KEY ("site_id") REFERENCES "sites"("id") ON DELETE CASCADE ON UPDATE NO ACTION;
