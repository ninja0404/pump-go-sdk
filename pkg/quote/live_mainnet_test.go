package quote

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"

	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
)

func TestLiveMainnetCurrentQuoteState(t *testing.T) {
	rpcURL := os.Getenv("PUMP_SDK_LIVE_RPC")
	poolValue := os.Getenv("PUMP_SDK_LIVE_POOL")
	mintValue := os.Getenv("PUMP_SDK_LIVE_MINT")
	if rpcURL == "" || poolValue == "" || mintValue == "" {
		t.Skip("set PUMP_SDK_LIVE_RPC, PUMP_SDK_LIVE_POOL, and PUMP_SDK_LIVE_MINT to run")
	}

	pool, err := solana.PublicKeyFromBase58(poolValue)
	if err != nil {
		t.Fatalf("parse pool: %v", err)
	}
	mint, err := solana.PublicKeyFromBase58(mintValue)
	if err != nil {
		t.Fatalf("parse mint: %v", err)
	}
	config := sdkconfig.DefaultRPCConfig()
	config.RPCURL = rpcURL
	config.Timeout = 20 * time.Second
	rpc := sdkrpc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := PumpBuyQuote(ctx, rpc, mint, 1_000_000); err != nil {
		t.Fatalf("quote Pump buy: %v", err)
	}
	if _, err := PumpSellQuote(ctx, rpc, mint, 1_000_000); err != nil {
		t.Fatalf("quote Pump sell: %v", err)
	}
	if _, err := GetAmmPoolPrice(ctx, rpc, pool); err != nil {
		t.Fatalf("get PumpSwap price: %v", err)
	}
}
