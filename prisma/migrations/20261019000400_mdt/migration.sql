-- Tablet MDT di kabin. Satu tablet dipasangkan ke satu unit (imei) lewat kode pairing dari supervisor.

CREATE TABLE "mdt_tablets" (
    "id" UUID NOT NULL DEFAULT gen_random_uuid(),
    "imei" TEXT NOT NULL,
    "name" VARCHAR(100),
    -- sha256 token tablet, diisi saat pairing berhasil
    "token_hash" VARCHAR(64),
    -- sha256 kode pairing 6 digit, berlaku sampai pair_code_expires
    "pair_code_hash" VARCHAR(64),
    "pair_code_expires" TIMESTAMPTZ(3),
    "paired_at" TIMESTAMPTZ(3),
    "last_sync_at" TIMESTAMPTZ(3),
    "revoked_at" TIMESTAMPTZ(3),
    "created_at" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "created_by" UUID,

    CONSTRAINT "mdt_tablets_pkey" PRIMARY KEY ("id"),
    CONSTRAINT "mdt_tablets_imei_fkey" FOREIGN KEY ("imei") REFERENCES "devices"("imei") ON DELETE CASCADE ON UPDATE NO ACTION
);

CREATE UNIQUE INDEX "mdt_tablets_token_hash_key" ON "mdt_tablets"("token_hash");
CREATE INDEX "mdt_tablets_imei" ON "mdt_tablets"("imei");

-- PIN operator untuk login MDT (bcrypt). Null = belum bisa login MDT.
ALTER TABLE "drivers" ADD COLUMN "pin_hash" TEXT;
