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
RUN mkdir -p /app/data /run/secrets && chown -R gorixpay:gorixpay /app /run/secrets
USER gorixpay
EXPOSE 8080
ENTRYPOINT ["gorixpay"]
CMD ["--http-address", ":8080", "--database-driver", "sqlite", "--database-dsn", "/app/data/gorixpay.db"]
