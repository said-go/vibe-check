# Развёртывание ВАЙБЧЕК в Yandex Cloud

Для рабочего AI-сценария разверните **весь проект одним контейнером**: Go сервер раздаёт PWA на `/` и API на `/api`. Это сохраняет один HTTPS origin для камеры, Service Worker и загрузки фото. Опубликованный статический Sites остаётся демонстрацией интерфейса до переключения пользователей на адрес контейнера.

Потребуются каталог Yandex Cloud с биллингом, Yandex Container Registry, Serverless Containers, сервисный аккаунт с доступом к AI Studio и секрет в Lockbox. Локально нужен Docker; для командного способа также `yc` CLI. Текущая среда разработки не имеет `yc` CLI или работающего Docker daemon, поэтому эта инструкция пока не является подтверждением живого запуска.

## 0. Проверить локальную готовность

Из корня проекта на Windows запустите `pwsh -NoProfile -File scripts/preflight.ps1`. Скрипт выполняет Go тесты, `go vet`, проверку синтаксиса PWA и сообщает, доступны ли Docker daemon и `yc`. Он не читает секреты и не создаёт облачные ресурсы. После запуска Docker можно добавить `-BuildImage`, чтобы проверить локальную сборку; `-RequireCloudTools` требует готового Docker daemon и установленного `yc`.

Сценарий `scripts/deploy-yandex.ps1` принимает только ID ресурсов, проверяет их формат и по умолчанию показывает план без облачных операций. Запуск с `-Apply` требует чистое Git-дерево, работающий Docker, настроенную авторизацию Docker в Container Registry и `yc` с правами на контейнер. Он собирает образ с полным SHA коммита, отправляет его в Registry и создаёт новую ревизию существующего контейнера. Он не создаёт ресурсы, не открывает публичный доступ и не читает значение ключа из Lockbox.

```powershell
pwsh -NoProfile -File scripts/deploy-yandex.ps1 `
  -FolderId <folder-id> -RegistryId <registry-id> -ContainerId <container-id> `
  -ServiceAccountId <service-account-id> -SecretId <lockbox-secret-id> `
  -SecretVersionId <lockbox-version-id>
# Повторите с -Apply после проверки плана и доступа.
```

## 1. Подготовить доступ

Создайте сервисный аккаунт для контейнера. Дайте ему `container-registry.images.puller` на registry, `ai.languageModels.user` на каталог и `lockbox.payloadViewer` на конкретный секрет. Если секрет защищён пользовательским KMS ключом, потребуется и `kms.keys.encrypterDecrypter` на ключ. Для создания ревизии вызывающему пользователю нужна роль `iam.serviceAccounts.user` на сервисном аккаунте; для отправки образа в Registry — `container-registry.images.pusher` на registry или repository. Создайте API-ключ с областью `yc.ai.foundationModels.execute`, сохраните его в Yandex Lockbox под ключом `YANDEX_API_KEY`. Ключ не добавляйте в GitHub, Dockerfile или командную строку.

## 2. Собрать образ

Создайте Container Registry и настройте официальный Docker credential helper командой `yc container registry configure-docker`. Из корня репозитория соберите образ по `Dockerfile`, пометьте адресом вашего Yandex Container Registry и отправьте туда. Деплойте конкретный тег или digest, связанный с Git commit; не используйте плавающий `latest` для проверки релиза.

```text
docker build --platform linux/amd64 -t cr.yandex/<registry-id>/vibe-check:<git-sha> .
docker push cr.yandex/<registry-id>/vibe-check:<git-sha>
```

## 3. Создать контейнер и ревизию

Создайте Serverless Container `vibe-check`. В ревизии укажите образ, сервисный аккаунт, таймаут выполнения 150 секунд, память и ограничения параллельных запросов. У приложения таймаут приёма фото составляет 30 секунд, запроса к AI — 90 секунд, HTTP запись ограничена 120 секундами; дополнительное время платформы покрывает запуск контейнера. Приложение слушает переменную `PORT`, которую выставляет платформа. Задайте обычные переменные:
Для анализа снимков размером до 20 мегапикселей выделите как минимум 512 МБ памяти, предпочтительно 1 ГБ: декодирование и очистка кадра создают несколько копий пикселей в памяти.

У Serverless Containers жёсткий предел входящего HTTP-запроса **3,5 МБ вместе с заголовками**. Браузер может открыть исходное фото до 25 МБ локально, но перед отправкой сжимает JPEG до **2,5 МиБ**. Серверный лимит в 10 МБ не отменяет ограничение платформы: запрос большего размера будет отклонён ещё до приложения.

```text
APP_ENV=production
AI_PROVIDER=yandex
YANDEX_FOLDER_ID=<folder-id>
YANDEX_MODEL=qwen3.6-35b-a3b
FRONTEND_DIR=/app/dist
ANALYSES_PER_HOUR=30
ANALYSES_PER_CLIENT_MINUTE=3
MAX_CONCURRENT_ANALYSES=2
```

Привяжите `YANDEX_API_KEY` из Lockbox как переменную окружения ревизии. После ротации ключа создавайте новую ревизию. Для браузерного доступа контейнеру потребуется публичный HTTPS вызов; это решение применяйте только после включения лимитов запросов и оценки расходов.

При работе через CLI команда создания контейнера — `yc serverless container create --name vibe-check`. Для ревизии используйте `yc serverless container revision deploy` с `--container-name`, `--image`, `--service-account-id`, `--execution-timeout 150s`, `--environment` для перечисленных выше открытых значений и `--secret environment-variable=YANDEX_API_KEY,id=<secret-id>,version-id=<version-id>,key=YANDEX_API_KEY`. Секретные значения в `--environment` не передавайте. Точные параметры CLI сверяйте с [документацией ревизий](https://yandex.cloud/en/docs/serverless-containers/operations/manage-revision) и [Lockbox](https://yandex.cloud/en/docs/lockbox/operations/serverless/containers).

Скрипт задаёт максимум один экземпляр **на зону**, поэтому суммарный расход может расти при запуске в нескольких зонах. Его лимиты в памяти также действуют отдельно на каждом экземпляре. Перед публичным доступом настройте бюджет и проверьте доступные ограничения в облаке.

Встроенные лимиты работают в памяти одного экземпляра. Если платформа запускает несколько экземпляров, общий расход может превышать `ANALYSES_PER_HOUR`; ограничьте масштабирование или настройте внешний gateway с общим лимитом перед открытым релизом.

## 4. Проверить релиз

Пока контейнер закрыт, проверьте `https://<container-id>.containers.yandexcloud.net/api/healthz` и `/api/readyz` через авторизованный HTTP клиент с IAM токеном по [инструкции Yandex Cloud](https://yandex.cloud/en/docs/serverless-containers/operations/invoke). Браузерная PWA на этом этапе не может вызвать закрытый контейнер без IAM токена. Проверьте журналы на отсутствие изображения, API-ключа и тела запроса.

