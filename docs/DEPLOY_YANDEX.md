# Развёртывание ВАЙБЧЕК в Yandex Cloud

Для рабочего AI-сценария разверните **весь проект одним контейнером**: Go сервер раздаёт PWA на `/` и API на `/api`. Это сохраняет один HTTPS origin для камеры, Service Worker и загрузки фото. Опубликованный статический Sites остаётся демонстрацией интерфейса до переключения пользователей на адрес контейнера.

Потребуются каталог Yandex Cloud с биллингом, Yandex Container Registry, Serverless Containers, сервисный аккаунт с доступом к AI Studio и секрет в Lockbox. Локально нужен Docker; для командного способа также `yc` CLI. Текущая среда разработки не имеет настроенного `yc` CLI или работающего Docker daemon, поэтому эта инструкция пока не является подтверждением живого запуска.

## 1. Подготовить доступ

Создайте сервисный аккаунт для контейнера. Дайте ему право читать образ из Container Registry, `ai.languageModels.user` для AI Studio и `lockbox.payloadViewer` для секрета. Создайте API-ключ с областью `yc.ai.foundationModels.execute`, сохраните его в Yandex Lockbox под ключом `YANDEX_API_KEY`. Ключ не добавляйте в GitHub, Dockerfile или командную строку.

## 2. Собрать образ

Из корня репозитория соберите образ по `Dockerfile`, пометьте адресом вашего Yandex Container Registry и отправьте туда. Деплойте конкретный тег или digest, связанный с Git commit; не используйте плавающий `latest` для проверки релиза.

```text
docker build -t cr.yandex/<registry-id>/vibe-check:<git-sha> .
docker push cr.yandex/<registry-id>/vibe-check:<git-sha>
```

## 3. Создать контейнер и ревизию

Создайте Serverless Container `vibe-check`. В ревизии укажите образ, сервисный аккаунт, достаточный HTTP timeout (например, 60 секунд), память и ограничения параллельных запросов. Приложение слушает переменную `PORT`, которую выставляет платформа. Задайте обычные переменные:

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

Встроенные лимиты работают в памяти одного экземпляра. Если платформа запускает несколько экземпляров, общий расход может превышать `ANALYSES_PER_HOUR`; ограничьте масштабирование или настройте внешний gateway с общим лимитом перед открытым релизом.

## 4. Проверить релиз

Откройте `https://<container-id>.containers.yandexcloud.net/`. Затем проверьте `/api/healthz` и `/api/readyz`. Отправьте тестовое фото через PWA: без согласия запрос не должен уходить, после согласия должен появиться результат либо понятная ошибка низкой уверенности. Проверьте журналы на отсутствие изображения, API-ключа и тела запроса. Для проверки лимита отправьте серию запросов и убедитесь, что сервер возвращает 429 до обращения к AI.

После живой проверки укажите адрес контейнера как основной URL проекта. Статический Sites не проксирует запросы к Go серверу и не является полноценным релизом анализа.

## Источники

- [Yandex Cloud: Serverless Containers quickstart](https://yandex.cloud/en/docs/serverless-containers/quickstart/container)
- [Yandex Cloud: передача секретов Lockbox в контейнер](https://yandex.cloud/en/docs/lockbox/operations/serverless/containers)
- [Yandex Cloud: вызов контейнера по HTTPS](https://yandex.cloud/en/docs/serverless-containers/operations/invoke)
