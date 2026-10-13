-- Stay that was still open when the truck's shift ended (shift beres): shown as "Selesai",
-- not counted as "di dalam" nor in the dwell averages.
ALTER TYPE "site_visit_status" ADD VALUE IF NOT EXISTS 'finished';
