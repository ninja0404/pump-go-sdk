package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// 示例：pump_amm 卖出，自动计算滑点。
func main() {
	ctx := context.Background()

	const (
		poolStr     = "DaMVs5xtRhipFWNyZFRDVw44xuLPRRCeakJABthJ5UAr" // 替换为实际池子
		slippageBps = uint64(200)                                    // 2% 滑点
		rpcURL      = solanarpc.MainNetBeta_RPC
	)

	// 从环境变量读取私钥
	privateKeyB58 := os.Getenv("PUMP_PRIVATE_KEY")
	if privateKeyB58 == "" {
		log.Fatal("PUMP_PRIVATE_KEY environment variable is required")
	}

	pool := solana.MustPublicKeyFromBase58(poolStr)

	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpcURL
	cfg.Timeout = 20 * time.Second
	client := sdkrpc.NewClient(cfg)
	builder := txbuilder.NewBuilder(client, solanarpc.CommitmentConfirmed)

	signer, err := wallet.NewLocalFromBase58(privateKeyB58)
	if err != nil {
		log.Fatalf("load signer: %v", err)
	}
	user := signer.PublicKey()

	// 获取 base ATA 并读取余额
	acctsPreview, _, _, err := autofill.PumpAmmSell(ctx, client, user, pool, 1, 0)
	if err != nil {
		log.Fatalf("autofill preview: %v", err)
	}
	baseIn, err := fetchTokenBalance(ctx, client, acctsPreview.UserBaseTokenAccount)
	if err != nil {
		log.Fatalf("fetch base balance: %v", err)
	}
	if baseIn == 0 {
		log.Fatalf("user base balance is zero at %s", acctsPreview.UserBaseTokenAccount)
	}

	fmt.Printf("Pool: %s\n", pool)
	fmt.Printf("User: %s\n", user)
	fmt.Printf("Selling: %d tokens\n", baseIn)

	// 使用 PumpAmmSellWithSlippage，它会自动创建 WSOL ATA（如果不存在）并模拟计算滑点
	accts, args, instrs, err := autofill.PumpAmmSellWithSlippage(ctx, client, user, pool, baseIn, slippageBps)
	if err != nil {
		log.Fatalf("sell with slippage: %v", err)
	}

	fmt.Printf("Simulated quote out: min_quote_out=%d lamports (%.9f SOL)\n",
		args.MinQuoteAmountOut, float64(args.MinQuoteAmountOut)/1e9)

	sig, err := builder.BuildSignSend(ctx, signer, nil, instrs...)
	if err != nil {
		log.Fatalf("send: %v", err)
	}
	fmt.Printf("tx: %s\n", sig)
	_ = accts
}

// fetchTokenBalance 获取 token 账户余额
func fetchTokenBalance(ctx context.Context, rpc *sdkrpc.Client, ata solana.PublicKey) (uint64, error) {
	res, err := rpc.Raw().GetTokenAccountBalance(ctx, ata, solanarpc.CommitmentConfirmed)
	if err != nil {
		return 0, err
	}
	if res == nil || res.Value == nil {
		return 0, fmt.Errorf("balance empty for %s", ata)
	}
	return strconv.ParseUint(res.Value.Amount, 10, 64)
}
