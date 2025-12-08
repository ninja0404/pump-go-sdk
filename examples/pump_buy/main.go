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

// 示例：以最少字段调用 SDK 构建并发送 pump buy 交易。
// 请替换示例常量为真实主网地址。
func main() {
	ctx := context.Background()

	const (
		mintStr = "8GT663BCnPZ1nLFkFynZzquy3WGS9gMkugFtNKcrpump"
		amount  = uint64(100_000_000) // 代币最小单位
		maxSol  = uint64(2_000_000)   // 可接受的 SOL 上限（lamports）
		rpcURL  = rpc.MainNetBeta_RPC
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
	user := signer.PublicKey() // 确保 user 与签名者一致

	// 自动推导账户（与 CLI 自动推导一致）
	accounts, args, instrs, err := autofill.PumpBuy(ctx, client, user, mint, amount, maxSol)
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
