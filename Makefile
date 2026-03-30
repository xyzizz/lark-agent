.PHONY: run build tidy clean

# 默认目标：直接运行
run:
	go run ./cmd/server/main.go -config ./config.yaml

# 编译二进制
build:
	go build -o ./bin/feishu-agent ./cmd/server/main.go

# 整理依赖
tidy:
	go mod tidy

# 清理
clean:
	rm -f ./bin/feishu-agent ./feishu-agent.db

# 格式化代码
fmt:
	gofmt -w ./...

# 运行测试
test:
	go test ./...
