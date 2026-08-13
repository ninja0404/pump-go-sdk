package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/token"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
	sdkquote "github.com/ninja0404/pump-go-sdk/pkg/quote"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

const (
	e2eAcknowledgement        = "I_UNDERSTAND_THIS_SENDS_TRANSACTIONS"
	mainnetAcknowledgement    = "I_UNDERSTAND_MAINNET_USES_REAL_FUNDS"
	devnetRPC                 = "https://api.devnet.solana.com"
	devnetPool                = "9sNparjr1Up6K9vSX6Ab7WEs6kXPPAfPeYnEFpuMqjmf"
	defaultTradeLamports      = uint64(1_000_000)
	maximumTradeLamports      = uint64(2_000_000)
	defaultUSDCQuoteAmount    = uint64(1_000_000)
	maximumUSDCQuoteAmount    = uint64(2_000_000)
	surfnetUSDCBalance        = uint64(100_000_000)
	maximumUSDCLoss           = uint64(5_000_000)
	defaultDevnetMaxLoss      = uint64(200_000_000)
	defaultMainnetMaxLoss     = uint64(50_000_000)
	devnetAirdropLamports     = uint64(1_000_000_000)
	transactionTestTimeout    = 5 * time.Minute
	transactionConfirmTimeout = 90 * time.Second
)

var mainnetUSDCMint = solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")

type e2eConfig struct {
	network         string
	rpcURL          string
	pool            solana.PublicKey
	mint            solana.PublicKey
	tradeLamports   uint64
	maxLossLamports uint64
	keypairPath     string
	createPumpMint  bool
}

type e2eRuntime struct {
	config  e2eConfig
	rpc     *sdkrpc.Client
	builder *txbuilder.Builder
	signer  wallet.Local
}

type surfnetUSDCConfig struct {
	e2eConfig
	quoteAmount uint64
}

func TestRealTransactionRoundTrips(t *testing.T) {
	config := loadE2EConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), transactionTestTimeout)
	defer cancel()

	runtime := newE2ERuntime(t, ctx, config)
	initialSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	if initialSOL < config.tradeLamports*3 {
		t.Fatalf("wallet balance %d is insufficient for three round trips", initialSOL)
	}

	mint := config.mint
	if config.createPumpMint {
		mint = createCashbackMint(t, ctx, runtime, "create_v2", false)
	}

	t.Run("legacy_pump", func(t *testing.T) {
		runLegacyPumpRoundTrip(t, ctx, runtime, mint)
	})
	t.Run("pump_v2", func(t *testing.T) {
		runPumpV2RoundTrip(t, ctx, runtime, mint)
	})
	t.Run("pumpswap", func(t *testing.T) {
		runPumpSwapRoundTrip(t, ctx, runtime, config.pool)
	})

	finalSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	if initialSOL > finalSOL && initialSOL-finalSOL > config.maxLossLamports {
		t.Fatalf("SOL loss %d exceeds configured maximum %d", initialSOL-finalSOL, config.maxLossLamports)
	}
	t.Logf("E2E_SOL_BALANCE initial=%d final=%d loss=%d", initialSOL, finalSOL, positiveDifference(initialSOL, finalSOL))
}

func TestSurfnetUSDCQuoteRoundTrips(t *testing.T) {
	config := loadSurfnetUSDCConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), transactionTestTimeout)
	defer cancel()

	runtime := newE2ERuntime(t, ctx, config.e2eConfig)
	initialSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	quoteAccount := setSurfnetTokenBalance(
		t,
		ctx,
		config.rpcURL,
		runtime,
		mainnetUSDCMint,
		surfnetUSDCBalance,
	)
	initialUSDC := getTokenBalance(t, ctx, runtime.rpc, quoteAccount, mainnetUSDCMint)

	mint := createCashbackMint(t, ctx, runtime, "create_v2_usdc", false, autofill.WithQuoteMint(mainnetUSDCMint))
	t.Run("pump_v2_usdc", func(t *testing.T) {
		runPumpV2TokenQuoteRoundTrip(t, ctx, runtime, mint, mainnetUSDCMint, config.quoteAmount)
	})
	t.Run("pumpswap_usdc", func(t *testing.T) {
		runPumpSwapTokenQuoteRoundTrip(t, ctx, runtime, config.pool, mainnetUSDCMint, config.quoteAmount)
	})

	finalUSDC := getTokenBalance(t, ctx, runtime.rpc, quoteAccount, mainnetUSDCMint)
	if loss := positiveDifference(initialUSDC, finalUSDC); loss > maximumUSDCLoss {
		t.Fatalf("USDC loss %d exceeds safety ceiling %d", loss, maximumUSDCLoss)
	}
	finalSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	if initialSOL > finalSOL && initialSOL-finalSOL > config.maxLossLamports {
		t.Fatalf("SOL loss %d exceeds configured maximum %d", initialSOL-finalSOL, config.maxLossLamports)
	}
	t.Logf(
		"E2E_USDC_BALANCE initial=%d final=%d loss=%d; SOL initial=%d final=%d loss=%d",
		initialUSDC,
		finalUSDC,
		positiveDifference(initialUSDC, finalUSDC),
		initialSOL,
		finalSOL,
		positiveDifference(initialSOL, finalSOL),
	)
}

