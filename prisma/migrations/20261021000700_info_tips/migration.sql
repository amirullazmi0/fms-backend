-- Info & panduan: tombol (i) di halaman, kartu, dan metrik supervisor & administrator yang menjelaskan
-- fitur, cara membaca angka, dan istilah tambang. Matikan bila tim sudah terbiasa.
INSERT INTO "features" ("key", "name", "description", "enabled", "config")
SELECT 'info_tips', 'Info & panduan', 'Tampilkan tombol (i) di setiap halaman, kartu, dan metrik penting: arahkan kursor untuk ringkasan, klik untuk penjelasan lengkap dan istilah. Berlaku di supervisor & administrator.', true, '{}'
WHERE NOT EXISTS (SELECT 1 FROM "features" WHERE "key" = 'info_tips');
