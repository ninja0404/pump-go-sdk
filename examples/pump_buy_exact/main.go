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

// 示例：按 SOL 数量买入（buy_exact_sol_in），自动推导账户、自动补 ATA。
func main() {
	ctx := context.Background()

	const (
		mintStr      = "8GT663BCnPZ1nLFkFynZzquy3WGS9gMkugFtNKcrpump"
		spendableSol = uint64(4_000_000) // 允许花费的 SOL（lamports）
		minTokensOut = uint64(1)         // 至少获得的 token 数量（最小单位）
		rpcURL       = rpc.MainNetBeta_RPC
	)

	// 从环境变量读取私钥
	privateKeyB58 := os.Getenv("PUMP_PRIVATE_KEY")
	if privateKeyB58 == "" {
		log.Fatal("PUMP_PRIVATE_KEY environment variable is required")
	}

	mint := solana.MustPublicKeyFromBase58(mintStr)

	// 配置与客户端
	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpcURL
	cfg.Timeout = 20 * time.Second
	client := sdkrpc.NewClient(cfg)
	builder := txbuilder.NewBuilder(client, rpc.CommitmentConfirmed)

	// 签名者
	signer, err := wallet.NewLocalFromBase58(privateKeyB58)
	if err != nil {
		log.Fatalf("load signer from base58: %v", err)
	}
	user := signer.PublicKey()

	accounts, args, instrs, err := autofill.PumpBuyExactSolIn(ctx, client, user, mint, spendableSol, minTokensOut)
	if err != nil {
		log.Fatalf("autofill/build ix: %v", err)
	}

	tx, err := builder.BuildTransaction(ctx, signer.PublicKey(), instrs...)
	if err != nil {
		log.Fatalf("build tx: %v", err)
	}
	if err := txbuilder.SignTransaction(ctx, tx, signer); err != nil {
		log.Fatalf("sign: %v", err)
	}
	sig, err := builder.Send(ctx, tx)
	if err != nil {
		log.Fatalf("send: %v", err)
	}
	fmt.Printf("tx signature: %s\n", sig.String())
	_ = accounts
	_ = args
}
