# pump-go-sdk

Pump.fun Bonding Curve、PumpSwap AMM 与 Pump Fees 的 Go SDK。协议层代码由 Pump 官方公开 IDL 生成，高层 API 覆盖当前用户交易路径。

## 特性

- 🚀 **自动账户推导** - 仅需提供最少参数，SDK 自动推导所有必要账户
- ⚡ **批量 RPC 优化** - 合并多个 RPC 调用，减少网络延迟
- 🛡️ **滑点保护** - 内置模拟和滑点计算
- 💰 **自动 WSOL 处理** - 自动 wrap/unwrap SOL ↔ WSOL
- 🔄 **交易确认** - 支持等待交易确认（processed/confirmed/finalized）
- 📝 **友好错误** - 清晰的错误消息，支持 Anchor 错误解析
- 🔧 **Token-2022** - 完整支持 Token-2022 标准
- 🪙 **统一 V2 交易** - 支持 SOL 与非原生 quote mint、cashback、Mayhem 和 buyback 账户
- 📦 **完整低层指令** - Pump、PumpSwap、Pump Fees 官方 IDL 中的指令均有 Go builder

## 安装

模块最低要求 Go 1.25.12，并在作为主模块开发时优先使用 Go 1.26.5；这两条版本线都包含 `crypto/tls` 的 [GO-2026-5856](https://pkg.go.dev/vuln/GO-2026-5856) 修复。

```bash
go get github.com/ninja0404/pump-go-sdk
```

## 快速开始

### 买入代币（推荐）

```go
import (
	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	"github.com/ninja0404/pump-go-sdk/pkg/config"
	"github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// 初始化
rpcConfig := config.DefaultRPCConfig()
rpcConfig.RPCURL = "https://api.mainnet-beta.solana.com"
rpcClient := rpc.NewClient(rpcConfig)
signer, _ := wallet.NewLocalFromBase58("your-base58-private-key")
builder := txbuilder.NewBuilder(rpcClient, solanarpc.CommitmentConfirmed)

// 使用 0.01 SOL 买入，1% 滑点
accts, args, instrs, simOut, err := autofill.PumpAmmBuyWithSol(
    ctx, rpcClient, signer.PublicKey(),
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
    ctx, rpcClient, signer.PublicKey(),
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
| `PumpAmmBuyExactQuoteIn` | 固定 quote token 原始单位买入，自定义最小输出 |
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
| `PumpBuyV2` | 当前统一买入，自动识别 SOL / 非原生 quote mint |
| `PumpBuyExactQuoteInV2` | 当前统一固定 quote 输入买入 |
| `PumpSellV2` | 当前统一卖出，支持 Token-2022 与非原生 quote mint |
| `PumpCreateV2` | 创建 Token-2022 coin，可通过 option 开启 cashback 或指定 quote mint |

### Pump V2 示例

```go
accounts, args, instructions, err := autofill.PumpBuyV2(
    ctx,
    rpcClient,
    signer.PublicKey(),
    mint,
    1_000_000,   // base token units
    10_000_000,  // max quote units
)

// 创建支持 cashback、以 USDC 为 quote 的 Token-2022 coin
createAccounts, createArgs, createInstruction, mintKey, err := autofill.PumpCreateV2(
    ctx,
    rpcClient,
    signer.PublicKey(),
    "My Token",
    "MTK",
    "https://example.com/metadata.json",
    false,
    autofill.WithCashbackEnabled(true),
    autofill.WithQuoteMint(usdcMint),
)
```

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

## 真实交易门禁

`e2e/TestRealTransactionRoundTrips` 使用不同阶段的代表性代币：新建的 `create_v2` Token-2022 mint 用于验证 Pump 内盘的 legacy 与 V2 买卖，另一个已毕业的 Token-2022 mint 用于验证 PumpSwap 外盘买卖；测试不要求两者是同一个 mint。每笔交易只发送一次，并校验确认回执、代币余额闭环、卖出后的 SOL 增量、外盘 base mint 的 Token-2022 owner 以及 PumpSwap 买入后的 WSOL 清理。普通 `go test ./...` 不会发送交易。

推荐先在 Surfpool 主网 fork 上执行真实字节码验证。测试会拒绝非 loopback 的 `surfnet` RPC：

```bash
surfpool start \
  --network mainnet \
  --port 18999 \
  --ws-port 19000 \
  --airdrop-keypair-path /path/to/devnet-only-keypair.json \
  --airdrop-amount 1000000000 \
  --no-deploy --yes --no-tui --no-studio

PUMP_E2E_ACK=I_UNDERSTAND_THIS_SENDS_TRANSACTIONS \
PUMP_E2E_NETWORK=surfnet \
PUMP_E2E_KEYPAIR=/path/to/devnet-only-keypair.json \
PUMP_E2E_RPC_URL=http://127.0.0.1:18999 \
PUMP_E2E_POOL=<mainnet-pumpswap-pool> \
go test ./e2e -run TestRealTransactionRoundTrips -v -count=1 -timeout=7m
```

公网主网验证必须使用专用小额钱包，并额外显式确认。`PUMP_E2E_MINT` 必须是尚未完成 bonding curve 的当前 V2 mint，`PUMP_E2E_POOL` 必须是 base mint 属于 Token-2022 的可交易 PumpSwap pool：

```bash
PUMP_E2E_ACK=I_UNDERSTAND_THIS_SENDS_TRANSACTIONS \
PUMP_E2E_MAINNET_ACK=I_UNDERSTAND_MAINNET_USES_REAL_FUNDS \
PUMP_E2E_NETWORK=mainnet \
PUMP_E2E_KEYPAIR=/path/to/dedicated-mainnet-keypair.json \
PUMP_E2E_RPC_URL=<private-mainnet-rpc> \
PUMP_E2E_MINT=<active-pump-v2-mint> \
PUMP_E2E_POOL=<active-pumpswap-pool> \
PUMP_E2E_TRADE_LAMPORTS=1000000 \
PUMP_E2E_MAX_LOSS_LAMPORTS=50000000 \
go test ./e2e -run TestRealTransactionRoundTrips -v -count=1 -timeout=7m
```

交易金额硬上限为 `2,000,000` lamports。不要粘贴或提交私钥；只传本机 `solana-keygen` JSON 文件路径。

非原生 quote mint 使用独立的 Surfpool E2E。它只接受 loopback RPC，通过 Surfpool 官方 `surfnet_setTokenAccount` 在本地分叉为测试钱包创建 100 USDC 余额，然后验证 USDC quote 的 `create_v2`、Pump V2 买卖和 PumpSwap 买卖：

```bash
PUMP_E2E_ACK=I_UNDERSTAND_THIS_SENDS_TRANSACTIONS \
PUMP_E2E_NETWORK=surfnet \
PUMP_E2E_KEYPAIR=/path/to/devnet-only-keypair.json \
PUMP_E2E_RPC_URL=http://127.0.0.1:18999 \
PUMP_E2E_USDC_POOL=<token-2022-cashback-usdc-pool> \
PUMP_E2E_USDC_AMOUNT=1000000 \
go test ./e2e -run TestSurfnetUSDCQuoteRoundTrips -v -count=1 -timeout=7m
```

USDC 单笔 quote 输入硬上限为 `2,000,000` 原始单位（2 USDC）。测试会校验 quote mint、Token Program、cashback pool、确认回执及 base/USDC 余额闭环；状态注入接口不会在 devnet 或公网 mainnet 模式运行。

Mayhem 分支使用独立的主网 fork 样本，显式校验内盘 BondingCurve 与外盘 Pool 的 `is_mayhem_mode`、`is_cashback_coin`，再执行 legacy、V2 与 PumpSwap 买卖：

```bash
PUMP_E2E_ACK=I_UNDERSTAND_THIS_SENDS_TRANSACTIONS \
PUMP_E2E_NETWORK=surfnet \
PUMP_E2E_KEYPAIR=/path/to/devnet-only-keypair.json \
PUMP_E2E_RPC_URL=http://127.0.0.1:18999 \
PUMP_E2E_POOL=<token-2022-mayhem-cashback-sol-pool> \
go test ./e2e -run TestSurfnetMayhemRoundTrips -v -count=1 -timeout=7m
```

## 构建与生成

```bash
# 生成程序代码（从 IDL 生成 Go 代码）
make gen

# 编译所有包
go build ./...

# 完整校验
go test ./...
go test -race ./...
go vet ./...

# 构建 CLI 工具
make build        # 输出到 bin/pumpcli

# 或安装到 $GOPATH/bin（全局可用）
make install      # 安装后可直接使用 pumpcli 命令
```

IDL 的官方来源、固定 commit、文件哈希与更新流程见 [idl/README.md](idl/README.md)。

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
