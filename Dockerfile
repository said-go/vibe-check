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
ENV APP_ENV=production FRONTEND_DIR=/app/dist HTTP_ADDRESS=:8080 AI_PROVIDER=yandex
EXPOSE 8080
CMD ["/app/vibe-api"]
