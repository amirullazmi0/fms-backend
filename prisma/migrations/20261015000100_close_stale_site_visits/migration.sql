-- One-time cleanup: stays still "inside" although their shift (or assignment) already ended
-- become "finished" (Selesai). From now on the scheduler sets this itself.
UPDATE "site_visits" v SET "status" = 'finished', "computed_at" = CURRENT_TIMESTAMP
FROM "shifts" s
WHERE s."id" = v."shift_id" AND v."status" = 'inside' AND s."status" = 'closed';

UPDATE "site_visits" v SET "status" = 'finished', "computed_at" = CURRENT_TIMESTAMP
FROM "assignments" a
WHERE a."id" = v."assignment_id" AND v."status" = 'inside' AND a."end_at" IS NOT NULL;
