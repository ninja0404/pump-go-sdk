package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/vanity"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

func main() {
	ctx := context.Background()

	// 从环境变量读取私钥
	privateKeyB58 := os.Getenv("PUMP_PRIVATE_KEY")
	if privateKeyB58 == "" {
		log.Fatal("PUMP_PRIVATE_KEY environment variable is required")
	}

	// Initialize RPC client
	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpc.MainNetBeta_RPC
	cfg.Timeout = 30 * time.Second
	rpcClient := sdkrpc.NewClient(cfg)

	// Load wallet
	signer, err := wallet.NewLocalFromBase58(privateKeyB58)
	if err != nil {
		log.Fatalf("load wallet: %v", err)
	}
	user := signer.PublicKey()

	// Token metadata
	name := "My Awesome Token"
	symbol := "MAT"
	uri := "https://example.com/metadata.json" // Should point to a valid JSON metadata file

	// ============================================
	// Option 1: Random address (default, fastest)
	// ============================================
	// opts := []autofill.Option{}

	// ============================================
	// Option 2: Vanity address ending with "pump"
	// ============================================
	fmt.Println("🔍 Searching for vanity address ending with 'pump'...")
	fmt.Printf("   Estimated difficulty: ~%d attempts\n", vanity.EstimateDifficulty(0, 4))

	opts := []autofill.Option{
		autofill.WithVanitySuffix("pump"),            // Address will end with "pump"
		autofill.WithVanityTimeout(10 * time.Minute), // Max search time
	}

	startTime := time.Now()

	// ============================================
	// Create SPL Token (use PumpCreate)
	// ============================================
	accts, args, ix, mintKey, err := autofill.PumpCreate(ctx, rpcClient, user, name, symbol, uri, opts...)
	if err != nil {
		log.Fatalf("build create: %v", err)
	}

	// ============================================
	// For Token-2022, use PumpCreateV2 instead:
	// ============================================
	// accts, args, ix, mintKey, err := autofill.PumpCreateV2(
	//     ctx, rpcClient, user, name, symbol, uri,
	//     false,  // isMayhemMode
	//     opts...,
	// )

	fmt.Printf("✅ Found vanity address in %s\n\n", time.Since(startTime))

	fmt.Printf("Creating token:\n")
	fmt.Printf("  Name: %s\n", args.Name)
	fmt.Printf("  Symbol: %s\n", args.Symbol)
	fmt.Printf("  URI: %s\n", args.Uri)
	fmt.Printf("  Mint: %s\n", mintKey.PublicKey())
	fmt.Printf("  BondingCurve: %s\n", accts.BondingCurve)
	fmt.Printf("  Creator: %s\n", args.Creator)

	// Build transaction builder
	builder := txbuilder.NewBuilder(rpcClient, rpc.CommitmentConfirmed)

	// Create mint signer wrapper
	mintSigner := wallet.NewLocalFromPrivateKey(mintKey)

	// Send transaction with both signers (user + mint)
	sig, err := builder.BuildSignSendAndConfirm(
		ctx,
		signer,                      // primary signer (fee payer)
		[]wallet.Signer{mintSigner}, // additional signers (mint)
		txbuilder.ConfirmationConfirmed,
		ix,
	)
	if err != nil {
		log.Fatalf("send transaction: %v", err)
	}

	fmt.Printf("\n✅ Token created successfully!\n")
	fmt.Printf("Transaction: https://solscan.io/tx/%s\n", sig)
	fmt.Printf("Token: https://pump.fun/coin/%s\n", mintKey.PublicKey())

	// Save mint keypair (important!)
	fmt.Printf("\n⚠️  Save the mint private key:\n")
	fmt.Printf("  %s\n", mintKey)
}
