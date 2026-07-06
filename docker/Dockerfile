# 构建阶段
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o webserver ./cmd/webserver
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o envinit ./cmd/envinit

# 运行阶段
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

WORKDIR /app

COPY --from=builder /app/webserver /app/envinit ./

EXPOSE 30000

CMD ["/app/webserver"]
