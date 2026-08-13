package autofill

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"

	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
)

func TestLiveMainnetCurrentAccountLayouts(t *testing.T) {
	rpcURL := os.Getenv("PUMP_SDK_LIVE_RPC")
	poolValue := os.Getenv("PUMP_SDK_LIVE_POOL")
	mintValue := os.Getenv("PUMP_SDK_LIVE_MINT")
	userValue := os.Getenv("PUMP_SDK_LIVE_USER")
	if rpcURL == "" || poolValue == "" || mintValue == "" || userValue == "" {
		t.Skip("set PUMP_SDK_LIVE_RPC, PUMP_SDK_LIVE_POOL, PUMP_SDK_LIVE_MINT, and PUMP_SDK_LIVE_USER to run")
	}

	pool, err := solana.PublicKeyFromBase58(poolValue)
	if err != nil {
		t.Fatalf("parse pool: %v", err)
	}
	mint, err := solana.PublicKeyFromBase58(mintValue)
	if err != nil {
		t.Fatalf("parse mint: %v", err)
	}
	user, err := solana.PublicKeyFromBase58(userValue)
	if err != nil {
		t.Fatalf("parse user: %v", err)
	}
	config := sdkconfig.DefaultRPCConfig()
	config.RPCURL = rpcURL
	config.Timeout = 20 * time.Second
	rpc := sdkrpc.NewClient(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	global, err := derivePDA(pump.ProgramKey, []byte("global"))
	if err != nil {
		t.Fatalf("derive pump global: %v", err)
	}
	globalAccount, err := rpc.Raw().GetAccountInfo(ctx, global)
	if err != nil {
		t.Fatalf("fetch pump global: %v", err)
	}
	if globalAccount == nil || globalAccount.Value == nil || globalAccount.Value.Data == nil {
		t.Fatal("pump global account is missing")
	}
	var globalState pump.Global
	if err := globalState.Unmarshal(globalAccount.Value.Data.GetBinary()); err != nil {
		t.Fatalf("decode pump global: %v", err)
	}
	if _, err := randomFeeRecipient("buyback fee recipient", globalState.BuybackFeeRecipients[:]); err != nil {
		t.Fatalf("pump global buyback fee recipients: %v", err)
	}

	buyAccounts, err := pumpAmmAutofillBuy(ctx, rpc, user, pool)
	if err != nil {
		t.Fatalf("autofill PumpSwap buy: %v", err)
	}
	if len(buyAccounts.RemainingAccounts) < 2 {
		t.Fatalf("PumpSwap remaining account count = %d, want at least 2", len(buyAccounts.RemainingAccounts))
	}

	legacyBuyAccounts, err := pumpAutofillBuy(ctx, rpc, user, mint)
	if err != nil {
		t.Fatalf("autofill legacy Pump buy: %v", err)
	}
	if len(legacyBuyAccounts.RemainingAccounts) != 2 {
		t.Fatalf("legacy Pump remaining account count = %d, want 2", len(legacyBuyAccounts.RemainingAccounts))
	}

	v2Accounts, err := fetchPumpV2AccountSet(ctx, rpc, user, mint)
	if err != nil {
		t.Fatalf("autofill Pump V2 buy: %v", err)
	}
	if v2Accounts.QuoteMint.IsZero() || v2Accounts.BaseTokenProgram.IsZero() || v2Accounts.QuoteTokenProgram.IsZero() {
		t.Fatal("Pump V2 account set contains an unresolved mint or token program")
	}

	_, _, legacyInstructions, err := PumpBuy(ctx, rpc, user, mint, 1, 1)
	if err != nil {
		t.Fatalf("build live legacy Pump buy: %v", err)
	}
	assertLiveInstructionAccountCount(t, legacyInstructions, pump.ProgramKey, 18)

	_, _, v2Instructions, err := PumpBuyV2(ctx, rpc, user, mint, 1, 1)
	if err != nil {
		t.Fatalf("build live Pump V2 buy: %v", err)
	}
	assertLiveInstructionAccountCount(t, v2Instructions, pump.ProgramKey, 27)

	ammAccounts, _, ammInstructions, err := PumpAmmBuyExactQuoteIn(ctx, rpc, user, pool, 1, 1)
	if err != nil {
		t.Fatalf("build live PumpSwap buy: %v", err)
	}
	assertLiveInstructionAccountCount(t, ammInstructions, pumpamm.ProgramKey, 23+len(ammAccounts.RemainingAccounts))
}

func assertLiveInstructionAccountCount(t *testing.T, instructions []solana.Instruction, program solana.PublicKey, want int) {
	t.Helper()
	for _, instruction := range instructions {
		if instruction.ProgramID() == program {
			if got := len(instruction.Accounts()); got != want {
				t.Fatalf("program %s account count = %d, want %d", program, got, want)
			}
			return
		}
	}
	t.Fatalf("instruction for program %s not found", program)
}
