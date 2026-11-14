# 依赖源可以用 --build-arg 覆盖：默认走国内镜像，境外网络传官方源即可
#   docker build -f deploy/Dockerfile \
#     --build-arg NPM_REGISTRY=https://registry.npmjs.org \
#     --build-arg GOPROXY=https://proxy.golang.org,direct -t gorixpay .
ARG NPM_REGISTRY=https://registry.npmmirror.com
ARG GOPROXY=https://goproxy.cn|https://proxy.golang.org|direct

FROM node:22-alpine AS web
ARG NPM_REGISTRY
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
# lock 里的 resolved 指向官方源，先改 registry 再装，否则容器里拉不动
RUN npm config set registry "$NPM_REGISTRY" && npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.21-alpine AS builder
ARG GOPROXY
ENV GOPROXY=$GOPROXY
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gorixpay ./cmd/gorixpay

FROM alpine:3.20
# Pin the UID/GID so a bind mount can be chowned to a documented number.
RUN addgroup -S -g 10001 gorixpay && adduser -S -u 10001 -G gorixpay gorixpay
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
