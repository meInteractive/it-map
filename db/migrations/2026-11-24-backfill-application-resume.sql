-- GO-453. Привязка старых откликов к первому резюме того же кандидата.
-- Запуск после сохранения резервной копии на базе с GO-452.
START TRANSACTION;

INSERT INTO user_profiles(user_name, resume_md)
SELECT CONCAT('application-resume:', a.id, ':', a.user_name),
       JSON_OBJECT(
           'id', 1,
           'name', COALESCE(NULLIF(JSON_VALUE(m.resume_md, '$.names[0]'), ''), 'Основное резюме'),
           'applicationCreatedAt', a.created_at
       )
FROM company_applications AS a
LEFT JOIN user_profiles AS m ON m.user_name = CONCAT('resumes:', a.user_name)
LEFT JOIN user_profiles AS saved
    ON saved.user_name = CONCAT('application-resume:', a.id, ':', a.user_name)
WHERE saved.user_name IS NULL
   OR COALESCE(JSON_VALUE(saved.resume_md, '$.applicationCreatedAt'), '') <> a.created_at
   OR COALESCE(JSON_VALUE(saved.resume_md, '$.id'), 0) NOT IN (1, 2, 3)
ON DUPLICATE KEY UPDATE resume_md = IF(
    JSON_VALUE(user_profiles.resume_md, '$.applicationCreatedAt') = JSON_VALUE(VALUES(resume_md), '$.applicationCreatedAt')
        AND JSON_VALUE(user_profiles.resume_md, '$.id') IN (1, 2, 3),
    user_profiles.resume_md,
    VALUES(resume_md)
);

COMMIT;
