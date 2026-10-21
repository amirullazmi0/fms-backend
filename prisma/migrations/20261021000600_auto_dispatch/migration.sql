-- P3-9: auto dispatch. Default mati; bila menyala, saran pindah DT dari panel dispatcher yang stabil
-- (stableMin menit) dijalankan otomatis, maksimal sekali per fleet tiap cooldownMin menit.
INSERT INTO "features" ("key", "name", "description", "enabled", "config")
SELECT 'auto_dispatch', 'Auto dispatch', 'Pindahkan DT antar fleet otomatis bila saran panel dispatcher stabil, agar match factor seimbang. Operator melihat fleet baru di MDT.', false, '{"stableMin": 10, "cooldownMin": 30}'
WHERE NOT EXISTS (SELECT 1 FROM "features" WHERE "key" = 'auto_dispatch');