func TestSurfnetMayhemRoundTrips(t *testing.T) {
	config := loadE2EConfig(t)
	if config.network != "surfnet" {
		t.Skip("Mayhem mint creation is only enabled for surfnet")
	}
	ctx, cancel := context.WithTimeout(context.Background(), transactionTestTimeout)
	defer cancel()

	runtime := newE2ERuntime(t, ctx, config)
	initialSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	if initialSOL < config.tradeLamports*3 {
		t.Fatalf("wallet balance %d is insufficient for Mayhem round trips", initialSOL)
	}
	mint := createCashbackMint(t, ctx, runtime, "create_v2_mayhem", true)
	requireMayhemPumpBondingCurve(t, ctx, runtime.rpc, mint)
	requireMayhemPumpSwapPool(t, ctx, runtime.rpc, config.pool)

	t.Run("legacy_pump_mayhem", func(t *testing.T) {
		runLegacyPumpRoundTrip(t, ctx, runtime, mint)
	})
	t.Run("pump_v2_mayhem", func(t *testing.T) {
		runPumpV2RoundTrip(t, ctx, runtime, mint)
	})
	t.Run("pumpswap_mayhem", func(t *testing.T) {
		runPumpSwapRoundTrip(t, ctx, runtime, config.pool)
	})

	finalSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	if initialSOL > finalSOL && initialSOL-finalSOL > config.maxLossLamports {
		t.Fatalf("SOL loss %d exceeds configured maximum %d", initialSOL-finalSOL, config.maxLossLamports)
	}
	t.Logf("E2E_MAYHEM_SOL_BALANCE initial=%d final=%d loss=%d", initialSOL, finalSOL, positiveDifference(initialSOL, finalSOL))
}

func loadE2EConfig(t *testing.T) e2eConfig {
	t.Helper()
	if os.Getenv("PUMP_E2E_ACK") != e2eAcknowledgement {
		t.Skip("set PUMP_E2E_ACK=I_UNDERSTAND_THIS_SENDS_TRANSACTIONS to run real transactions")
	}

	network := os.Getenv("PUMP_E2E_NETWORK")
	if network != "devnet" && network != "mainnet" && network != "surfnet" {
		t.Fatal("PUMP_E2E_NETWORK must be devnet, mainnet, or surfnet")
	}
	config := e2eConfig{
		network:         network,
		tradeLamports:   envUint64(t, "PUMP_E2E_TRADE_LAMPORTS", defaultTradeLamports),
		keypairPath:     os.Getenv("PUMP_E2E_KEYPAIR"),
		maxLossLamports: defaultDevnetMaxLoss,
	}
	if config.tradeLamports == 0 || config.tradeLamports > maximumTradeLamports {
		t.Fatalf("PUMP_E2E_TRADE_LAMPORTS must be between 1 and %d", maximumTradeLamports)
	}

	if network == "devnet" {
		config.rpcURL = envOrDefault("PUMP_E2E_RPC_URL", devnetRPC)
		config.pool = mustPublicKey(t, "PUMP_E2E_POOL", envOrDefault("PUMP_E2E_POOL", devnetPool))
		config.maxLossLamports = envUint64(t, "PUMP_E2E_MAX_LOSS_LAMPORTS", defaultDevnetMaxLoss)
		config.createPumpMint = os.Getenv("PUMP_E2E_MINT") == ""
		if !config.createPumpMint {
			config.mint = mustPublicKey(t, "PUMP_E2E_MINT", os.Getenv("PUMP_E2E_MINT"))
		}
		return config
	}
	if network == "surfnet" {
		if config.keypairPath == "" {
			t.Fatal("PUMP_E2E_KEYPAIR is required for surfnet")
		}
		config.rpcURL = os.Getenv("PUMP_E2E_RPC_URL")
		if !isLoopbackRPC(config.rpcURL) {
			t.Fatal("PUMP_E2E_RPC_URL must be an explicit loopback URL for surfnet")
		}
		config.pool = mustPublicKey(t, "PUMP_E2E_POOL", os.Getenv("PUMP_E2E_POOL"))
		config.maxLossLamports = envUint64(t, "PUMP_E2E_MAX_LOSS_LAMPORTS", defaultDevnetMaxLoss)
		config.createPumpMint = os.Getenv("PUMP_E2E_MINT") == ""
		if !config.createPumpMint {
			config.mint = mustPublicKey(t, "PUMP_E2E_MINT", os.Getenv("PUMP_E2E_MINT"))
		}
		return config
	}

	if os.Getenv("PUMP_E2E_MAINNET_ACK") != mainnetAcknowledgement {
		t.Fatal("set PUMP_E2E_MAINNET_ACK=I_UNDERSTAND_MAINNET_USES_REAL_FUNDS for mainnet")
	}
	if config.keypairPath == "" {
		t.Fatal("PUMP_E2E_KEYPAIR is required for mainnet")
	}
	config.rpcURL = os.Getenv("PUMP_E2E_RPC_URL")
	if config.rpcURL == "" {
		t.Fatal("PUMP_E2E_RPC_URL is required for mainnet")
	}
	config.pool = mustPublicKey(t, "PUMP_E2E_POOL", os.Getenv("PUMP_E2E_POOL"))
	config.mint = mustPublicKey(t, "PUMP_E2E_MINT", os.Getenv("PUMP_E2E_MINT"))
	config.maxLossLamports = envUint64(t, "PUMP_E2E_MAX_LOSS_LAMPORTS", defaultMainnetMaxLoss)
	return config
}

