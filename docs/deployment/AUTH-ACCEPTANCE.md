# Приёмка HTTPS, proxy и повторного входа

Средняя часть GO-008 требует настоящего тестового домена. GO-320 готовит
проверку границы протоколов; сама по себе она не закрывает внешний gate.
Развёртывание стенда, DNS/ACME и реальные credentials в этой задаче не запускались.

## Что проверяет автоматический harness

```bash
go test -race ./internal/httpapi -run '^TestAuthHTTPSProxySessionFlow$' -count=1
```

Тест создаёт отдельные loopback HTTP backend и TLS reverse proxy, доверяет
только тестовому сертификату через штатный TLS client и использует настоящий
HTTP cookie jar. Virtual hostname example.com с корректным test SAN через
строго ограниченный dialer направляется только на два loopback ports: внешнего
DNS/network нет. Это исключает browser/Go localhost Secure-cookie exception.
TLS verification не отключается. Для Eugene/Nastya/Demo
проверяются anonymous401, invalid login401/header, login303/Location,
Secure/HttpOnly/SameSite=Strict/Path/Max-Age, session role, отказ передачи
Secure cookie по HTTP, чужой Origin403, tampered/signed-expired token401,
повторный вход200 и logout303→401. Сертификат/секреты/cookies не сохраняются
в файлы и не выводятся в результаты. SQL и провайдеры не вызываются.

Это Go proxy с реальными HTTP/TLS hops, а не запуск вашего Nginx. Cookie jar
проверяет доставку Secure cookie и полученные атрибуты; browser SameSite
policy он не моделирует. Внешний браузерный сценарий остаётся отдельным.
Nginx-шаблон уже передаёт исходный Host вместе с портом через `$http_host`:
[nginx.conf.example](../../deploy/nginx.conf.example). Ради входа нельзя
ослаблять same-origin guard или доверять присланному клиентом X-Forwarded-Host.

## Сценарий на разрешённом staging-домене

1. После отдельной подготовки изолированного стенда зафиксировать точный SHA,
   ОС, домен, дату и версии браузеров. Проверить доверенную цепочку TLS,
   отсутствие certificate exceptions и сохранение исходного Host reverse proxy.
   До cutover readiness остаётся закрытым; `/health/live` не означает готовность.
2. В свежем браузерном профиле открыть приложение по HTTPS. Без cookie
   `/api/session` должен вернуть401; вход с неверным паролем остаётся на форме.
   Использовать настроенные владельцем тестовые учётные записи, не публиковать
   пароли, cookie, HAR, traces или снимки приватных данных.
3. Войти через штатную форму под каждой из трёх ролей. Проверить303→приложение,
   cookie Secure/HttpOnly/SameSite=Strict/Path=/ и срок30дней, обновление страницы
   и новую вкладку на том же origin. Demo/Nastya не получают admin-интерфейс.
   Не выполнять бизнес-записи или скачивание реального backup ради auth-проверки.
4. На desktop/mobile удалить session cookie в браузере или задать истёкший
   browser expiry. Следующий API-запрос должен дать401 и показать повторный
   вход; после ввода корректного пароля доступ восстанавливается без reload loop.
   Это проверка browser expiry/потери cookie. Отказ сервера для подписанного
   expired token отдельно доказан harness; изменение SESSION_SECRET не требуется.
5. Выйти через интерфейс: cookie удаляется, `/api/session` снова401; refresh
   сохраняет форму входа. Проверить, что повторный вход продолжает работать.
6. Записать результаты по роли/браузеру, commit SHA и redacted HTTP statuses.
   Ошибку Host/Origin исправлять на proxy-границе; не отключать Secure/TLS/CSRF.

Перед внешней приёмкой нужен подключённый и отдельно разрешённый staging-домен.
Сейчас он отсутствует. Инструкции подготовки: [SERVER.md](SERVER.md), для Ubuntu
20.04 — [UBUNTU-20.md](UBUNTU-20.md). Этот документ не разрешает deployment.
