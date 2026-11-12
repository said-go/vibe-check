# VibeCheck Backend

Go API для приёма фото, извлечения признаков одежды через Yandex AI Studio и детерминированного расчёта результата. Backend раздаёт `dist/` на `/` и API на `/api`.

## Запуск

Из каталога `backend/` выполните `go run ./cmd/api`, предварительно задав `YANDEX_API_KEY` и `YANDEX_FOLDER_ID` в окружении. Без них development сервер запускается для проверки интерфейса, а анализ возвращает `503 ai_unavailable`. В production отсутствие реквизитов блокирует запуск.

```text
GET  /api/healthz
GET  /api/readyz
POST /api/v1/analyses
```

`POST /api/v1/analyses` принимает ровно одно поле `image` (JPEG/PNG, 320×320 или больше, до 20 мегапикселей) и ровно одно `privacy_consent=true`. Дубли полей отклоняются. Максимальный размер запроса 10 МБ. Фото проверяется, повторно кодируется в JPEG без EXIF и отправляется провайдеру только после согласия. Наш сервер не сохраняет изображение.

## Настройки

| Переменная | По умолчанию |
|---|---|
| `APP_ENV` | `development` |
| `HTTP_ADDRESS` | `:8080` |
| `FRONTEND_DIR` | `../dist` |
| `HTTP_READ_TIMEOUT` | `10s` |
| `HTTP_WRITE_TIMEOUT` | `60s` |
| `HTTP_MAX_REQUEST_BYTES` | `10485760` |
| `AI_PROVIDER` | `yandex` |
| `YANDEX_API_KEY` | нет |
| `YANDEX_FOLDER_ID` | нет |
| `YANDEX_MODEL` | `qwen3.6-35b-a3b` |
| `YANDEX_BASE_URL` | `https://ai.api.cloud.yandex.net/v1` |
| `YANDEX_TIMEOUT` | `45s` |

Альтернативный провайдер `AI_PROVIDER=openai` использует `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` и `OPENAI_TIMEOUT`. Секреты держите в окружении хостинга, не в Git.

Для Yandex API ключу нужен scope `yc.ai.foundationModels.execute`, а сервисному аккаунту роль `ai.languageModels.user` для каталога. Подробнее в [документации Yandex AI Studio](https://aistudio.yandex.ru/en/docs/ai-studio/operations/generation/multimodels-request).

Строгий контракт признаков: [openapi/ai-features.schema.json](openapi/ai-features.schema.json). Он не содержит национальности, религии, привлекательности, здоровья и иных чувствительных выводов. HTTP контракт: [openapi/openapi.yaml](openapi/openapi.yaml).

## Проверка

```bash
go test ./...
go vet ./...
```