func loadSurfnetUSDCConfig(t *testing.T) surfnetUSDCConfig {
	t.Helper()
	if os.Getenv("PUMP_E2E_ACK") != e2eAcknowledgement {
		t.Skip("set PUMP_E2E_ACK=I_UNDERSTAND_THIS_SENDS_TRANSACTIONS to run real transactions")
	}
	poolValue := os.Getenv("PUMP_E2E_USDC_POOL")
	if poolValue == "" {
		t.Skip("set PUMP_E2E_USDC_POOL to run non-native quote transactions")
	}
	if os.Getenv("PUMP_E2E_NETWORK") != "surfnet" {
		t.Fatal("USDC state injection is only supported with PUMP_E2E_NETWORK=surfnet")
	}
	rpcURL := os.Getenv("PUMP_E2E_RPC_URL")
	if !isLoopbackRPC(rpcURL) {
		t.Fatal("PUMP_E2E_RPC_URL must be an explicit loopback URL for surfnet")
	}
	keypairPath := os.Getenv("PUMP_E2E_KEYPAIR")
	if keypairPath == "" {
		t.Fatal("PUMP_E2E_KEYPAIR is required for surfnet")
	}
	quoteAmount := envUint64(t, "PUMP_E2E_USDC_AMOUNT", defaultUSDCQuoteAmount)
	if quoteAmount == 0 || quoteAmount > maximumUSDCQuoteAmount {
		t.Fatalf("PUMP_E2E_USDC_AMOUNT must be between 1 and %d", maximumUSDCQuoteAmount)
	}
	return surfnetUSDCConfig{
		e2eConfig: e2eConfig{
			network:         "surfnet",
			rpcURL:          rpcURL,
			pool:            mustPublicKey(t, "PUMP_E2E_USDC_POOL", poolValue),
			maxLossLamports: defaultDevnetMaxLoss,
			keypairPath:     keypairPath,
		},
		quoteAmount: quoteAmount,
	}
}

func newE2ERuntime(t *testing.T, ctx context.Context, config e2eConfig) e2eRuntime {
	t.Helper()
	rpcConfig := sdkconfig.DefaultRPCConfig()
	rpcConfig.RPCURL = config.rpcURL
	rpcConfig.Commitment = string(solanarpc.CommitmentConfirmed)
	rpcConfig.Timeout = 30 * time.Second
	rpcConfig.Retry.Enabled = false
	rpc := sdkrpc.NewClient(rpcConfig)

	var signer wallet.Local
	if config.keypairPath != "" {
		loaded, err := wallet.NewLocalFromKeygen(config.keypairPath)
		if err != nil {
			t.Fatalf("load E2E keypair: %v", err)
		}
		signer = loaded
	} else {
		signer = wallet.NewLocalFromPrivateKey(solana.NewWallet().PrivateKey)
		airdropSignature, err := rpc.Raw().RequestAirdrop(
			ctx,
			signer.PublicKey(),
			devnetAirdropLamports,
			solanarpc.CommitmentConfirmed,
		)
		if err != nil {
			t.Fatalf("request devnet airdrop: %v", err)
		}
		waitCtx, cancel := context.WithTimeout(ctx, transactionConfirmTimeout)
		defer cancel()
		builder := txbuilder.NewBuilder(rpc, solanarpc.CommitmentConfirmed)
		if err := builder.WaitForConfirmation(waitCtx, airdropSignature, txbuilder.ConfirmationConfirmed); err != nil {
			t.Fatalf("confirm devnet airdrop %s: %v", airdropSignature, err)
		}
		t.Logf("E2E_TX airdrop=%s", airdropSignature)
	}

	return e2eRuntime{
		config:  config,
		rpc:     rpc,
		builder: txbuilder.NewBuilder(rpc, solanarpc.CommitmentConfirmed),
		signer:  signer,
	}
}

