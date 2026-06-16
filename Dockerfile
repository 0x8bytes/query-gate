# syntax=docker/dockerfile:1

# ---- build stage ----
# 构建器始终原生运行（BUILDPLATFORM），通过 GOARCH 交叉编译到目标架构，
# 避免在 QEMU 模拟环境里跑 Go 工具链导致的运行时崩溃。
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

# 先拉依赖（利用层缓存：go.mod/go.sum 不变则跳过重新下载）
COPY go.mod go.sum ./
RUN go mod download

# 再复制源码并交叉编译静态二进制（纯 Go sqlite，无需 CGO）
ARG TARGETARCH
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/querygate ./cmd/server

# ---- runtime stage ----
FROM scratch
WORKDIR /app

# CA 证书：连接 TLS 数据库 / mongodb+srv 时需要
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/querygate /app/querygate
COPY config.example.yaml /app/config.example.yaml

# SQLite 默认落在 /data（在 compose / 部署平台挂载持久卷）
VOLUME ["/data"]
EXPOSE 8080

# 启动：有 config.yaml 用它，否则用内置示例
ENTRYPOINT ["/app/querygate"]
CMD ["-config", "/app/config.example.yaml"]
