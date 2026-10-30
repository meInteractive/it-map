# GO-360: контракт структурированных данных

Перенесены source extraction schema v2, лимит120000 UTF-16 units и валидация17 обязательных полей. Schema доступна как defensive copy; исходный schema JSON сформирован из неизменённого JavaScript.

`NormalizeExtraction` отклоняет неизвестные/пропущенные поля, неверные типы/enums, пустые/nullbyte/слишком длинные строки, короткое описание, перепутанные границы и неподтверждённые суммы зарплаты, недействительные даты. Неподтверждённые optional currency/tax/period/work-format/date заменяются null с теми же corrections. `ValidateExtraction` сохраняет source strict-поведение и возвращает ошибку вместо corrections.

46 actual source frames проверяют schema/version/limit, нормализованные поля/corrections и точные сообщения ошибок. Отдельно проверяются JS whitespace/NBSP, UTF-16 длина title и окон evidence, финальная sigma и запрет ложного ASCII case folding long-s/Kelvin, разделители строк. Oracle работает без provider/SQL/секретов и включён required шагом Public CI.

Это pure-контракт GO-025: automatic extraction SQL stage/snapshot provenance/cache/leases и применение результата в pipeline подключаются следующей задачей. Одно наличие валидатора не означает готовность parser. Схема БД, данные, сервер и runtime не менялись; реальные calls и деплой не выполнялись.
