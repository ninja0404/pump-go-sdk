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

// 示例：最小输入在池子内买 base token。
// 替换示例常量为实际主网数据。
func main() {
	ctx := context.Background()

	const (
		poolStr    = "DaMVs5xtRhipFWNyZFRDVw44xuLPRRCeakJABthJ5UAr"
		baseOut    = uint64(10_000)    // 期望获得的 base 数量
		maxQuoteIn = uint64(2_000_000) // 可支付的 quote 上限
		rpcURL     = rpc.MainNetBeta_RPC
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
	builder := txbuilder.NewBuilder(client, rpc.CommitmentConfirmed)

	signer, err := wallet.NewLocalFromBase58(privateKeyB58)
	if err != nil {
		log.Fatalf("load signer from base58: %v", err)
	}
	user := signer.PublicKey()

	accounts, args, instrs, err := autofill.PumpAmmBuy(ctx, client, user, pool, baseOut, maxQuoteIn)
	if err != nil {
		log.Fatalf("autofill/build ix: %v", err)
	}

	fmt.Printf("Pool: %s\n", pool)
	fmt.Printf("User: %s\n", user)
	fmt.Printf("Base out: %d tokens\n", baseOut)
	fmt.Printf("Max quote in: %d lamports (%.9f SOL)\n", maxQuoteIn, float64(maxQuoteIn)/1e9)

	sig, err := builder.BuildSignSendAndConfirm(ctx, signer, nil, txbuilder.ConfirmationConfirmed, instrs...)
	if err != nil {
		log.Fatalf("send: %v", err)
	}
	fmt.Printf("tx confirmed: %s\n", sig.String())

	_ = accounts
	_ = args
}
