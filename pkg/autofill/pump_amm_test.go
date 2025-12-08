package autofill_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// Test configuration - set via environment variables
// PUMP_TEST_RPC_URL: RPC endpoint (default: mainnet)
// PUMP_TEST_PRIVATE_KEY: Base58 encoded private key
// PUMP_TEST_POOL: AMM pool address to test

func getTestConfig(t *testing.T) (rpcURL, privateKey, pool string) {
	rpcURL = os.Getenv("PUMP_TEST_RPC_URL")
	if rpcURL == "" {
		rpcURL = solanarpc.MainNetBeta_RPC
	}

	privateKey = os.Getenv("PUMP_TEST_PRIVATE_KEY")
	if privateKey == "" {
		t.Skip("PUMP_TEST_PRIVATE_KEY not set, skipping integration test")
	}

	pool = os.Getenv("PUMP_TEST_POOL")
	if pool == "" {
		t.Skip("PUMP_TEST_POOL not set, skipping integration test")
	}

	return rpcURL, privateKey, pool
}

// TestPumpAmmBuyThenSell tests buying tokens and immediately selling them.
// The sell amount is obtained from the buy transaction result, not from RPC query.
func TestPumpAmmBuyThenSell(t *testing.T) {
	rpcURL, privateKeyStr, poolStr := getTestConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Initialize RPC client
	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpcURL
	cfg.Timeout = 30 * time.Second
	rpcClient := sdkrpc.NewClient(cfg)

	// Load wallet
	signer, err := wallet.NewLocalFromBase58(privateKeyStr)
	if err != nil {
		t.Fatalf("load wallet: %v", err)
	}

	pool := solana.MustPublicKeyFromBase58(poolStr)

	// Test parameters
	buyAmountLamports := uint64(1_000_000) // 0.001 SOL
	slippageBps := uint64(500)             // 5% slippage

	t.Logf("Test configuration:")
	t.Logf("  Pool: %s", pool)
	t.Logf("  User: %s", signer.PublicKey())
	t.Logf("  Buy amount: %d lamports (%.6f SOL)", buyAmountLamports, float64(buyAmountLamports)/1e9)
	t.Logf("  Slippage: %d bps (%.2f%%)", slippageBps, float64(slippageBps)/100)

	// Step 1: Buy tokens
	t.Log("\n=== Step 1: Buy tokens ===")
	buyAccts, buyArgs, buyInstrs, simOut, err := autofill.PumpAmmBuyWithSol(
		ctx, rpcClient, signer, pool, buyAmountLamports, slippageBps,
	)
	if err != nil {
		t.Fatalf("build buy: %v", err)
	}

	t.Logf("  Simulated output: %d tokens", simOut)
	t.Logf("  Min base out (after slippage): %d", buyArgs.MinBaseAmountOut)
	t.Logf("  Base mint: %s", buyAccts.BaseMint)
	t.Logf("  User base ATA: %s", buyAccts.UserBaseTokenAccount)

	// Send buy transaction
	builder := txbuilder.NewBuilder(rpcClient, solanarpc.CommitmentConfirmed)
	buySig, err := builder.BuildSignSendAndConfirm(
		ctx, signer, nil, txbuilder.ConfirmationConfirmed, buyInstrs...,
	)
	if err != nil {
		t.Fatalf("send buy tx: %v", err)
	}
	t.Logf("  Buy tx: %s", buySig)

	// Step 2: Get actual tokens bought from transaction result
	t.Log("\n=== Step 2: Parse buy transaction result ===")

	tokensReceived, err := getTokensReceivedFromTx(ctx, rpcClient.Raw(), buySig, signer.PublicKey(), buyAccts.BaseMint)
	if err != nil {
		t.Fatalf("parse tx result: %v", err)
	}
	t.Logf("  Tokens received (from tx): %d", tokensReceived)

	if tokensReceived == 0 {
		t.Fatal("no tokens received from buy")
	}

	// Step 3: Sell all tokens using the amount from transaction result
	t.Log("\n=== Step 3: Sell all tokens ===")

	// Estimate expected quote output (≈ buy amount for small trades, accounting for fees)
	// For immediate sell after buy, the price should be similar
	estimatedQuoteOut := buyAmountLamports * 95 / 100 // ~5% less due to fees/slippage

	// Use WithKnownATAs + WithExpectedQuoteOut to skip RPC queries entirely
	sellAccts, sellArgs, sellInstrs, err := autofill.PumpAmmSellWithSlippage(
		ctx, rpcClient, signer, pool, tokensReceived, slippageBps,
		autofill.WithKnownATAs(buyAccts.UserBaseTokenAccount, buyAccts.UserQuoteTokenAccount),
		autofill.WithExpectedQuoteOut(estimatedQuoteOut),
	)
	if err != nil {
		t.Fatalf("build sell: %v", err)
	}

	t.Logf("  Selling: %d tokens (from buy tx result)", sellArgs.BaseAmountIn)
	t.Logf("  Min quote out (after slippage): %d lamports", sellArgs.MinQuoteAmountOut)

	// Verify we're selling the tokens we bought
	if sellAccts.BaseMint != buyAccts.BaseMint {
		t.Fatalf("mint mismatch: buy=%s, sell=%s", buyAccts.BaseMint, sellAccts.BaseMint)
	}

	// Send sell transaction
	sellSig, err := builder.BuildSignSendAndConfirm(
		ctx, signer, nil, txbuilder.ConfirmationConfirmed, sellInstrs...,
	)
	if err != nil {
		t.Fatalf("send sell tx: %v", err)
	}
	t.Logf("  Sell tx: %s", sellSig)

	// Step 4: Verify sell result
	t.Log("\n=== Step 4: Verify sell result ===")

	solReceived, err := getSOLReceivedFromTx(ctx, rpcClient.Raw(), sellSig, signer.PublicKey())
	if err != nil {
		t.Logf("  Could not parse SOL received: %v", err)
	} else {
		t.Logf("  SOL received: %d lamports (%.6f SOL)", solReceived, float64(solReceived)/1e9)
	}

	t.Log("\n=== Test completed successfully ===")
}

