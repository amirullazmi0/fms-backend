-- P1-2: target produksi per rute. Berlaku mulai valid_from sampai ada target baru.

CREATE TABLE "production_targets" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "route_id" UUID NOT NULL,
    "rit_per_hour" DOUBLE PRECISION,
    "ton_per_hour" DOUBLE PRECISION,
    "rit_per_shift" DOUBLE PRECISION,
    "ton_per_shift" DOUBLE PRECISION,
    "valid_from" DATE NOT NULL DEFAULT CURRENT_DATE,
    "note" TEXT,
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "updated_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "deleted_at" TIMESTAMPTZ(3),
    "created_by" UUID,
    "updated_by" UUID,
    "deleted_by" UUID,

    CONSTRAINT "production_targets_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "production_targets_route_id_fkey" FOREIGN KEY ("route_id") REFERENCES "routes"("id") ON DELETE CASCADE ON UPDATE NO ACTION
);

CREATE INDEX "production_targets_route_valid" ON "production_targets"("route_id", "valid_from");
