# syntax=docker/dockerfile:1
# 多阶段构建：Go 编译 + alpine 运行时（非 root + healthcheck）。
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/trae2api ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache wget ca-certificates tzdata \
 && adduser -D -u 10001 app \
 && mkdir -p /app/auths /app/data \
 && chown -R app:app /app
USER app
WORKDIR /app
COPY --from=build /out/trae2api /app/trae2api
EXPOSE 7864
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:7864/healthz || exit 1
ENTRYPOINT ["/app/trae2api", "-config", "/app/config.json"]
