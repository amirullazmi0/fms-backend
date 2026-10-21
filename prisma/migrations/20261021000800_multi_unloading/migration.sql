-- Satu rute bisa punya lebih dari satu stockpile / jetty: routes.unloading_site_id = jetty utama,
-- extra_unloading_ids = jetty tambahan (urut). Ritase terhitung bila unit tiba di jetty mana pun milik rutenya.
ALTER TABLE "routes" ADD COLUMN "extra_unloading_ids" UUID[] NOT NULL DEFAULT '{}';

-- Jetty tujuan unit di penugasan shift. NULL = jetty utama rute.
ALTER TABLE "assignments" ADD COLUMN "unloading_site_id" UUID;
ALTER TABLE "assignments" ADD CONSTRAINT "assignments_unloading_site_id_fkey" FOREIGN KEY ("unloading_site_id")
    REFERENCES "sites"("id") ON DELETE SET NULL ON UPDATE NO ACTION;
