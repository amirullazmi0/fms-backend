-- CreateSchema
CREATE SCHEMA IF NOT EXISTS "public";

-- CreateEnum
CREATE TYPE "shift_status" AS ENUM ('planned', 'active', 'closed');

-- CreateEnum
CREATE TYPE "shift_close_reason" AS ENUM ('manual', 'auto');

-- CreateEnum
CREATE TYPE "trip_status" AS ENUM ('done', 'unfinished');

-- CreateEnum
CREATE TYPE "trip_leg" AS ENUM ('go', 'back');

-- CreateTable
CREATE TABLE "users" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "email" TEXT NOT NULL,
    "name" TEXT,
    "password_hash" TEXT NOT NULL,
    "role" TEXT NOT NULL DEFAULT 'admin',
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "last_login_at" TIMESTAMPTZ(3),

    CONSTRAINT "users_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "sessions" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "token_hash" TEXT NOT NULL,
    "user_id" UUID NOT NULL,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "expires_at" TIMESTAMPTZ(3) NOT NULL,
    "user_agent" TEXT,
    "ip" TEXT,

    CONSTRAINT "sessions_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "devices" (
    "imei" TEXT NOT NULL,
    "name" TEXT,
    "plate" TEXT,
    "vehicle" TEXT,
    "first_seen" TIMESTAMPTZ(3) NOT NULL,
    "last_seen" TIMESTAMPTZ(3),
    "last_connected_at" TIMESTAMPTZ(3),
    "last_remote" TEXT,

    CONSTRAINT "devices_pkey" PRIMARY KEY ("imei")
);

-- CreateTable
CREATE TABLE "records" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "imei" TEXT NOT NULL,
    "time" TIMESTAMPTZ(3) NOT NULL,
    "received_at" TIMESTAMPTZ(3) NOT NULL,
    "lat" DOUBLE PRECISION NOT NULL,
    "lng" DOUBLE PRECISION NOT NULL,
    "alt" INTEGER NOT NULL,
    "angle" INTEGER NOT NULL,
    "sats" INTEGER NOT NULL,
    "speed" INTEGER NOT NULL,
    "priority" INTEGER NOT NULL,
    "event_io" INTEGER NOT NULL,
    "valid" BOOLEAN NOT NULL,
    "io" JSONB NOT NULL,

    CONSTRAINT "records_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "events" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "imei" TEXT NOT NULL,
    "time" TIMESTAMPTZ(3) NOT NULL,
    "level" TEXT NOT NULL,
    "kind" TEXT NOT NULL,
    "text" TEXT NOT NULL,

    CONSTRAINT "events_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "commands" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "imei" TEXT NOT NULL,
    "time" TIMESTAMPTZ(3) NOT NULL,
    "dir" TEXT NOT NULL,
    "text" TEXT NOT NULL,

    CONSTRAINT "commands_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "places" (
    "key" TEXT NOT NULL,
    "label" TEXT,
    "fetched_at" TIMESTAMPTZ(3) NOT NULL,

    CONSTRAINT "places_pkey" PRIMARY KEY ("key")
);

-- CreateTable
CREATE TABLE "sites" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" TEXT NOT NULL,
    "lat" DOUBLE PRECISION NOT NULL,
    "lng" DOUBLE PRECISION NOT NULL,
    "radius_m" INTEGER NOT NULL DEFAULT 10,
    "note" TEXT,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "sites_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "routes" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" TEXT NOT NULL,
    "loading_site_id" UUID NOT NULL,
    "unloading_site_id" UUID NOT NULL,
    "note" TEXT,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "routes_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "drivers" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" TEXT NOT NULL,
    "phone" TEXT,
    "employee_no" TEXT,
    "ibutton" TEXT,
    "active" BOOLEAN NOT NULL DEFAULT true,
    "note" TEXT,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "drivers_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "shift_templates" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" TEXT NOT NULL,
    "start_time" TIME(0) NOT NULL,
    "end_time" TIME(0) NOT NULL,
    "auto_close" BOOLEAN NOT NULL DEFAULT true,
    "sort" INTEGER NOT NULL DEFAULT 0,

    CONSTRAINT "shift_templates_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "shifts" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "name" TEXT NOT NULL,
    "route_id" UUID,
    "date" DATE NOT NULL,
    "planned_start" TIMESTAMPTZ(3) NOT NULL,
    "planned_end" TIMESTAMPTZ(3) NOT NULL,
    "auto_close" BOOLEAN NOT NULL DEFAULT true,
    "status" "shift_status" NOT NULL DEFAULT 'planned',
    "started_at" TIMESTAMPTZ(3),
    "closed_at" TIMESTAMPTZ(3),
    "close_reason" "shift_close_reason",
    "overdue_noted" BOOLEAN NOT NULL DEFAULT false,
    "created_by" UUID,
    "closed_by" UUID,
    "note" TEXT,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "shifts_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "assignments" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "shift_id" UUID NOT NULL,
    "driver_id" UUID NOT NULL,
    "imei" TEXT NOT NULL,
    "route_id" UUID NOT NULL,
    "note" TEXT,
    "start_at" TIMESTAMPTZ(3),
    "end_at" TIMESTAMPTZ(3),
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "assignments_pkey" PRIMARY KEY ("id")
);

