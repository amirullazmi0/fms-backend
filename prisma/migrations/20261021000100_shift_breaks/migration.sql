-- P2-3: istirahat terjadwal. Template menyimpan daftar istirahat; shift menyalinnya saat dibuat
-- (perubahan template tidak mengubah shift lama). Format: [{"start":"12:00","end":"13:00","code":"ISHOMA"}].
ALTER TABLE "shift_templates" ADD COLUMN "breaks" JSONB NOT NULL DEFAULT '[]';
ALTER TABLE "shifts" ADD COLUMN "breaks" JSONB NOT NULL DEFAULT '[]';