func createCashbackMint(
	t *testing.T,
	ctx context.Context,
	runtime e2eRuntime,
	label string,
	isMayhemMode bool,
	opts ...autofill.Option,
) solana.PublicKey {
	t.Helper()
	options := []autofill.Option{autofill.WithCashbackEnabled(true)}
	options = append(options, opts...)
	_, _, instruction, mintKey, err := autofill.PumpCreateV2(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		"pump-go-sdk e2e",
		"PGSE2E",
		"https://example.com/pump-go-sdk-e2e.json",
		isMayhemMode,
		options...,
	)
	if err != nil {
		t.Fatalf("build create_v2: %v", err)
	}
	mintSigner := wallet.NewLocalFromPrivateKey(mintKey)
	sendAndVerify(t, ctx, runtime, label, []wallet.Signer{mintSigner}, instruction)
	t.Logf("E2E_MINT %s=%s", label, mintKey.PublicKey())
	return mintKey.PublicKey()
}

func runLegacyPumpRoundTrip(t *testing.T, ctx context.Context, runtime e2eRuntime, mint solana.PublicKey) {
	t.Helper()
	expectedTokens, err := sdkquote.PumpBuyQuote(ctx, runtime.rpc, mint, runtime.config.tradeLamports)
	if err != nil {
		t.Fatalf("quote legacy Pump buy: %v", err)
	}
	accounts, _, buyInstructions, err := autofill.PumpBuyExactSolIn(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		mint,
		runtime.config.tradeLamports,
		1,
	)
	if err != nil {
		t.Fatalf("build legacy Pump buy: %v", err)
	}
	beforeTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedUser, mint)
	sendAndVerify(t, ctx, runtime, "legacy_buy", nil, buyInstructions...)
	afterBuyTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedUser, mint)
	if afterBuyTokens <= beforeTokens {
		t.Fatalf("legacy buy token balance did not increase: before=%d after=%d", beforeTokens, afterBuyTokens)
	}
	purchased := afterBuyTokens - beforeTokens
	if purchased != expectedTokens {
		t.Fatalf("legacy Pump buy output = %d, quote = %d", purchased, expectedTokens)
	}

	beforeSellSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	_, _, sellInstructions, err := autofill.PumpSellWithSlippage(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		mint,
		purchased,
		1_000,
	)
	if err != nil {
		t.Fatalf("build legacy Pump sell: %v", err)
	}
	sendAndVerify(t, ctx, runtime, "legacy_sell", nil, sellInstructions...)
	afterSellTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedUser, mint)
	if afterSellTokens != beforeTokens {
		t.Fatalf("legacy sell token balance = %d, want %d", afterSellTokens, beforeTokens)
	}
	afterSellSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	if afterSellSOL <= beforeSellSOL {
		t.Fatalf("legacy sell SOL balance did not increase: before=%d after=%d", beforeSellSOL, afterSellSOL)
	}
}

func runPumpV2RoundTrip(t *testing.T, ctx context.Context, runtime e2eRuntime, mint solana.PublicKey) {
	t.Helper()
	expectedTokens, err := sdkquote.PumpBuyQuote(ctx, runtime.rpc, mint, runtime.config.tradeLamports)
	if err != nil {
		t.Fatalf("quote Pump V2 buy: %v", err)
	}
	accounts, _, buyInstructions, err := autofill.PumpBuyExactQuoteInV2(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		mint,
		runtime.config.tradeLamports,
		1,
	)
	if err != nil {
		t.Fatalf("build Pump V2 buy: %v", err)
	}
	beforeTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedBaseUser, mint)
	sendAndVerify(t, ctx, runtime, "pump_v2_buy", nil, buyInstructions...)
	afterBuyTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedBaseUser, mint)
	if afterBuyTokens <= beforeTokens {
		t.Fatalf("Pump V2 buy token balance did not increase: before=%d after=%d", beforeTokens, afterBuyTokens)
	}
	purchased := afterBuyTokens - beforeTokens
	if purchased != expectedTokens {
		t.Fatalf("Pump V2 buy output = %d, quote = %d", purchased, expectedTokens)
	}

	beforeSellSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	_, _, sellInstructions, err := autofill.PumpSellV2(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		mint,
		purchased,
		1,
	)
	if err != nil {
		t.Fatalf("build Pump V2 sell: %v", err)
	}
	sendAndVerify(t, ctx, runtime, "pump_v2_sell", nil, sellInstructions...)
	afterSellTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedBaseUser, mint)
	if afterSellTokens != beforeTokens {
		t.Fatalf("Pump V2 sell token balance = %d, want %d", afterSellTokens, beforeTokens)
	}
	afterSellSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	if afterSellSOL <= beforeSellSOL {
		t.Fatalf("Pump V2 sell SOL balance did not increase: before=%d after=%d", beforeSellSOL, afterSellSOL)
	}
}

