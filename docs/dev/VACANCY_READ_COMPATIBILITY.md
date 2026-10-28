# Совместимость чтения вакансий

Остаток critical GO-011 разбит до реализации: GO-318 — даты/периоды,
GO-319 — каталог ролей и descendants, затем readonly SQL/API приёмка полного
чтения с нормализованными entities, источниками и пользовательским состоянием.
Эти части не заменяют согласованный снимок или полный browser/staging gate.

## GO-318: дата публикации и периоды

Эталон — неизменённый `shared/publication-period.ts`, который используют и
React, и исходный сервер. Вход Go соответствует текстовым/null полям БД:
publishedAt → loadedAt → createdAt; первая корректная дата выигрывает.
Проверяется десятисимвольный YYYY-MM-DD prefix после ECMAScript trim;
содержимое timestamp после prefix исходник не валидирует.

Go теперь сохраняет две ранее пропущенные особенности исходника:

- Date.UTC переопределяет годы0–99 как1900–1999, после чего исходная проверка
  равенства года отвергает исходную дату. Go также отвергает0000–0099 и
  продолжает fallback к loadedAt/createdAt.
- ECMAScript trim включает FEFF, но не включает0085. Go TrimSpace давал
  другое поведение. Предикат использует точный набор ECMAScript whitespace.

`TestPublicationPeriodSource` запускает unchanged TypeScript на pinned
Node24.19.0 и сравнивает27 375 результатов: все10 000 четырёхзначных лет для
1января и29февраля, календарные границы/невалидные месяцы и дни,
whitespace/prefix/surrogate inputs, fallback дат, Moscow midnight,
исторические смены московского времени, inclusive week, month-end/leap
clamping, strict archive и all. До исправления этот же corpus дал569
расхождений; после исправления их нет. Проверка обязательна в Public CI.

Фильтр all сохраняет любые записи, даже без корректной даты. Остальные
периоды исключают записи, у которых все три даты невалидны; archive — строго
раньше границы двух месяцев. Исходники TypeScript и фронтенда не менялись.
SQL row selection, role/source joins, HTTP query validation и полная GO-011
этой pure проверкой не объявляются принятыми.

## GO-319: роль, дерево и descendants

`roles.Build` сохраняет flat order входного SQL read set, а sibling lists
сортирует по sortOrder, принятому Russian collation из ADR-023 и затем ID.
Ключи вычисляются один раз на роль; строки name/stableKey/path не меняются.
Канонически равные имена сохраняют source ID tie, не сливаются. SQL childCount
и usageCount передаются как в source readVacancyRoles; ChildCount больше не
пересчитывается заново по входному массиву. Старые unit/HTTP fixtures получили
согласованные с их graph значения SQL ChildCount.

`TestRoleCatalogSource` вызывает unchanged readVacancyRoles с явным single-query
read-set stub и сравнивает164 frames: empty, metadata/child counts, arbitrary
flat order,128 sibling permutations и32 forests. Проверяются весь JSON
roles/tree/roots/leaves, paths/descendants, sortOrder/usage/child counts,
Cyrillic/Latin/case/accent/canonical equal names и стабильный ID tie. До
исправления129 frames расходились; после — все совпадают. Эти fixtures
содержат UTF8 text и positive safe integer IDs, соответствующие типам обычных
SQL rows. Required Public CI запускает source gate с race detector.

Это pure view proof, не выборка SQL, schema bootstrap, HTTP descendants
handler или доказательство role/source vacancy filters. Предыдущая защита от
cycles/orphan/duplicate/неположительных ID остаётся fail-closed; source orphan
поведение намеренно не объявляется принимаемым вместо целостного FK graph.
SQL/API role/filter integration и весь GO-011 остаются открытыми. Новое
архитектурное решение Unicode не требуется: используется уже принятое ADR-023.
