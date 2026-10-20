-- Operator / driver jadi user. users.user_type: staff (login dashboard pakai email + password) atau
-- operator (login MDT pakai NIK + PIN, nggak punya email / password).
-- Baris drivers disalin ke users dengan id yang SAMA, jadi assignments / trips / site_visits /
-- unit_status_logs tetap nyambung; FK driver_id dipindah ke users. Tabel lama disimpan sebagai
-- drivers_legacy (cadangan, dihapus di migrasi berikutnya setelah dicek).

CREATE TYPE "user_type" AS ENUM ('staff', 'operator');

ALTER TABLE "users" ADD COLUMN "user_type" "user_type" NOT NULL DEFAULT 'staff';
ALTER TABLE "users" ADD COLUMN "employee_no" TEXT;
ALTER TABLE "users" ADD COLUMN "phone" TEXT;
ALTER TABLE "users" ADD COLUMN "ibutton" TEXT;
ALTER TABLE "users" ADD COLUMN "pin_hash" TEXT;
ALTER TABLE "users" ADD COLUMN "note" TEXT;
ALTER TABLE "users" ALTER COLUMN "email" DROP NOT NULL;
ALTER TABLE "users" ALTER COLUMN "password_hash" DROP NOT NULL;

-- Staff wajib email + password; operator nggak boleh punya password (nggak bisa login dashboard).
ALTER TABLE "users" ADD CONSTRAINT "users_staff_login" CHECK (
    ("user_type" = 'staff' AND "email" IS NOT NULL AND "password_hash" IS NOT NULL)
    OR ("user_type" = 'operator' AND "password_hash" IS NULL)
);

-- Role khusus operator: tanpa permission dashboard.
INSERT INTO "roles" ("name", "is_default", "is_administrator")
SELECT 'OPERATOR', false, false
WHERE NOT EXISTS (SELECT 1 FROM "roles" WHERE "name" = 'OPERATOR');

INSERT INTO "users" ("id", "email", "name", "password_hash", "role_id", "is_administrator", "is_active", "user_type",
    "employee_no", "phone", "ibutton", "pin_hash", "note", "created_at", "updated_at", "deleted_at",
    "created_by", "updated_by", "deleted_by")
SELECT d."id", NULL, d."name", NULL, (SELECT "id" FROM "roles" WHERE "name" = 'OPERATOR'), false, d."active", 'operator',
    d."employee_no", d."phone", d."ibutton", d."pin_hash", d."note", d."created_at", d."updated_at", d."deleted_at",
    d."created_by", d."updated_by", d."deleted_by"
FROM "drivers" d
WHERE NOT EXISTS (SELECT 1 FROM "users" u WHERE u."id" = d."id");

-- NIK unik di antara operator yang masih ada.
CREATE UNIQUE INDEX "users_operator_employee_no" ON "users" (lower("employee_no"))
    WHERE "user_type" = 'operator' AND "deleted_at" IS NULL AND "employee_no" IS NOT NULL;
CREATE INDEX "users_user_type" ON "users" ("user_type");

ALTER TABLE "assignments" DROP CONSTRAINT "assignments_driver_id_fkey";
ALTER TABLE "assignments" ADD CONSTRAINT "assignments_driver_id_fkey" FOREIGN KEY ("driver_id") REFERENCES "users"("id") ON DELETE RESTRICT ON UPDATE NO ACTION;
ALTER TABLE "trips" DROP CONSTRAINT "trips_driver_id_fkey";
ALTER TABLE "trips" ADD CONSTRAINT "trips_driver_id_fkey" FOREIGN KEY ("driver_id") REFERENCES "users"("id") ON DELETE SET NULL ON UPDATE NO ACTION;
ALTER TABLE "site_visits" DROP CONSTRAINT "site_visits_driver_id_fkey";
ALTER TABLE "site_visits" ADD CONSTRAINT "site_visits_driver_id_fkey" FOREIGN KEY ("driver_id") REFERENCES "users"("id") ON DELETE SET NULL ON UPDATE NO ACTION;
ALTER TABLE "unit_status_logs" DROP CONSTRAINT "unit_status_logs_driver_id_fkey";
ALTER TABLE "unit_status_logs" ADD CONSTRAINT "unit_status_logs_driver_id_fkey" FOREIGN KEY ("driver_id") REFERENCES "users"("id") ON DELETE SET NULL ON UPDATE NO ACTION;

ALTER TABLE "drivers" RENAME TO "drivers_legacy";