func runPumpSwapRoundTrip(t *testing.T, ctx context.Context, runtime e2eRuntime, pool solana.PublicKey) {
	t.Helper()
	requireCashbackPumpSwapPool(t, ctx, runtime.rpc, pool)
	accounts, _, buyInstructions, simulatedBaseOut, err := autofill.PumpAmmBuyWithSol(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		pool,
		runtime.config.tradeLamports,
		1_000,
	)
	if err != nil {
		t.Fatalf("build PumpSwap buy: %v", err)
	}
	if simulatedBaseOut == 0 {
		t.Fatal("PumpSwap buy simulation returned zero base output")
	}
	if accounts.BaseTokenProgram != constants.Token2022ProgramID {
		t.Fatalf("PumpSwap base token program = %s, want Token-2022", accounts.BaseTokenProgram)
	}
	beforeTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.UserBaseTokenAccount, accounts.BaseMint)
	sendAndVerify(t, ctx, runtime, "pumpswap_buy", nil, buyInstructions...)
	afterBuyTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.UserBaseTokenAccount, accounts.BaseMint)
	if afterBuyTokens <= beforeTokens {
		t.Fatalf("PumpSwap buy token balance did not increase: before=%d after=%d", beforeTokens, afterBuyTokens)
	}
	assertAccountMissing(t, ctx, runtime.rpc, accounts.UserQuoteTokenAccount)
	purchased := afterBuyTokens - beforeTokens
	if purchased != simulatedBaseOut {
		t.Fatalf("PumpSwap buy output = %d, simulation = %d", purchased, simulatedBaseOut)
	}

	beforeSellSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	_, _, sellInstructions, err := autofill.PumpAmmSellWithSlippage(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		pool,
		purchased,
		1_000,
	)
	if err != nil {
		t.Fatalf("build PumpSwap sell: %v", err)
	}
	sendAndVerify(t, ctx, runtime, "pumpswap_sell", nil, sellInstructions...)
	afterSellTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.UserBaseTokenAccount, accounts.BaseMint)
	if afterSellTokens != beforeTokens {
		t.Fatalf("PumpSwap sell token balance = %d, want %d", afterSellTokens, beforeTokens)
	}
	afterSellSOL := getSOLBalance(t, ctx, runtime.rpc, runtime.signer.PublicKey())
	if afterSellSOL <= beforeSellSOL {
		t.Fatalf("PumpSwap sell SOL balance did not increase: before=%d after=%d", beforeSellSOL, afterSellSOL)
	}
}

func runPumpV2TokenQuoteRoundTrip(
	t *testing.T,
	ctx context.Context,
	runtime e2eRuntime,
	mint, quoteMint solana.PublicKey,
	quoteAmount uint64,
) {
	t.Helper()
	expectedTokens, err := sdkquote.PumpBuyQuote(ctx, runtime.rpc, mint, quoteAmount)
	if err != nil {
		t.Fatalf("quote Pump V2 token-quote buy: %v", err)
	}
	accounts, _, buyInstructions, err := autofill.PumpBuyExactQuoteInV2(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		mint,
		quoteAmount,
		1,
	)
	if err != nil {
		t.Fatalf("build Pump V2 token-quote buy: %v", err)
	}
	if accounts.QuoteMint != quoteMint || accounts.QuoteTokenProgram != constants.TokenProgramID {
		t.Fatalf("Pump V2 quote accounts = (%s, %s), want (%s, %s)", accounts.QuoteMint, accounts.QuoteTokenProgram, quoteMint, constants.TokenProgramID)
	}
	beforeTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedBaseUser, mint)
	beforeQuote := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedQuoteUser, quoteMint)
	sendAndVerify(t, ctx, runtime, "pump_v2_usdc_buy", nil, buyInstructions...)
	afterBuyTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedBaseUser, mint)
	afterBuyQuote := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedQuoteUser, quoteMint)
	if afterBuyTokens <= beforeTokens {
		t.Fatalf("Pump V2 token-quote buy did not increase base balance: before=%d after=%d", beforeTokens, afterBuyTokens)
	}
	if afterBuyQuote >= beforeQuote || beforeQuote-afterBuyQuote > quoteAmount {
		t.Fatalf("Pump V2 token-quote buy quote delta invalid: before=%d after=%d max=%d", beforeQuote, afterBuyQuote, quoteAmount)
	}
	purchased := afterBuyTokens - beforeTokens
	if purchased != expectedTokens {
		t.Fatalf("Pump V2 token-quote buy output = %d, quote = %d", purchased, expectedTokens)
	}

	expectedQuoteOut, err := sdkquote.PumpSellQuote(ctx, runtime.rpc, mint, purchased)
	if err != nil {
		t.Fatalf("quote Pump V2 token-quote sell: %v", err)
	}
	_, _, sellInstructions, err := autofill.PumpSellV2(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		mint,
		purchased,
		1,
	)
	if err != nil {
		t.Fatalf("build Pump V2 token-quote sell: %v", err)
	}
	beforeSellQuote := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedQuoteUser, quoteMint)
	sendAndVerify(t, ctx, runtime, "pump_v2_usdc_sell", nil, sellInstructions...)
	afterSellTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedBaseUser, mint)
	if afterSellTokens != beforeTokens {
		t.Fatalf("Pump V2 token-quote sell base balance = %d, want %d", afterSellTokens, beforeTokens)
	}
	afterSellQuote := getTokenBalance(t, ctx, runtime.rpc, accounts.AssociatedQuoteUser, quoteMint)
	if afterSellQuote <= beforeSellQuote {
		t.Fatalf("Pump V2 token-quote sell did not increase quote balance: before=%d after=%d", beforeSellQuote, afterSellQuote)
	}
	if actualQuoteOut := afterSellQuote - beforeSellQuote; actualQuoteOut != expectedQuoteOut {
		t.Fatalf("Pump V2 token-quote sell output = %d, quote = %d", actualQuoteOut, expectedQuoteOut)
	}
}

