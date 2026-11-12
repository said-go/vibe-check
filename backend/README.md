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
POST /v1/analyses
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
| `HTTP_MAX_REQUEST_BYTES` | `10485760` |
| `OPENAI_API_KEY` | отсутствует |
| `OPENAI_MODEL` | отсутствует |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` |
| `OPENAI_TIMEOUT` | `45s` |

`openapi/ai-features.schema.json` является версионированным контрактом ответа AI. Не добавляй в него национальность, религию, привлекательность, здоровье или другие чувствительные выводы.

## Анализ фотографии

`POST /v1/analyses` принимает одну фотографию в multipart-поле `image` и обязательное поле `privacy_consent=true`. Поддерживаются JPEG и PNG размером от 320×320 до 20 мегапикселей. Сервер:

1. проверяет сигнатуру файла и размеры до полного декодирования;
2. декодирует изображение только в памяти;
3. повторно кодирует его в JPEG, удаляя EXIF и геолокацию;
4. отправляет очищенную копию в OpenAI Responses API;
5. проверяет Structured Output по строгому контракту;
6. рассчитывает итог отдельным Scoring Engine.

Фотография не записывается нашим сервером на диск, в базу или в логи, но очищенная копия передаётся внешнему обработчику OpenAI только после явного согласия. Без `OPENAI_API_KEY` и `OPENAI_MODEL` сервер запускается в development, но endpoint анализа возвращает `ai_unavailable`. В production отсутствие этих переменных блокирует запуск.
