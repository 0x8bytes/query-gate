# QueryGate Makefile
# 用法:在仓库根目录执行 make <target>

# 可覆盖:make server CONFIG=config.product.yaml
CONFIG ?= config.yaml
BIN    ?= bin/querygate
PKG    := ./cmd/server

.PHONY: help server run build test vet race clean tidy

help: ## 显示可用命令
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

server: ## 用 config.yaml 启动服务(开发,go run)
	go run $(PKG) -config $(CONFIG)

run: server ## server 的别名

build: ## 编译二进制到 bin/querygate
	@mkdir -p bin
	go build -o $(BIN) $(PKG)

test: ## 运行全部测试
	go test ./...

vet: ## go vet 静态检查
	go vet ./...

race: ## 带竞态检测运行测试
	go test -race ./...

tidy: ## 整理 go.mod/go.sum
	go mod tidy

clean: ## 清理编译产物与本地 SQLite
	rm -rf bin querygate.db