func runPumpSwapTokenQuoteRoundTrip(
	t *testing.T,
	ctx context.Context,
	runtime e2eRuntime,
	pool, quoteMint solana.PublicKey,
	quoteAmount uint64,
) {
	t.Helper()
	requireCashbackPumpSwapPool(t, ctx, runtime.rpc, pool)
	accounts, _, buyInstructions, err := autofill.PumpAmmBuyExactQuoteIn(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		pool,
		quoteAmount,
		1,
	)
	if err != nil {
		t.Fatalf("build PumpSwap token-quote buy: %v", err)
	}
	if accounts.BaseTokenProgram != constants.Token2022ProgramID {
		t.Fatalf("PumpSwap base token program = %s, want Token-2022", accounts.BaseTokenProgram)
	}
	if accounts.QuoteMint != quoteMint || accounts.QuoteTokenProgram != constants.TokenProgramID {
		t.Fatalf("PumpSwap quote accounts = (%s, %s), want (%s, %s)", accounts.QuoteMint, accounts.QuoteTokenProgram, quoteMint, constants.TokenProgramID)
	}
	beforeTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.UserBaseTokenAccount, accounts.BaseMint)
	beforeQuote := getTokenBalance(t, ctx, runtime.rpc, accounts.UserQuoteTokenAccount, quoteMint)
	sendAndVerify(t, ctx, runtime, "pumpswap_usdc_buy", nil, buyInstructions...)
	afterBuyTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.UserBaseTokenAccount, accounts.BaseMint)
	afterBuyQuote := getTokenBalance(t, ctx, runtime.rpc, accounts.UserQuoteTokenAccount, quoteMint)
	if afterBuyTokens <= beforeTokens {
		t.Fatalf("PumpSwap token-quote buy did not increase base balance: before=%d after=%d", beforeTokens, afterBuyTokens)
	}
	if afterBuyQuote >= beforeQuote || beforeQuote-afterBuyQuote > quoteAmount {
		t.Fatalf("PumpSwap token-quote buy quote delta invalid: before=%d after=%d max=%d", beforeQuote, afterBuyQuote, quoteAmount)
	}
	purchased := afterBuyTokens - beforeTokens

	expectedSell, err := sdkquote.AmmSellQuote(ctx, runtime.rpc, runtime.signer, pool, purchased)
	if err != nil {
		t.Fatalf("quote PumpSwap token-quote sell: %v", err)
	}
	_, _, sellInstructions, err := autofill.PumpAmmSellWithSlippage(
		ctx,
		runtime.rpc,
		runtime.signer.PublicKey(),
		pool,
		purchased,
		1_000,
	)
	if err != nil {
		t.Fatalf("build PumpSwap token-quote sell: %v", err)
	}
	beforeSellQuote := getTokenBalance(t, ctx, runtime.rpc, accounts.UserQuoteTokenAccount, quoteMint)
	sendAndVerify(t, ctx, runtime, "pumpswap_usdc_sell", nil, sellInstructions...)
	afterSellTokens := getTokenBalance(t, ctx, runtime.rpc, accounts.UserBaseTokenAccount, accounts.BaseMint)
	if afterSellTokens != beforeTokens {
		t.Fatalf("PumpSwap token-quote sell base balance = %d, want %d", afterSellTokens, beforeTokens)
	}
	afterSellQuote := getTokenBalance(t, ctx, runtime.rpc, accounts.UserQuoteTokenAccount, quoteMint)
	if afterSellQuote <= beforeSellQuote {
		t.Fatalf("PumpSwap token-quote sell did not increase quote balance: before=%d after=%d", beforeSellQuote, afterSellQuote)
	}
	if actualQuoteOut := afterSellQuote - beforeSellQuote; actualQuoteOut != expectedSell.ExpectedOut {
		t.Fatalf("PumpSwap token-quote sell output = %d, quote = %d", actualQuoteOut, expectedSell.ExpectedOut)
	}
}

