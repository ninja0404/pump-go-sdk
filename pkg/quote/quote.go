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

// PumpBuyQuote estimates the token output for a given SOL input on Pump bonding curve.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - mint: token mint address
//   - solLamports: SOL amount to spend (lamports)
//
// Returns estimated token output amount.
func PumpBuyQuote(ctx context.Context, rpc *sdkrpc.Client, mint solana.PublicKey, solLamports uint64) (uint64, error) {
	if rpc == nil {
		return 0, types.ErrNilRPC
	}
	if solLamports == 0 {
		return 0, types.NewValidationError("solLamports", "must be greater than 0")
	}

	// Fetch bonding curve state
	bc, err := fetchBondingCurve(ctx, rpc, mint)
	if err != nil {
		return 0, err
	}

	// Calculate using bonding curve formula
	// tokens_out = (sol_in * virtual_token_reserves) / (virtual_sol_reserves + sol_in)
	solIn := new(big.Int).SetUint64(solLamports)
	tokenReserves := new(big.Int).SetUint64(bc.VirtualTokenReserves)
	solReserves := new(big.Int).SetUint64(bc.VirtualSolReserves)

	numerator := new(big.Int).Mul(solIn, tokenReserves)
	denominator := new(big.Int).Add(solReserves, solIn)

	tokensOut := new(big.Int).Div(numerator, denominator)

	return tokensOut.Uint64(), nil
}

// PumpSellQuote estimates the SOL output for a given token input on Pump bonding curve.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - mint: token mint address
//   - tokenAmount: token amount to sell (base units)
//
// Returns estimated SOL output amount (lamports).
func PumpSellQuote(ctx context.Context, rpc *sdkrpc.Client, mint solana.PublicKey, tokenAmount uint64) (uint64, error) {
	if rpc == nil {
		return 0, types.ErrNilRPC
	}
	if tokenAmount == 0 {
		return 0, types.NewValidationError("tokenAmount", "must be greater than 0")
	}

	// Fetch bonding curve state
	bc, err := fetchBondingCurve(ctx, rpc, mint)
	if err != nil {
		return 0, err
	}

	// Calculate using bonding curve formula
	// sol_out = (token_in * virtual_sol_reserves) / (virtual_token_reserves + token_in)
	tokenIn := new(big.Int).SetUint64(tokenAmount)
	tokenReserves := new(big.Int).SetUint64(bc.VirtualTokenReserves)
	solReserves := new(big.Int).SetUint64(bc.VirtualSolReserves)

	numerator := new(big.Int).Mul(tokenIn, solReserves)
	denominator := new(big.Int).Add(tokenReserves, tokenIn)

	solOut := new(big.Int).Div(numerator, denominator)

	return solOut.Uint64(), nil
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
// Returns price as SOL per token, scaled by 1e9.
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

	// price = virtual_sol_reserves / virtual_token_reserves (scaled by 1e9)
	price := new(big.Int).SetUint64(bc.VirtualSolReserves)
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

	var state pumpamm.Pool
	if err := state.Unmarshal(info.Value.Data.GetBinary()); err != nil {
		return poolReserves{}, fmt.Errorf("decode pool: %w", err)
	}

	// Fetch pool token accounts for actual reserves
	res, err := rpc.Raw().GetMultipleAccounts(ctx, state.PoolBaseTokenAccount, state.PoolQuoteTokenAccount)
	if err != nil {
		return poolReserves{}, err
	}

	var baseReserves, quoteReserves uint64
	if len(res.Value) >= 2 {
		if res.Value[0] != nil && res.Value[0].Data != nil {
			dec := bin.NewBinDecoder(res.Value[0].Data.GetBinary())
			var acc token.Account
			if err := dec.Decode(&acc); err == nil {
				baseReserves = acc.Amount
			}
		}
		if res.Value[1] != nil && res.Value[1].Data != nil {
			dec := bin.NewBinDecoder(res.Value[1].Data.GetBinary())
			var acc token.Account
			if err := dec.Decode(&acc); err == nil {
				quoteReserves = acc.Amount
			}
		}
	}

	return poolReserves{BaseReserves: baseReserves, QuoteReserves: quoteReserves}, nil
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
