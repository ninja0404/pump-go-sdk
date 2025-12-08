# pump-go-sdk

Pump.fun 和 Pump AMM 的 Go SDK，提供完整的交易构建、发送和确认功能。

## 特性

- 🚀 **自动账户推导** - 仅需提供最少参数，SDK 自动推导所有必要账户
- ⚡ **批量 RPC 优化** - 合并多个 RPC 调用，减少网络延迟
- 🛡️ **滑点保护** - 内置模拟和滑点计算
- 💰 **自动 WSOL 处理** - 自动 wrap/unwrap SOL ↔ WSOL
- 🔄 **交易确认** - 支持等待交易确认（processed/confirmed/finalized）
- 📝 **友好错误** - 清晰的错误消息，支持 Anchor 错误解析
- 🔧 **Token-2022** - 完整支持 Token-2022 标准

## 安装

```bash
go get github.com/ninja0404/pump-go-sdk
```

## 快速开始

### 买入代币（推荐）

```go
import (
    "github.com/ninja0404/pump-go-sdk/pkg/autofill"
    "github.com/ninja0404/pump-go-sdk/pkg/rpc"
    "github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
    "github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// 初始化
rpcClient := rpc.NewClient("https://api.mainnet-beta.solana.com")
signer, _ := wallet.NewLocalFromBase58("your-base58-private-key")
builder := txbuilder.NewBuilder(rpcClient, solanarpc.CommitmentConfirmed)

// 使用 0.01 SOL 买入，1% 滑点
accts, args, instrs, simOut, err := autofill.PumpAmmBuyWithSol(
    ctx, rpcClient, signer, 
    poolAddress,      // 池子地址
    10_000_000,       // 0.01 SOL (lamports)
    100,              // 1% 滑点 (basis points)
)

// 发送并等待确认
sig, err := builder.BuildSignSendAndConfirm(ctx, signer, nil, txbuilder.ConfirmationConfirmed, instrs...)
fmt.Printf("交易成功: %s\n", sig)
```

### 卖出代币

```go
// 卖出全部代币，1% 滑点
accts, args, instrs, err := autofill.PumpAmmSellWithSlippage(
    ctx, rpcClient, signer,
    poolAddress,     // 池子地址
    tokenAmount,     // 卖出数量
    100,             // 1% 滑点
)

sig, err := builder.BuildSignSendAndConfirm(ctx, signer, nil, txbuilder.ConfirmationConfirmed, instrs...)
```

## 核心函数

### Pump AMM（池子交易）

| 函数 | 说明 |
|------|------|
| `PumpAmmBuyWithSol` | 用 SOL 买入，自动滑点计算（推荐） |
| `PumpAmmBuyExactQuoteIn` | 固定 SOL 买入，自定义最小输出 |
| `PumpAmmBuy` | 底层买入，指定精确输出数量 |
| `PumpAmmSellWithSlippage` | 卖出代币，自动滑点计算（推荐） |
| `PumpAmmSell` | 底层卖出 |

### Pump（Bonding Curve）

| 函数 | 说明 |
|------|------|
| `PumpBuy` | 买入代币 |
| `PumpBuyExactSolIn` | 固定 SOL 买入 |
| `PumpSellWithSlippage` | 卖出代币，自动滑点计算（推荐） |
| `PumpSell` | 底层卖出 |

## 错误处理

SDK 提供清晰的错误消息：

```go
// 旧格式（难读）
// simulate err: map[InstructionError:[0 map[Custom:3012]]], logs: [...]

// 新格式（清晰）
// program error [3012]: account 'user_base_token_account' not initialized (create the account first)
```

常见错误码：
- `3012` - 账户未初始化
- `6001` - 零数量交易
- `6023` - 代币余额不足
- `6003/6024` - 滑点超限

## 构建与生成

```bash
# 生成程序代码（从 IDL 生成 Go 代码）
make gen

# 编译所有包
go build ./...

# 构建 CLI 工具
make build        # 输出到 bin/pumpcli

# 或安装到 $GOPATH/bin（全局可用）
make install      # 安装后可直接使用 pumpcli 命令
```

## CLI 安装

```bash
# 从源码安装（推荐）
git clone https://github.com/ninja0404/pump-go-sdk.git
cd pump-go-sdk
make install    # 安装 pumpcli 到 $GOPATH/bin

# 验证安装
pumpcli --help
```

## CLI 使用

### 全局参数
- `--rpc-url` RPC 地址（默认 mainnet）
- `--commitment` 承诺级别（默认 finalized）
- `--fee-payer` 密钥文件路径
- `--log-level` 日志级别（debug/info/warn/error）

### Pump 指令
```bash
# 买入
pumpcli pump buy --mint <mint> --user <user> --amount 1000000 --max-sol-cost 1000000

# 卖出
pumpcli pump sell --mint <mint> --user <user> --amount 1000000 --min-sol-output 500000

# 查看信息
pumpcli pump info --mint <mint>
```

### Pump AMM 指令
```bash
# 用 SOL 买入（推荐）
pumpcli pump-amm buy-sol --pool <pool> --amount-sol 2000000 --slippage-bps 100

# 卖出
pumpcli pump-amm sell --pool <pool> --base-in 1000000 --min-quote-out 500000

# 查看池子信息
pumpcli pump-amm pool-info --pool <pool>
```

## License

MIT