func setSurfnetTokenBalance(
	t *testing.T,
	ctx context.Context,
	rpcURL string,
	runtime e2eRuntime,
	mint solana.PublicKey,
	amount uint64,
) solana.PublicKey {
	t.Helper()
	if runtime.config.network != "surfnet" || !isLoopbackRPC(rpcURL) {
		t.Fatal("surfnet token balance injection requires an explicit loopback surfnet")
	}
	payload, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  []any  `json:"params"`
	}{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "surfnet_setTokenAccount",
		Params: []any{
			runtime.signer.PublicKey().String(),
			mint.String(),
			map[string]uint64{"amount": amount},
		},
	})
	if err != nil {
		t.Fatalf("encode surfnet token balance request: %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build surfnet token balance request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("set surfnet token balance: %v", err)
	}
	defer response.Body.Close()
	var result struct {
		Error *struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode surfnet token balance response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("surfnet token balance HTTP status = %d", response.StatusCode)
	}
	if result.Error != nil {
		t.Fatalf("surfnet token balance RPC error %d: %s (%s)", result.Error.Code, result.Error.Message, result.Error.Data)
	}
	owner := runtime.signer.PublicKey()
	account, _, err := solana.FindProgramAddress(
		[][]byte{owner[:], constants.TokenProgramID[:], mint[:]},
		constants.AssociatedTokenProgramID,
	)
	if err != nil {
		t.Fatalf("derive surfnet token account: %v", err)
	}
	if balance := getTokenBalance(t, ctx, runtime.rpc, account, mint); balance != amount {
		t.Fatalf("surfnet token balance = %d, want %d", balance, amount)
	}
	return account
}

func requireCashbackPumpSwapPool(t *testing.T, ctx context.Context, rpc *sdkrpc.Client, pool solana.PublicKey) {
	t.Helper()
	state := fetchPumpSwapPool(t, ctx, rpc, pool)
	if !state.IsCashbackCoin {
		t.Fatalf("PumpSwap pool %s must be a cashback pool", pool)
	}
}

func requireMayhemPumpSwapPool(t *testing.T, ctx context.Context, rpc *sdkrpc.Client, pool solana.PublicKey) {
	t.Helper()
	state := fetchPumpSwapPool(t, ctx, rpc, pool)
	if !state.IsMayhemMode || !state.IsCashbackCoin {
		t.Fatalf("PumpSwap pool %s modes = (mayhem=%t, cashback=%t), want both true", pool, state.IsMayhemMode, state.IsCashbackCoin)
	}
}

func fetchPumpSwapPool(t *testing.T, ctx context.Context, rpc *sdkrpc.Client, pool solana.PublicKey) pumpamm.Pool {
	t.Helper()
	account, err := rpc.Raw().GetAccountInfo(ctx, pool)
	if err != nil {
		t.Fatalf("fetch PumpSwap pool %s: %v", pool, err)
	}
	if account == nil || account.Value == nil || account.Value.Data == nil {
		t.Fatalf("PumpSwap pool %s is missing", pool)
	}
	if account.Value.Owner != pumpamm.ProgramKey {
		t.Fatalf("PumpSwap pool %s owner = %s, want %s", pool, account.Value.Owner, pumpamm.ProgramKey)
	}
	var state pumpamm.Pool
	if err := state.Unmarshal(account.Value.Data.GetBinary()); err != nil {
		t.Fatalf("decode PumpSwap pool %s: %v", pool, err)
	}
	return state
}

func requireMayhemPumpBondingCurve(t *testing.T, ctx context.Context, rpc *sdkrpc.Client, mint solana.PublicKey) {
	t.Helper()
	bondingCurve, _, err := solana.FindProgramAddress(
		[][]byte{[]byte(constants.SeedBondingCurve), mint[:]},
		pump.ProgramKey,
	)
	if err != nil {
		t.Fatalf("derive Mayhem bonding curve: %v", err)
	}
	account, err := rpc.Raw().GetAccountInfo(ctx, bondingCurve)
	if err != nil {
		t.Fatalf("fetch Mayhem bonding curve %s: %v", bondingCurve, err)
	}
	if account == nil || account.Value == nil || account.Value.Data == nil {
		t.Fatalf("Mayhem bonding curve %s is missing", bondingCurve)
	}
	if account.Value.Owner != pump.ProgramKey {
		t.Fatalf("Mayhem bonding curve %s owner = %s, want %s", bondingCurve, account.Value.Owner, pump.ProgramKey)
	}
	var state pump.BondingCurve
	if err := state.Unmarshal(account.Value.Data.GetBinary()); err != nil {
		t.Fatalf("decode Mayhem bonding curve %s: %v", bondingCurve, err)
	}
	if !state.IsMayhemMode || !state.IsCashbackCoin {
		t.Fatalf("bonding curve %s modes = (mayhem=%t, cashback=%t), want both true", bondingCurve, state.IsMayhemMode, state.IsCashbackCoin)
	}
}

