package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	"github.com/ninja0404/pump-go-sdk/pkg/jito"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// 示例：使用 Jito 发送 pump_amm 买入交易
// Jito 提供 MEV 保护和更快的交易落地
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		poolStr     = "DaMVs5xtRhipFWNyZFRDVw44xuLPRRCeakJABthJ5UAr" // 替换为实际池子
		amountSol   = uint64(1_000_000)                              // 0.001 SOL
		slippageBps = uint64(500)                                    // 5% 滑点
		priorityFee = uint64(1_000_000)                              // 0.001 SOL Priority Fee
		tipLamports = uint64(1_000_000)                              // 0.001 SOL tip to Jito
		rpcURL      = solanarpc.MainNetBeta_RPC
	)

	// 从环境变量读取私钥
	privateKeyB58 := os.Getenv("PUMP_PRIVATE_KEY")
	if privateKeyB58 == "" {
		log.Fatal("PUMP_PRIVATE_KEY environment variable is required")
	}

	pool := solana.MustPublicKeyFromBase58(poolStr)

	// 初始化 RPC 客户端
	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpcURL
	cfg.Timeout = 20 * time.Second
	client := sdkrpc.NewClient(cfg)

	// 初始化 Jito 客户端
	jitoClient := jito.NewClient("https://tokyo.mainnet.block-engine.jito.wtf/api/v1", "")

	// 初始化 Builder 并配置 Jito
	builder := txbuilder.NewBuilder(client, solanarpc.CommitmentConfirmed).
		WithJito(jitoClient).
		WithSkipPreflight(true) // Jito 通常建议跳过 preflight

	signer, err := wallet.NewLocalFromBase58(privateKeyB58)
	if err != nil {
		log.Fatalf("load signer: %v", err)
	}
	user := signer.PublicKey()

	fmt.Printf("Pool: %s\n", pool)
	fmt.Printf("User: %s\n", user)
	fmt.Printf("Amount: %d lamports (%.6f SOL)\n", amountSol, float64(amountSol)/1e9)
	fmt.Printf("Tip: %d lamports (%.6f SOL)\n", tipLamports, float64(tipLamports)/1e9)
	fmt.Printf("Using Jito: %v\n", builder.HasJito())

	// 构建买入指令，通过 WithJitoTip 选项自动添加 tip 转账
	_, args, instrs, simBase, err := autofill.PumpAmmBuyWithSol(ctx, client, user, pool, amountSol, slippageBps,
		autofill.WithPriorityFee(priorityFee),
		autofill.WithJitoTip(tipLamports), // 自动添加 Jito tip 转账指令
	)
	if err != nil {
		log.Fatalf("build instruction: %v", err)
	}

	fmt.Printf("Simulated base out: %d tokens\n", simBase)
	fmt.Printf("Min base out (after slippage): %d\n", args.MinBaseAmountOut)

	// 通过 Jito 发送交易
	sig, err := builder.BuildSignSendAndConfirm(ctx, signer, nil, txbuilder.ConfirmationConfirmed, instrs...)
	if err != nil {
		log.Fatalf("send via jito: %v", err)
	}

	fmt.Printf("\n✅ Transaction sent via Jito!\n")
	fmt.Printf("Signature: %s\n", sig)
	fmt.Printf("Explorer: https://solscan.io/tx/%s\n", sig)
}