// TestPumpAmmBuyExactAndSell tests buying with exact quote input and selling.
func TestPumpAmmBuyExactAndSell(t *testing.T) {
	rpcURL, privateKeyStr, poolStr := getTestConfig(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpcURL
	cfg.Timeout = 30 * time.Second
	rpcClient := sdkrpc.NewClient(cfg)

	signer, err := wallet.NewLocalFromBase58(privateKeyStr)
	if err != nil {
		t.Fatalf("load wallet: %v", err)
	}

	pool := solana.MustPublicKeyFromBase58(poolStr)

	buyAmountLamports := uint64(1_000_000)
	minBaseOut := uint64(1)
	slippageBps := uint64(500)

	t.Logf("Test: Buy exact quote -> Sell all")
	t.Logf("  Pool: %s", pool)
	t.Logf("  Buy amount: %d lamports", buyAmountLamports)

	// Buy with exact quote input
	buyAccts, buyArgs, buyInstrs, err := autofill.PumpAmmBuyExactQuoteIn(
		ctx, rpcClient, signer, pool, buyAmountLamports, minBaseOut,
	)
	if err != nil {
		t.Fatalf("build buy: %v", err)
	}
	t.Logf("  Min base out: %d", buyArgs.MinBaseAmountOut)

	builder := txbuilder.NewBuilder(rpcClient, solanarpc.CommitmentConfirmed)
	buySig, err := builder.BuildSignSendAndConfirm(
		ctx, signer, nil, txbuilder.ConfirmationConfirmed, buyInstrs...,
	)
	if err != nil {
		t.Fatalf("send buy tx: %v", err)
	}
	t.Logf("  Buy tx: %s", buySig)

	// Get tokens from tx result
	tokensReceived, err := getTokensReceivedFromTx(ctx, rpcClient.Raw(), buySig, signer.PublicKey(), buyAccts.BaseMint)
	if err != nil {
		t.Fatalf("parse tx result: %v", err)
	}
	t.Logf("  Tokens received (from tx): %d", tokensReceived)

	if tokensReceived == 0 {
		t.Fatal("no tokens received")
	}

	// Estimate expected quote output
	estimatedQuoteOut := buyAmountLamports * 95 / 100

	// Sell all using WithKnownATAs + WithExpectedQuoteOut to skip RPC queries
	_, sellArgs, sellInstrs, err := autofill.PumpAmmSellWithSlippage(
		ctx, rpcClient, signer, pool, tokensReceived, slippageBps,
		autofill.WithKnownATAs(buyAccts.UserBaseTokenAccount, buyAccts.UserQuoteTokenAccount),
		autofill.WithExpectedQuoteOut(estimatedQuoteOut),
	)
	if err != nil {
		t.Fatalf("build sell: %v", err)
	}
	t.Logf("  Selling: %d tokens", sellArgs.BaseAmountIn)

	sellSig, err := builder.BuildSignSendAndConfirm(
		ctx, signer, nil, txbuilder.ConfirmationConfirmed, sellInstrs...,
	)
	if err != nil {
		t.Fatalf("send sell tx: %v", err)
	}
	t.Logf("  Sell tx: %s", sellSig)

	t.Log("Test completed successfully")
}

// getTokensReceivedFromTx parses the transaction to get the actual tokens received.
// It calculates: postBalance - preBalance for the specified mint.
func getTokensReceivedFromTx(
	ctx context.Context,
	rpc *solanarpc.Client,
	sig solana.Signature,
	owner solana.PublicKey,
	mint solana.PublicKey,
) (uint64, error) {
	version := uint64(0)
	tx, err := rpc.GetTransaction(ctx, sig, &solanarpc.GetTransactionOpts{
		Commitment:                     solanarpc.CommitmentConfirmed,
		MaxSupportedTransactionVersion: &version,
	})
	if err != nil {
		return 0, fmt.Errorf("get transaction: %w", err)
	}
	if tx == nil || tx.Meta == nil {
		return 0, fmt.Errorf("transaction not found or no meta")
	}

	// Find pre and post balance for the specified mint owned by the user
	var preBalance, postBalance uint64

	for _, b := range tx.Meta.PreTokenBalances {
		if b.Owner != nil && b.Owner.String() == owner.String() && b.Mint.String() == mint.String() {
			preBalance, _ = strconv.ParseUint(b.UiTokenAmount.Amount, 10, 64)
			break
		}
	}

	for _, b := range tx.Meta.PostTokenBalances {
		if b.Owner != nil && b.Owner.String() == owner.String() && b.Mint.String() == mint.String() {
			postBalance, _ = strconv.ParseUint(b.UiTokenAmount.Amount, 10, 64)
			break
		}
	}

	if postBalance > preBalance {
		return postBalance - preBalance, nil
	}
	return 0, nil
}

// getSOLReceivedFromTx parses the transaction to get the SOL received (for sells).
// It calculates: postBalance - preBalance for the user's SOL account.
func getSOLReceivedFromTx(
	ctx context.Context,
	rpc *solanarpc.Client,
	sig solana.Signature,
	owner solana.PublicKey,
) (uint64, error) {
	version := uint64(0)
	tx, err := rpc.GetTransaction(ctx, sig, &solanarpc.GetTransactionOpts{
		Commitment:                     solanarpc.CommitmentConfirmed,
		MaxSupportedTransactionVersion: &version,
	})
	if err != nil {
		return 0, fmt.Errorf("get transaction: %w", err)
	}
	if tx == nil || tx.Meta == nil {
		return 0, fmt.Errorf("transaction not found or no meta")
	}

	// Find the owner's account index
	decodedTx, err := tx.Transaction.GetTransaction()
	if err != nil {
		return 0, fmt.Errorf("decode transaction: %w", err)
	}

	ownerIdx := -1
	for i, key := range decodedTx.Message.AccountKeys {
		if key.String() == owner.String() {
			ownerIdx = i
			break
		}
	}

	if ownerIdx < 0 || ownerIdx >= len(tx.Meta.PreBalances) || ownerIdx >= len(tx.Meta.PostBalances) {
		return 0, fmt.Errorf("owner account not found in transaction")
	}

	preBalance := tx.Meta.PreBalances[ownerIdx]
	postBalance := tx.Meta.PostBalances[ownerIdx]

	// Account for fees
	if postBalance > preBalance {
		return postBalance - preBalance, nil
	}
	return 0, nil
}

// BenchmarkPumpAmmBuy benchmarks buy transaction construction.
func BenchmarkPumpAmmBuy(b *testing.B) {
	rpcURL := os.Getenv("PUMP_TEST_RPC_URL")
	if rpcURL == "" {
		rpcURL = solanarpc.MainNetBeta_RPC
	}
	privateKeyStr := os.Getenv("PUMP_TEST_PRIVATE_KEY")
	if privateKeyStr == "" {
		b.Skip("PUMP_TEST_PRIVATE_KEY not set")
	}
	poolStr := os.Getenv("PUMP_TEST_POOL")
	if poolStr == "" {
		b.Skip("PUMP_TEST_POOL not set")
	}

	ctx := context.Background()
	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpcURL
	rpcClient := sdkrpc.NewClient(cfg)

	signer, _ := wallet.NewLocalFromBase58(privateKeyStr)
	pool := solana.MustPublicKeyFromBase58(poolStr)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _, err := autofill.PumpAmmBuyWithSol(
			ctx, rpcClient, signer, pool, 1_000_000, 500,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}