func sendAndVerify(
	t *testing.T,
	ctx context.Context,
	runtime e2eRuntime,
	label string,
	extraSigners []wallet.Signer,
	instructions ...solana.Instruction,
) solana.Signature {
	t.Helper()
	sendCtx, cancel := context.WithTimeout(ctx, transactionConfirmTimeout)
	defer cancel()
	signature, err := runtime.builder.BuildSignSendAndConfirm(
		sendCtx,
		runtime.signer,
		extraSigners,
		txbuilder.ConfirmationConfirmed,
		instructions...,
	)
	if err != nil {
		t.Fatalf("send %s: %v", label, err)
	}
	version := uint64(0)
	transaction, err := runtime.rpc.Raw().GetTransaction(sendCtx, signature, &solanarpc.GetTransactionOpts{
		Commitment:                     solanarpc.CommitmentConfirmed,
		MaxSupportedTransactionVersion: &version,
	})
	if err != nil {
		t.Fatalf("fetch confirmed %s transaction %s: %v", label, signature, err)
	}
	if transaction == nil || transaction.Meta == nil {
		t.Fatalf("confirmed %s transaction %s has no receipt", label, signature)
	}
	if transaction.Meta.Err != nil {
		t.Fatalf("confirmed %s transaction %s failed: %v", label, signature, transaction.Meta.Err)
	}
	t.Logf("E2E_TX %s=%s", label, signature)
	return signature
}

func getTokenBalance(t *testing.T, ctx context.Context, rpc *sdkrpc.Client, address, mint solana.PublicKey) uint64 {
	t.Helper()
	account, err := rpc.Raw().GetAccountInfo(ctx, address)
	if errors.Is(err, solanarpc.ErrNotFound) {
		return 0
	}
	if err != nil {
		t.Fatalf("fetch token account %s: %v", address, err)
	}
	if account == nil || account.Value == nil || account.Value.Data == nil {
		return 0
	}
	if account.Value.Owner != constants.TokenProgramID && account.Value.Owner != constants.Token2022ProgramID {
		t.Fatalf("token account %s has unsupported owner %s", address, account.Value.Owner)
	}
	var state token.Account
	if err := bin.NewBinDecoder(account.Value.Data.GetBinary()).Decode(&state); err != nil {
		t.Fatalf("decode token account %s: %v", address, err)
	}
	if state.Mint != mint {
		t.Fatalf("token account %s mint = %s, want %s", address, state.Mint, mint)
	}
	return state.Amount
}

func getSOLBalance(t *testing.T, ctx context.Context, rpc *sdkrpc.Client, address solana.PublicKey) uint64 {
	t.Helper()
	balance, err := rpc.Raw().GetBalance(ctx, address, solanarpc.CommitmentConfirmed)
	if err != nil {
		t.Fatalf("fetch SOL balance %s: %v", address, err)
	}
	if balance == nil {
		t.Fatalf("SOL balance %s is missing", address)
	}
	return balance.Value
}

func assertAccountMissing(t *testing.T, ctx context.Context, rpc *sdkrpc.Client, address solana.PublicKey) {
	t.Helper()
	account, err := rpc.Raw().GetAccountInfo(ctx, address)
	if errors.Is(err, solanarpc.ErrNotFound) || (err == nil && (account == nil || account.Value == nil)) {
		return
	}
	if err != nil {
		t.Fatalf("fetch account %s: %v", address, err)
	}
	t.Fatalf("account %s still exists", address)
}

func envUint64(t *testing.T, name string, fallback uint64) uint64 {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return parsed
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func mustPublicKey(t *testing.T, name, value string) solana.PublicKey {
	t.Helper()
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	publicKey, err := solana.PublicKeyFromBase58(value)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return publicKey
}

func positiveDifference(before, after uint64) uint64 {
	if before > after {
		return before - after
	}
	return 0
}

func isLoopbackRPC(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func TestE2ESafetyConstants(t *testing.T) {
	if maximumTradeLamports > 2_000_000 {
		t.Fatalf("maximum trade amount %d exceeds the safety ceiling", maximumTradeLamports)
	}
	if e2eAcknowledgement == "" || mainnetAcknowledgement == "" {
		t.Fatal("E2E acknowledgements must not be empty")
	}
	if devnetPool == "" {
		t.Fatal("devnet pool must be configured")
	}
	if defaultTradeLamports == 0 {
		t.Fatal("default trade amount must be non-zero")
	}
	if maximumUSDCQuoteAmount > 2_000_000 || surfnetUSDCBalance < maximumUSDCQuoteAmount {
		t.Fatal("USDC E2E safety constants are invalid")
	}
	for _, test := range []struct {
		url  string
		want bool
	}{
		{url: "http://127.0.0.1:8899", want: true},
		{url: "http://[::1]:8899", want: true},
		{url: "http://localhost:8899", want: true},
		{url: "https://api.mainnet-beta.solana.com", want: false},
		{url: "127.0.0.1:8899", want: false},
	} {
		if got := isLoopbackRPC(test.url); got != test.want {
			t.Errorf("isLoopbackRPC(%q) = %t, want %t", test.url, got, test.want)
		}
	}
}
