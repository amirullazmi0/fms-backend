-- Excavator ditugaskan di shift seperti DT (wajib operator). work_site_id = lokasi kerja EX:
-- pit rute (NULL) atau salah satu stockpile / jetty rute. NULL untuk DT.
ALTER TABLE "assignments" ADD COLUMN "work_site_id" UUID;
ALTER TABLE "assignments" ADD CONSTRAINT "assignments_work_site_id_fkey" FOREIGN KEY ("work_site_id")
    REFERENCES "sites"("id") ON DELETE SET NULL ON UPDATE NO ACTION;
