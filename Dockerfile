FROM golang:1.25-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod ./
COPY backend/cmd ./cmd
COPY backend/internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /vibe-api ./cmd/api

FROM alpine:3.21
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /vibe-api /app/vibe-api
COPY dist /app/dist
USER app
ENV APP_ENV=production FRONTEND_DIR=/app/dist AI_PROVIDER=yandex
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD wget -q -O /dev/null http://127.0.0.1:${PORT:-8080}/api/healthz || exit 1
CMD ["/app/vibe-api"]
