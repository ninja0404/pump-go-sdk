SHELL := /bin/sh

.PHONY: gen tidy build install clean

# 生成程序代码
gen:
	go run ./internal/gen --idl idl/pump.json --pkg pump --out pkg/program/pump
	go run ./internal/gen --idl idl/pump_amm.json --pkg pumpamm --out pkg/program/pumpamm
	go run ./internal/gen --idl idl/pump_fees.json --pkg pumpfees --out pkg/program/pumpfees

# 整理依赖
tidy:
	go mod tidy

# 构建 CLI 到 bin/ 目录
build:
	go build -o bin/pumpcli ./cmd/cli

# 安装 CLI 到 $GOPATH/bin（重命名为 pumpcli）
install:
	go build -o $(shell go env GOPATH)/bin/pumpcli ./cmd/cli

# 清理构建产物
clean:
	rm -rf bin/

