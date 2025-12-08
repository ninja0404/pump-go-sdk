# Examples

本目录包含 pump-go-sdk 的使用示例。

## 前置准备

1. **准备私钥**：使用 base58 格式的私钥字符串
2. **配置 RPC**：推荐使用付费 RPC 以获得更好的性能
3. **准备 SOL**：账户需要有足够的 SOL 支付交易费用

## 示例列表

### Pump AMM（推荐）

| 示例 | 文件 | 说明 |
|------|------|------|
| 买入（自动滑点） | `pump_amm_buy/main.go` | 用 SOL 买入代币，自动模拟计算滑点 |
| 卖出（自动滑点） | `pump_amm_sell/main.go` | 卖出代币，自动计算滑点，全部卖出时自动关闭 ATA |

### Pump（Bonding Curve）

| 示例 | 文件 | 说明 |
|------|------|------|
| 创建代币 | `pump_create/main.go` | 在 Pump.fun 创建新代币 |
| 买入 | `pump_buy/main.go` | 从 bonding curve 买入代币 |
| 卖出 | `pump_sell/main.go` | 卖出代币到 bonding curve |

## 运行示例

```bash
# 买入示例
go run examples/pump_amm_buy/main.go

# 卖出示例
go run examples/pump_amm_sell/main.go
```

## 代码示例

### 1. 初始化

```go
import (
    "context"
    
    "github.com/gagliardetto/solana-go"
    solanarpc "github.com/gagliardetto/solana-go/rpc"
    
    "github.com/ninja0404/pump-go-sdk/pkg/autofill"
    "github.com/ninja0404/pump-go-sdk/pkg/rpc"
    "github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
    "github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

func main() {
    ctx := context.Background()
    
    // 初始化 RPC 客户端
    rpcClient := rpc.NewClient("https://api.mainnet-beta.solana.com")
    
    // 加载钱包
    signer, err := wallet.NewLocalFromBase58("your-base58-private-key")
    if err != nil {
        log.Fatal(err)
    }
    
    // 创建交易构建器
    builder := txbuilder.NewBuilder(rpcClient, solanarpc.CommitmentConfirmed)
    
    // ...
}
```

### 2. 买入代币（PumpAmmBuyWithSol）

```go
pool := solana.MustPublicKeyFromBase58("池子地址")
quoteLamports := uint64(10_000_000)  // 0.01 SOL
slippageBps := uint64(100)           // 1% 滑点

// 构建交易
accts, args, instrs, simOut, err := autofill.PumpAmmBuyWithSol(
    ctx, rpcClient, signer,
    pool,
    quoteLamports,
    slippageBps,
)
if err != nil {
    log.Fatalf("构建交易失败: %v", err)
}

fmt.Printf("预计获得代币: %d\n", simOut)
fmt.Printf("最小获得（含滑点）: %d\n", args.MinBaseAmountOut)

// 发送并等待确认
sig, err := builder.BuildSignSendAndConfirm(
    ctx, signer, nil,
    txbuilder.ConfirmationConfirmed,
    instrs...,
)
if err != nil {
    log.Fatalf("交易失败: %v", err)
}

fmt.Printf("交易成功: https://solscan.io/tx/%s\n", sig)
```

### 3. 卖出代币（PumpAmmSellWithSlippage）

```go
pool := solana.MustPublicKeyFromBase58("池子地址")
baseIn := uint64(1_000_000)   // 卖出数量
slippageBps := uint64(100)    // 1% 滑点

// 构建交易
accts, args, instrs, err := autofill.PumpAmmSellWithSlippage(
    ctx, rpcClient, signer,
    pool,
    baseIn,
    slippageBps,
)
if err != nil {
    log.Fatalf("构建交易失败: %v", err)
}

fmt.Printf("最小获得 SOL: %d lamports\n", args.MinQuoteAmountOut)

// 发送并等待确认
sig, err := builder.BuildSignSendAndConfirm(
    ctx, signer, nil,
    txbuilder.ConfirmationConfirmed,
    instrs...,
)
if err != nil {
    log.Fatalf("交易失败: %v", err)
}

fmt.Printf("交易成功: https://solscan.io/tx/%s\n", sig)
```

### 4. 创建代币

#### SPL Token（PumpCreate）

```go
name := "My Token"
symbol := "MTK"
uri := "https://example.com/metadata.json"

// 方式 1：随机地址（默认，最快）
accts, args, ix, mintKey, err := autofill.PumpCreate(
    ctx, rpcClient, signer.PublicKey(),
    name, symbol, uri,
)

// 方式 2：Vanity 地址（以 "pump" 结尾）
accts, args, ix, mintKey, err := autofill.PumpCreate(
    ctx, rpcClient, signer.PublicKey(),
    name, symbol, uri,
    autofill.WithVanitySuffix("pump"),            // 地址将以 "pump" 结尾
    autofill.WithVanityTimeout(5 * time.Minute),  // 搜索超时时间
)

// 需要同时签名：用户 + mint 密钥
mintSigner := wallet.NewLocalFromPrivateKey(mintKey)
sig, err := builder.BuildSignSendAndConfirm(
    ctx, signer, []wallet.Signer{mintSigner},
    txbuilder.ConfirmationConfirmed,
    ix,
)
```

#### Token-2022（PumpCreateV2）

