package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// 示例：用固定 SOL 预算买入，自动 wrap WSOL、自动账户推导、自动滑点保护。
func main() {
	ctx := context.Background()

	const (
		poolStr     = "DaMVs5xtRhipFWNyZFRDVw44xuLPRRCeakJABthJ5UAr" // 替换为实际池子
		rpcURL      = rpc.MainNetBeta_RPC
		amountSol   = uint64(7_000_000) // lamports，将花费的 SOL 上限
		slippageBps = uint64(500)       // 5% 滑点
	)

	// 从环境变量读取私钥
	privateKeyB58 := os.Getenv("PUMP_PRIVATE_KEY")
	if privateKeyB58 == "" {
		log.Fatal("PUMP_PRIVATE_KEY environment variable is required")
	}

	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpcURL
	cfg.Timeout = 20 * time.Second
	client := sdkrpc.NewClient(cfg)
	builder := txbuilder.NewBuilder(client, "finalized")

	signer, err := wallet.NewLocalFromBase58(privateKeyB58)
	if err != nil {
		log.Fatalf("load signer: %v", err)
	}
	pool := solana.MustPublicKeyFromBase58(poolStr)

	accounts, args, instrs, simBase, err := autofill.PumpAmmBuyWithSol(ctx, client, signer.PublicKey(), pool, amountSol, slippageBps)
	if err != nil {
		log.Fatalf("build instruction: %v", err)
	}

	sig, err := builder.BuildSignSend(ctx, signer, nil, instrs...)
	if err != nil {
		log.Fatalf("send: %v", err)
	}

	fmt.Printf("simulated_base_out=%d min_base_out=%d sig=%s\n", simBase, args.MinBaseAmountOut, sig)
	_ = accounts
}