-- CreateTable
CREATE TABLE "trips" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "assignment_id" UUID,
    "shift_id" UUID,
    "driver_id" UUID,
    "imei" TEXT NOT NULL,
    "route_id" UUID,
    "route_name" TEXT NOT NULL,
    "loading_site_id" UUID,
    "loading_name" TEXT NOT NULL,
    "unloading_site_id" UUID,
    "unloading_name" TEXT NOT NULL,
    "depart_at" TIMESTAMPTZ(3) NOT NULL,
    "arrive_at" TIMESTAMPTZ(3),
    "duration_ms" BIGINT NOT NULL,
    "distance_m" DOUBLE PRECISION NOT NULL,
    "moving_ms" BIGINT NOT NULL,
    "stops" INTEGER NOT NULL,
    "stopped_ms" BIGINT NOT NULL,
    "max_speed" INTEGER NOT NULL,
    "avg_speed" DOUBLE PRECISION NOT NULL,
    "fuel_unit" TEXT,
    "fuel_at_loading" DOUBLE PRECISION,
    "fuel_at_unloading" DOUBLE PRECISION,
    "fuel_used" DOUBLE PRECISION,
    "load_dwell_ms" BIGINT NOT NULL,
    "unload_dwell_ms" BIGINT,
    "status" "trip_status" NOT NULL,
    "leg" "trip_leg" NOT NULL DEFAULT 'go',
    "computed_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT "trips_pkey" PRIMARY KEY ("id")
);

-- CreateIndex
CREATE UNIQUE INDEX "users_email_key" ON "users"("email");

-- CreateIndex
CREATE UNIQUE INDEX "sessions_token_hash_key" ON "sessions"("token_hash");

-- CreateIndex
CREATE INDEX "sessions_user_id" ON "sessions"("user_id");

-- CreateIndex
CREATE INDEX "records_imei_time" ON "records"("imei", "time");

-- CreateIndex
CREATE UNIQUE INDEX "records_unique" ON "records"("imei", "time", "event_io", "priority");

-- CreateIndex
CREATE INDEX "events_time" ON "events"("time");

-- CreateIndex
CREATE INDEX "commands_imei_time" ON "commands"("imei", "time");

-- CreateIndex
CREATE INDEX "routes_loading_site_id" ON "routes"("loading_site_id");

-- CreateIndex
CREATE INDEX "routes_unloading_site_id" ON "routes"("unloading_site_id");

-- CreateIndex
CREATE INDEX "shifts_date" ON "shifts"("date");

-- CreateIndex
CREATE INDEX "shifts_route_id" ON "shifts"("route_id");

-- CreateIndex
CREATE INDEX "shifts_status" ON "shifts"("status");

-- CreateIndex
CREATE INDEX "assignments_shift_id" ON "assignments"("shift_id");

-- CreateIndex
CREATE INDEX "trips_depart_at" ON "trips"("depart_at");

-- CreateIndex
CREATE INDEX "trips_shift_id" ON "trips"("shift_id");

-- CreateIndex
CREATE UNIQUE INDEX "trips_imei_depart" ON "trips"("imei", "depart_at");

-- AddForeignKey
ALTER TABLE "sessions" ADD CONSTRAINT "sessions_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users"("id") ON DELETE CASCADE ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "records" ADD CONSTRAINT "records_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "events" ADD CONSTRAINT "events_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "commands" ADD CONSTRAINT "commands_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "routes" ADD CONSTRAINT "routes_loading_site_id_fkey" FOREIGN KEY ("loading_site_id") REFERENCES "sites"("id") ON DELETE RESTRICT ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "routes" ADD CONSTRAINT "routes_unloading_site_id_fkey" FOREIGN KEY ("unloading_site_id") REFERENCES "sites"("id") ON DELETE RESTRICT ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "shifts" ADD CONSTRAINT "shifts_created_by_fkey" FOREIGN KEY ("created_by") REFERENCES "users"("id") ON DELETE SET NULL ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "shifts" ADD CONSTRAINT "shifts_closed_by_fkey" FOREIGN KEY ("closed_by") REFERENCES "users"("id") ON DELETE SET NULL ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "shifts" ADD CONSTRAINT "shifts_route_id_fkey" FOREIGN KEY ("route_id") REFERENCES "routes"("id") ON DELETE RESTRICT ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "assignments" ADD CONSTRAINT "assignments_shift_id_fkey" FOREIGN KEY ("shift_id") REFERENCES "shifts"("id") ON DELETE CASCADE ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "assignments" ADD CONSTRAINT "assignments_driver_id_fkey" FOREIGN KEY ("driver_id") REFERENCES "drivers"("id") ON DELETE RESTRICT ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "assignments" ADD CONSTRAINT "assignments_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "assignments" ADD CONSTRAINT "assignments_route_id_fkey" FOREIGN KEY ("route_id") REFERENCES "routes"("id") ON DELETE RESTRICT ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "trips" ADD CONSTRAINT "trips_assignment_id_fkey" FOREIGN KEY ("assignment_id") REFERENCES "assignments"("id") ON DELETE SET NULL ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "trips" ADD CONSTRAINT "trips_shift_id_fkey" FOREIGN KEY ("shift_id") REFERENCES "shifts"("id") ON DELETE SET NULL ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "trips" ADD CONSTRAINT "trips_driver_id_fkey" FOREIGN KEY ("driver_id") REFERENCES "drivers"("id") ON DELETE SET NULL ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "trips" ADD CONSTRAINT "trips_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION;

-- AddForeignKey
ALTER TABLE "trips" ADD CONSTRAINT "trips_route_id_fkey" FOREIGN KEY ("route_id") REFERENCES "routes"("id") ON DELETE SET NULL ON UPDATE NO ACTION;

