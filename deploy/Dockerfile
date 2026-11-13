FROM node:22-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.21-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gorixpay ./cmd/gorixpay

FROM alpine:3.20
RUN addgroup -S gorixpay && adduser -S -G gorixpay gorixpay
WORKDIR /app
COPY --from=builder /out/gorixpay /usr/local/bin/gorixpay
COPY --from=web /web/dist /app/frontend/dist
RUN mkdir -p /app/data /run/secrets && chown -R gorixpay:gorixpay /app /run/secrets
USER gorixpay
EXPOSE 8080
ENTRYPOINT ["gorixpay"]
# 只给监听地址，数据库参数留给环境变量：命令行参数优先级高于环境变量，
# 在这里写死 sqlite 会把 compose 里的 postgres 配置顶掉。
CMD ["--http-address", ":8080"]