После открытия публичного доступа для проверок без отправки фото запустите `pwsh -NoProfile -File scripts/smoke-yandex.ps1 -BaseUrl https://<container-id>.containers.yandexcloud.net/`. Он требует ответы 200 от `healthz`, `readyz`, главной страницы и PWA-манифеста. Затем через PWA отправьте тестовое фото после явного согласия и убедитесь, что появляется результат либо понятная ошибка низкой уверенности. Для проверки лимита отправьте серию запросов и убедитесь, что сервер возвращает 429 до обращения к AI. Скрипт проверяет обычный публичный HTTPS origin. Для локального Go сервера допускается `-AllowHttp`.

Когда контейнер пройдёт проверки и лимит расходов будет настроен для публичного трафика, разрешите вызов без IAM токена через `yc serverless container allow-unauthenticated-invoke vibe-check`. После живой проверки укажите адрес контейнера как основной URL проекта. Статический Sites не проксирует запросы к Go серверу и не является полноценным релизом анализа.

## Источники

- [Yandex Cloud: Serverless Containers quickstart](https://yandex.cloud/en/docs/serverless-containers/quickstart/container)
- [Yandex Cloud: передача секретов Lockbox в контейнер](https://yandex.cloud/en/docs/lockbox/operations/serverless/containers)
- [Yandex Cloud: вызов контейнера по HTTPS](https://yandex.cloud/en/docs/serverless-containers/operations/invoke)
- [Yandex Cloud: Docker credential helper для Container Registry](https://yandex.cloud/en/docs/container-registry/operations/authentication)
- [Yandex Cloud: публичный вызов контейнера](https://yandex.cloud/en/docs/serverless-containers/operations/container-public)
- [Yandex Cloud: актуальные параметры `yc serverless container revision deploy`](https://yandex.cloud/en/docs/cli/cli-ref/serverless/cli-ref/container/revision/deploy)
- [Yandex Cloud: квоты, в том числе лимит HTTP-запроса 3.5 МБ](https://yandex.cloud/en/docs/serverless-containers/concepts/limits)
- [Yandex Cloud: роли Container Registry](https://yandex.cloud/en/docs/container-registry/security/)