```go
// Token-2022 使用 PumpCreateV2
accts, args, ix, mintKey, err := autofill.PumpCreateV2(
    ctx, rpcClient, signer.PublicKey(),
    name, symbol, uri,
    false, // isMayhemMode
    autofill.WithVanitySuffix("pump"), // 可选：vanity 地址
)

// 同样需要两个签名
mintSigner := wallet.NewLocalFromPrivateKey(mintKey)
sig, err := builder.BuildSignSendAndConfirm(
    ctx, signer, []wallet.Signer{mintSigner},
    txbuilder.ConfirmationConfirmed,
    ix,
)
```

### Vanity Address 生成时间估算

| 后缀长度 | 预计尝试次数 | 大约时间 |
|---------|------------|---------|
| 1 字符 | ~58 | 毫秒级 |
| 2 字符 | ~3,364 | 毫秒级 |
| 3 字符 | ~195,112 | 秒级 |
| 4 字符 (pump) | ~11,316,496 | 1-5 秒 |
| 5 字符 | ~656,356,768 | 分钟级 |

### 5. Pump Bonding Curve 买入

```go
mint := solana.MustPublicKeyFromBase58("代币地址")
amount := uint64(1_000_000)       // 买入数量
maxSol := uint64(100_000_000)     // 最大支付 0.1 SOL

// 构建交易
accts, args, instrs, err := autofill.PumpBuy(
    ctx, rpcClient, signer.PublicKey(),
    mint, amount, maxSol,
)
if err != nil {
    log.Fatalf("构建交易失败: %v", err)
}

// 发送
sig, err := builder.BuildSignSendAndConfirm(
    ctx, signer, nil,
    txbuilder.ConfirmationConfirmed,
    instrs...,
)
```

### 6. Pump Bonding Curve 卖出（带滑点）

```go
mint := solana.MustPublicKeyFromBase58("代币地址")
amount := uint64(1_000_000)   // 卖出数量
slippageBps := uint64(100)    // 1% 滑点

// 构建交易（自动计算最小输出）
accts, args, instrs, err := autofill.PumpSellWithSlippage(
    ctx, rpcClient, signer,
    mint, amount, slippageBps,
)
if err != nil {
    log.Fatalf("构建交易失败: %v", err)
}

// 发送
sig, err := builder.BuildSignSendAndConfirm(
    ctx, signer, nil,
    txbuilder.ConfirmationConfirmed,
    instrs...,
)
```

## 错误处理

SDK 提供友好的错误消息：

```go
accts, args, instrs, err := autofill.PumpAmmSellWithSlippage(...)
if err != nil {
    // 错误消息示例：
    // - "program error [3012]: account 'user_base_token_account' not initialized"
    // - "program error [6023]: not enough tokens to sell"
    // - "validation error: amount - must be greater than 0"
    log.Fatalf("错误: %v", err)
}
```

## 交易确认级别

```go
// 三种确认级别
txbuilder.ConfirmationProcessed  // 最快，但可能被回滚
txbuilder.ConfirmationConfirmed  // 推荐，已被超级多数确认
txbuilder.ConfirmationFinalized  // 最安全，不可逆
```

## 注意事项

1. **滑点设置**：生产环境建议 50-200 bps (0.5%-2%)
2. **交易确认**：重要交易建议使用 `ConfirmationConfirmed` 或 `ConfirmationFinalized`
3. **错误重试**：网络错误可以重试，程序错误（如余额不足）无需重试
4. **RPC 选择**：高频交易建议使用付费 RPC 服务

## CLI 使用

除了 Go 代码，也可以使用 CLI 工具。

### 安装 CLI

```bash
# 在项目根目录执行
make install

# 验证
pumpcli --help
```

### CLI 示例

```bash
# 创建 SPL Token（随机地址）
pumpcli pump create \
    --rpc-url https://api.mainnet-beta.solana.com \
    --fee-payer ~/.config/solana/id.json \
    --name "My Token" \
    --symbol "MTK" \
    --uri "https://example.com/metadata.json"

# 创建 SPL Token（Vanity 地址，以 "pump" 结尾）
pumpcli pump create \
    --rpc-url https://api.mainnet-beta.solana.com \
    --fee-payer ~/.config/solana/id.json \
    --name "My Token" \
    --symbol "MTK" \
    --uri "https://example.com/metadata.json" \
    --suffix pump

# 创建 Token-2022（使用 create-v2 命令）
pumpcli pump create-v2 \
    --rpc-url https://api.mainnet-beta.solana.com \
    --fee-payer ~/.config/solana/id.json \
    --name "My Token" \
    --symbol "MTK" \
    --uri "https://example.com/metadata.json" \
    --suffix pump

# 买入代币（Pump AMM）
pumpcli pump-amm buy-sol \
    --rpc-url https://api.mainnet-beta.solana.com \
    --fee-payer ~/.config/solana/id.json \
    --pool <pool-address> \
    --amount-sol 10000000 \
    --slippage-bps 100

# 卖出代币（Pump AMM）
pumpcli pump-amm sell \
    --rpc-url https://api.mainnet-beta.solana.com \
    --fee-payer ~/.config/solana/id.json \
    --pool <pool-address> \
    --base-in 1000000 \
    --min-quote-out 500000

# 查看池子信息
pumpcli pump-amm pool-info \
    --rpc-url https://api.mainnet-beta.solana.com \
    --pool <pool-address>

# 查看帮助
pumpcli --help
pumpcli pump --help
pumpcli pump create --help
pumpcli pump-amm --help
```

