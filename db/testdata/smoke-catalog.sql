-- SYNTHETIC CI DATA ONLY. Import AFTER schema checks, ONLY into disposable itmap_ci.
-- Never import into a real staging, shared, or production database.
INSERT INTO `company_crm` (`company_name`) VALUES ('CI Synthetic Employer');
INSERT INTO `company_identity` (`id`, `company_name`) VALUES (1, 'CI Synthetic Employer');
INSERT INTO `app_catalog` (`id`, `data_json`) VALUES (1, '{"companies":[{"name":"CI Synthetic Employer","sector":"CI test","offices":[],"jobs":[],"vacancySources":{}}],"agencies":[]}');
