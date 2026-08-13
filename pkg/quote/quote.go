// Package quote provides price calculation utilities for Pump and Pump AMM.
//
// This package offers simulation-based quoting functions to estimate trade outcomes
// before executing actual transactions. All quotes are non-binding estimates that
// may differ from actual execution due to price movement.
//
// Example usage:
//
//	// Get buy quote for Pump AMM
//	quote, err := quote.AmmBuyQuote(ctx, rpc, signer, pool, 10_000_000) // 0.01 SOL
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Expected tokens: %d\n", quote.ExpectedOut)
//	fmt.Printf("Price impact: %.2f%%\n", quote.PriceImpactBps/100.0)
package quote

import (
	"context"
	"fmt"
	"math/big"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/token"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/types"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// QuoteResult contains the result of a price quote.
type QuoteResult struct {
	// ExpectedOut is the estimated output amount (tokens for buy, SOL for sell).
	ExpectedOut uint64

	// MinOut is the minimum output with slippage applied.
	MinOut uint64

	// PriceImpactBps is the estimated price impact in basis points.
	// Calculated as: (spotPrice - executionPrice) / spotPrice * 10000
	PriceImpactBps uint64

	// SpotPrice is the current pool price (quote per base, scaled by 1e9).
	SpotPrice uint64

	// ExecutionPrice is the effective price after this trade (quote per base, scaled by 1e9).
	ExecutionPrice uint64
}

// AmmBuyQuote estimates the token output for a given SOL input on Pump AMM.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - signer: wallet signer (required for simulation)
//   - pool: AMM pool address
//   - quoteLamports: SOL amount to spend (lamports)
//   - slippageBps: optional slippage for MinOut calculation (default 0)
//
// Returns QuoteResult with expected output and price metrics.
func AmmBuyQuote(ctx context.Context, rpc *sdkrpc.Client, signer wallet.Signer, pool solana.PublicKey, quoteLamports uint64, slippageBps ...uint64) (*QuoteResult, error) {
	if rpc == nil {
		return nil, types.ErrNilRPC
	}
	if signer == nil {
		return nil, types.ErrNilSigner
	}
	if quoteLamports == 0 {
		return nil, types.NewValidationError("quoteLamports", "must be greater than 0")
	}

	slip := uint64(0)
	if len(slippageBps) > 0 {
		slip = slippageBps[0]
	}

	// Use PumpAmmBuyWithSol to get simulated output
	_, _, _, simOut, err := autofill.PumpAmmBuyWithSol(ctx, rpc, signer.PublicKey(), pool, quoteLamports, slip)
	if err != nil {
		return nil, fmt.Errorf("simulate buy: %w", err)
	}

	// Calculate min output with slippage
	minOut := applySlippage(simOut, slip)

	// Get pool state for price calculation
	poolState, err := fetchPoolState(ctx, rpc, pool)
	if err != nil {
		return nil, fmt.Errorf("fetch pool state: %w", err)
	}

	spotPrice, execPrice, impact := calculatePriceMetrics(poolState, quoteLamports, simOut, true)

	return &QuoteResult{
		ExpectedOut:    simOut,
		MinOut:         minOut,
		PriceImpactBps: impact,
		SpotPrice:      spotPrice,
		ExecutionPrice: execPrice,
	}, nil
}

// AmmSellQuote estimates the SOL output for a given token input on Pump AMM.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - signer: wallet signer (required for simulation)
//   - pool: AMM pool address
//   - baseAmount: token amount to sell (base units)
//   - slippageBps: optional slippage for MinOut calculation (default 0)
//
// Returns QuoteResult with expected SOL output and price metrics.
func AmmSellQuote(ctx context.Context, rpc *sdkrpc.Client, signer wallet.Signer, pool solana.PublicKey, baseAmount uint64, slippageBps ...uint64) (*QuoteResult, error) {
	if rpc == nil {
		return nil, types.ErrNilRPC
	}
	if signer == nil {
		return nil, types.ErrNilSigner
	}
	if baseAmount == 0 {
		return nil, types.NewValidationError("baseAmount", "must be greater than 0")
	}

	slip := uint64(0)
	if len(slippageBps) > 0 {
		slip = slippageBps[0]
	}

	// Simulate sell to get quote output
	accts, _, ix, err := autofill.PumpAmmSell(ctx, rpc, signer.PublicKey(), pool, baseAmount, 0)
	if err != nil {
		return nil, fmt.Errorf("build sell: %w", err)
	}

	quoteOut, err := simulateQuoteOut(ctx, rpc, signer, accts.UserQuoteTokenAccount, ix)
	if err != nil {
		return nil, fmt.Errorf("simulate sell: %w", err)
	}

	minOut := applySlippage(quoteOut, slip)

	// Get pool state for price calculation
	poolState, err := fetchPoolState(ctx, rpc, pool)
	if err != nil {
		return nil, fmt.Errorf("fetch pool state: %w", err)
	}

	spotPrice, execPrice, impact := calculatePriceMetrics(poolState, quoteOut, baseAmount, false)

	return &QuoteResult{
		ExpectedOut:    quoteOut,
		MinOut:         minOut,
		PriceImpactBps: impact,
		SpotPrice:      spotPrice,
		ExecutionPrice: execPrice,
	}, nil
}

// PumpBuyQuote estimates the token output for a given quote-token input on a Pump bonding curve.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - mint: token mint address
//   - quoteAmount: quote-token amount to spend (lamports for native SOL, otherwise raw token units)
//
// Returns estimated token output amount.
func PumpBuyQuote(ctx context.Context, rpc *sdkrpc.Client, mint solana.PublicKey, quoteAmount uint64) (uint64, error) {
	if rpc == nil {
		return 0, types.ErrNilRPC
	}
	if quoteAmount == 0 {
		return 0, types.NewValidationError("quoteAmount", "must be greater than 0")
	}

	state, err := fetchPumpQuoteState(ctx, rpc, mint)
	if err != nil {
		return 0, err
	}
	protocolFeeBps, creatorFeeBps, err := state.feeBasisPoints(state.MintSupply)
	if err != nil {
		return 0, err
	}
	if state.BondingCurve.Creator.IsZero() {
		creatorFeeBps = 0
	}
	totalFeeBps, err := addBasisPoints(protocolFeeBps, creatorFeeBps)
	if err != nil {
		return 0, err
	}
	return calculatePumpBuyAmount(
		quoteAmount,
		state.BondingCurve.VirtualTokenReserves,
		state.BondingCurve.VirtualQuoteReserves,
		state.BondingCurve.RealTokenReserves,
		totalFeeBps,
	), nil
}

// PumpSellQuote estimates the quote-token output for a given token input on a Pump bonding curve.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - mint: token mint address
//   - tokenAmount: token amount to sell (base units)
//
// Returns raw quote-token output units (lamports for native SOL).
func PumpSellQuote(ctx context.Context, rpc *sdkrpc.Client, mint solana.PublicKey, tokenAmount uint64) (uint64, error) {
	if rpc == nil {
		return 0, types.ErrNilRPC
	}
	if tokenAmount == 0 {
		return 0, types.NewValidationError("tokenAmount", "must be greater than 0")
	}

	state, err := fetchPumpQuoteState(ctx, rpc, mint)
	if err != nil {
		return 0, err
	}
	mintSupply := uint64(1_000_000_000_000_000)
	if state.BondingCurve.IsMayhemMode {
		mintSupply = state.MintSupply
	}
	protocolFeeBps, creatorFeeBps, err := state.feeBasisPoints(mintSupply)
	if err != nil {
		return 0, err
	}
	if state.BondingCurve.Creator.IsZero() {
		creatorFeeBps = 0
	}
	return calculatePumpSellAmount(
		tokenAmount,
		state.BondingCurve.VirtualTokenReserves,
		state.BondingCurve.VirtualQuoteReserves,
		protocolFeeBps,
		creatorFeeBps,
	)
}

// GetAmmPoolPrice returns the current spot price of an AMM pool.
//
// Returns price as quote per base, scaled by 1e9 (e.g., 1000000000 = 1 SOL per token).
func GetAmmPoolPrice(ctx context.Context, rpc *sdkrpc.Client, pool solana.PublicKey) (uint64, error) {
	if rpc == nil {
		return 0, types.ErrNilRPC
	}

	poolState, err := fetchPoolState(ctx, rpc, pool)
	if err != nil {
		return 0, err
	}

	// price = quote_reserves / base_reserves (scaled by 1e9)
	if poolState.BaseReserves == 0 {
		return 0, fmt.Errorf("pool has zero base reserves")
	}

	price := new(big.Int).SetUint64(poolState.QuoteReserves)
	price.Mul(price, big.NewInt(1e9))
	price.Div(price, new(big.Int).SetUint64(poolState.BaseReserves))

	return price.Uint64(), nil
}

// GetPumpPrice returns the current spot price of a Pump bonding curve.
//
// Returns price as quote-token units per base-token unit, scaled by 1e9.
func GetPumpPrice(ctx context.Context, rpc *sdkrpc.Client, mint solana.PublicKey) (uint64, error) {
	if rpc == nil {
		return 0, types.ErrNilRPC
	}

	bc, err := fetchBondingCurve(ctx, rpc, mint)
	if err != nil {
		return 0, err
	}

	if bc.VirtualTokenReserves == 0 {
		return 0, fmt.Errorf("bonding curve has zero token reserves")
	}

	// price = virtual_quote_reserves / virtual_token_reserves (scaled by 1e9)
	price := new(big.Int).SetUint64(bc.VirtualQuoteReserves)
	price.Mul(price, big.NewInt(1e9))
	price.Div(price, new(big.Int).SetUint64(bc.VirtualTokenReserves))

	return price.Uint64(), nil
}

// --- internal helpers ---

type poolReserves struct {
	BaseReserves  uint64
	QuoteReserves uint64
}

func fetchPoolState(ctx context.Context, rpc *sdkrpc.Client, pool solana.PublicKey) (poolReserves, error) {
	info, err := rpc.Raw().GetAccountInfo(ctx, pool)
	if err != nil {
		return poolReserves{}, err
	}
	if info == nil || info.Value == nil || info.Value.Data == nil {
		return poolReserves{}, fmt.Errorf("pool account not found")
	}
	if info.Value.Owner != pumpamm.ProgramKey {
		return poolReserves{}, fmt.Errorf("pool account has unexpected owner %s", info.Value.Owner)
	}

	var state pumpamm.Pool
	if err := state.Unmarshal(info.Value.Data.GetBinary()); err != nil {
		return poolReserves{}, fmt.Errorf("decode pool: %w", err)
	}

	// Fetch pool token accounts for actual reserves
	res, err := rpc.Raw().GetMultipleAccounts(ctx, state.PoolBaseTokenAccount, state.PoolQuoteTokenAccount)
	if err != nil {
		return poolReserves{}, err
	}

	if res == nil || len(res.Value) < 2 || res.Value[0] == nil || res.Value[0].Data == nil || res.Value[1] == nil || res.Value[1].Data == nil {
		return poolReserves{}, fmt.Errorf("pool reserve token account not found")
	}
	if !isTokenProgram(res.Value[0].Owner) || !isTokenProgram(res.Value[1].Owner) {
		return poolReserves{}, fmt.Errorf("pool reserve account has unsupported token program")
	}
	var baseAccount, quoteAccount token.Account
	if err := bin.NewBinDecoder(res.Value[0].Data.GetBinary()).Decode(&baseAccount); err != nil {
		return poolReserves{}, fmt.Errorf("decode base reserve account: %w", err)
	}
	if err := bin.NewBinDecoder(res.Value[1].Data.GetBinary()).Decode(&quoteAccount); err != nil {
		return poolReserves{}, fmt.Errorf("decode quote reserve account: %w", err)
	}
	if baseAccount.Mint != state.BaseMint || quoteAccount.Mint != state.QuoteMint {
		return poolReserves{}, fmt.Errorf("pool reserve token account mint mismatch")
	}

	quoteReserves := new(big.Int).SetUint64(quoteAccount.Amount)
	quoteReserves.Add(quoteReserves, state.VirtualQuoteReserves.BigInt())
	if !quoteReserves.IsUint64() {
		return poolReserves{}, fmt.Errorf("effective quote reserves are outside uint64 range")
	}

	return poolReserves{BaseReserves: baseAccount.Amount, QuoteReserves: quoteReserves.Uint64()}, nil
}

type pumpQuoteState struct {
	BondingCurve pump.BondingCurve
	Global       pump.Global
	FeeConfig    *pump.FeeConfig
	MintSupply   uint64
}

func fetchPumpQuoteState(ctx context.Context, rpc *sdkrpc.Client, mint solana.PublicKey) (pumpQuoteState, error) {
	var state pumpQuoteState
	bondingCurve, _, err := solana.FindProgramAddress(
		[][]byte{[]byte(constants.SeedBondingCurve), mint[:]},
		pump.ProgramKey,
	)
	if err != nil {
		return state, fmt.Errorf("derive bonding curve: %w", err)
	}
	global, _, err := solana.FindProgramAddress([][]byte{[]byte(constants.SeedGlobal)}, pump.ProgramKey)
	if err != nil {
		return state, fmt.Errorf("derive global: %w", err)
	}
	feeConfig, _, err := solana.FindProgramAddress(
		[][]byte{[]byte("fee_config"), pump.ProgramKey[:]},
		constants.PumpFeeProgramID,
	)
	if err != nil {
		return state, fmt.Errorf("derive fee config: %w", err)
	}

	accounts, err := rpc.Raw().GetMultipleAccounts(ctx, bondingCurve, global, feeConfig, mint)
	if err != nil {
		return state, fmt.Errorf("fetch pump quote state: %w", err)
	}
	if accounts == nil || len(accounts.Value) != 4 {
		return state, fmt.Errorf("fetch pump quote state: expected 4 accounts")
	}
	if accounts.Value[0] == nil || accounts.Value[0].Data == nil {
		return state, fmt.Errorf("bonding curve not found for mint %s", mint)
	}
	if accounts.Value[0].Owner != pump.ProgramKey {
		return state, fmt.Errorf("bonding curve has unexpected owner %s", accounts.Value[0].Owner)
	}
	if err := state.BondingCurve.Unmarshal(accounts.Value[0].Data.GetBinary()); err != nil {
		return state, fmt.Errorf("decode bonding curve: %w", err)
	}
	if accounts.Value[1] == nil || accounts.Value[1].Data == nil {
		return state, fmt.Errorf("pump global account not found")
	}
	if accounts.Value[1].Owner != pump.ProgramKey {
		return state, fmt.Errorf("pump global account has unexpected owner %s", accounts.Value[1].Owner)
	}
	if err := state.Global.Unmarshal(accounts.Value[1].Data.GetBinary()); err != nil {
		return state, fmt.Errorf("decode pump global: %w", err)
	}
	if accounts.Value[2] != nil && accounts.Value[2].Data != nil {
		if accounts.Value[2].Owner != constants.PumpFeeProgramID {
			return state, fmt.Errorf("pump fee config has unexpected owner %s", accounts.Value[2].Owner)
		}
		state.FeeConfig = new(pump.FeeConfig)
		if err := state.FeeConfig.Unmarshal(accounts.Value[2].Data.GetBinary()); err != nil {
			return state, fmt.Errorf("decode pump fee config: %w", err)
		}
	}
	if accounts.Value[3] == nil || accounts.Value[3].Data == nil {
		return state, fmt.Errorf("mint account %s not found", mint)
	}
	if !isTokenProgram(accounts.Value[3].Owner) {
		return state, fmt.Errorf("mint %s has unsupported token program %s", mint, accounts.Value[3].Owner)
	}
	var mintState token.Mint
	if err := bin.NewBinDecoder(accounts.Value[3].Data.GetBinary()).Decode(&mintState); err != nil {
		return state, fmt.Errorf("decode mint %s: %w", mint, err)
	}
	state.MintSupply = mintState.Supply
	return state, nil
}

func (s pumpQuoteState) feeBasisPoints(mintSupply uint64) (uint64, uint64, error) {
	if s.FeeConfig == nil {
		return s.Global.FeeBasisPoints, s.Global.CreatorFeeBasisPoints, nil
	}
	if len(s.FeeConfig.FeeTiers) == 0 {
		return 0, 0, fmt.Errorf("pump fee config has no fee tiers")
	}
	if s.BondingCurve.VirtualTokenReserves == 0 {
		return 0, 0, nil
	}

	marketCap := new(big.Int).Mul(
		new(big.Int).SetUint64(mintSupply),
		new(big.Int).SetUint64(s.BondingCurve.VirtualQuoteReserves),
	)
	marketCap.Div(marketCap, new(big.Int).SetUint64(s.BondingCurve.VirtualTokenReserves))
	selected := s.FeeConfig.FeeTiers[0].Fees
	for i := len(s.FeeConfig.FeeTiers) - 1; i >= 0; i-- {
		tier := s.FeeConfig.FeeTiers[i]
		if marketCap.Cmp(tier.MarketCapLamportsThreshold.BigInt()) >= 0 {
			selected = tier.Fees
			break
		}
	}
	return selected.ProtocolFeeBps, selected.CreatorFeeBps, nil
}

func addBasisPoints(a, b uint64) (uint64, error) {
	result := new(big.Int).Add(new(big.Int).SetUint64(a), new(big.Int).SetUint64(b))
	if !result.IsUint64() {
		return 0, fmt.Errorf("fee basis points overflow")
	}
	return result.Uint64(), nil
}

func isTokenProgram(program solana.PublicKey) bool {
	return program == constants.TokenProgramID || program == constants.Token2022ProgramID
}

func fetchBondingCurve(ctx context.Context, rpc *sdkrpc.Client, mint solana.PublicKey) (pump.BondingCurve, error) {
	var bc pump.BondingCurve

	// Derive bonding curve PDA
	bcAddr, _, err := solana.FindProgramAddress(
		[][]byte{[]byte(constants.SeedBondingCurve), mint.Bytes()},
		pump.ProgramKey,
	)
	if err != nil {
		return bc, fmt.Errorf("derive bonding curve: %w", err)
	}

	info, err := rpc.Raw().GetAccountInfo(ctx, bcAddr)
	if err != nil {
		return bc, err
	}
	if info == nil || info.Value == nil || info.Value.Data == nil {
		return bc, fmt.Errorf("bonding curve not found for mint %s", mint)
	}
	if info.Value.Owner != pump.ProgramKey {
		return bc, fmt.Errorf("bonding curve has unexpected owner %s", info.Value.Owner)
	}

	if err := bc.Unmarshal(info.Value.Data.GetBinary()); err != nil {
		return bc, fmt.Errorf("decode bonding curve: %w", err)
	}

	return bc, nil
}

func calculatePriceMetrics(reserves poolReserves, quoteAmount, baseAmount uint64, isBuy bool) (spotPrice, execPrice, impactBps uint64) {
	if reserves.BaseReserves == 0 || baseAmount == 0 {
		return 0, 0, 0
	}

	// Spot price = quote_reserves / base_reserves (scaled by 1e9)
	spot := new(big.Int).SetUint64(reserves.QuoteReserves)
	spot.Mul(spot, big.NewInt(1e9))
	spot.Div(spot, new(big.Int).SetUint64(reserves.BaseReserves))
	spotPrice = spot.Uint64()

	// Execution price = quote_amount / base_amount (scaled by 1e9)
	exec := new(big.Int).SetUint64(quoteAmount)
	exec.Mul(exec, big.NewInt(1e9))
	exec.Div(exec, new(big.Int).SetUint64(baseAmount))
	execPrice = exec.Uint64()

	// Price impact calculation
	if spotPrice > 0 {
		if isBuy {
			// For buys, execution price > spot price means negative impact
			if execPrice > spotPrice {
				impactBps = (execPrice - spotPrice) * 10000 / spotPrice
			}
		} else {
			// For sells, execution price < spot price means negative impact
			if spotPrice > execPrice {
				impactBps = (spotPrice - execPrice) * 10000 / spotPrice
			}
		}
	}

	return spotPrice, execPrice, impactBps
}

func simulateQuoteOut(ctx context.Context, rpc *sdkrpc.Client, signer wallet.Signer, quoteATA solana.PublicKey, ix solana.Instruction) (uint64, error) {
	// Get pre-balance
	preInfo, err := rpc.Raw().GetAccountInfo(ctx, quoteATA)
	if err != nil {
		return 0, err
	}
	var pre uint64
	if preInfo != nil && preInfo.Value != nil && preInfo.Value.Data != nil {
		dec := bin.NewBinDecoder(preInfo.Value.Data.GetBinary())
		var acc token.Account
		if err := dec.Decode(&acc); err == nil {
			pre = acc.Amount
		}
	}

	// Build and simulate
	builder := txbuilder.NewBuilder(rpc, solanarpc.CommitmentConfirmed)
	tx, err := builder.BuildTransaction(ctx, signer.PublicKey(), ix)
	if err != nil {
		return 0, err
	}
	if err := txbuilder.SignTransaction(ctx, tx, signer); err != nil {
		return 0, err
	}

	res, err := rpc.SimulateTransaction(ctx, tx, &solanarpc.SimulateTransactionOpts{
		SigVerify: true,
		Accounts: &solanarpc.SimulateTransactionAccountsOpts{
			Encoding:  solana.EncodingBase64,
			Addresses: []solana.PublicKey{quoteATA},
		},
	})
	if err != nil {
		return 0, err
	}
	if res == nil || res.Value == nil {
		return 0, fmt.Errorf("simulate empty result")
	}
	if res.Value.Err != nil {
		return 0, fmt.Errorf("simulate error: %v", res.Value.Err)
	}
	if len(res.Value.Accounts) == 0 || res.Value.Accounts[0] == nil {
		return 0, fmt.Errorf("simulate missing account data")
	}

	data := res.Value.Accounts[0].Data.GetBinary()
	dec := bin.NewBinDecoder(data)
	var postAcc token.Account
	if err := dec.Decode(&postAcc); err != nil {
		return 0, err
	}

	if postAcc.Amount < pre {
		return 0, fmt.Errorf("quote decreased")
	}
	return postAcc.Amount - pre, nil
}

func applySlippage(amount uint64, slippageBps uint64) uint64 {
	if slippageBps >= 10000 {
		return 0
	}
	return amount * (10000 - slippageBps) / 10000
}
