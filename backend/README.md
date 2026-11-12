# VibeCheck Backend

Первый backend-этап: HTTP-каркас, строгий контракт признаков Vision AI и детерминированный Scoring Engine.

## Запуск

```bash
go run ./cmd/api
```

По умолчанию API работает на `http://localhost:8080`.

```text
GET  /healthz
GET  /readyz
POST /v1/scoring/evaluate
```

## Проверка

```bash
go test ./...
go vet ./...
```

## Конфигурация

| Переменная | Значение по умолчанию |
|---|---|
| `APP_ENV` | `development` |
| `HTTP_ADDRESS` | `:8080` |
| `HTTP_READ_TIMEOUT` | `10s` |
| `HTTP_WRITE_TIMEOUT` | `20s` |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` |
| `HTTP_MAX_REQUEST_BYTES` | `1048576` |

`openapi/ai-features.schema.json` является версионированным контрактом ответа AI. Не добавляй в него национальность, религию, привлекательность, здоровье или другие чувствительные выводы.

## Текущая граница

API пока не принимает фотографии и не вызывает внешний AI. Endpoint `/v1/scoring/evaluate` проверяет следующий готовый слой: строгие визуальные признаки → Scoring Engine → категория и уровень образа. Загрузка фото и Vision-провайдер реализуются отдельными вертикальными этапами.
