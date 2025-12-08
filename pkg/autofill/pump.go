package autofill

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/jito"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/types"
	"github.com/ninja0404/pump-go-sdk/pkg/vanity"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// PumpBuy constructs a pump buy instruction with auto-filled accounts.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: buyer's public key (fee payer and signer)
//   - mint: token mint address to buy
//   - amount: token amount to buy (in base units, e.g. 1e6 for 1 token with 6 decimals)
//   - maxSol: maximum SOL to spend (in lamports, e.g. 1e9 for 1 SOL)
//   - opts: optional configurations (WithOverrides, WithTrackVolume, etc.)
//
// Returns:
//   - BuyAccounts: auto-filled account addresses
//   - BuyArgs: instruction arguments
//   - []Instruction: instructions to execute (may include ATA creation)
//   - error: validation or RPC errors
//
// Example:
//
//	accts, args, instrs, err := autofill.PumpBuy(ctx, rpc, user, mint, 1_000_000, 100_000_000)
func PumpBuy(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey, amount, maxSol uint64, opts ...Option) (pump.BuyAccounts, pump.BuyArgs, []solana.Instruction, error) {
	// Input validation
	if rpc == nil {
		return pump.BuyAccounts{}, pump.BuyArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.BuyAccounts{}, pump.BuyArgs{}, nil, err
	}
	if err := types.ValidatePublicKey("mint", mint); err != nil {
		return pump.BuyAccounts{}, pump.BuyArgs{}, nil, err
	}
	if err := types.ValidateBuyParams(amount, maxSol); err != nil {
		return pump.BuyAccounts{}, pump.BuyArgs{}, nil, err
	}

	options := &Options{TrackVolume: true}
	for _, opt := range opts {
		opt(options)
	}

	accts, err := pumpAutofillBuy(ctx, rpc, user, mint)
	if err != nil {
		return pump.BuyAccounts{}, pump.BuyArgs{}, nil, err
	}
	applyOverrides(&accts, options.Overrides)

	args := pump.BuyArgs{
		Amount:      amount,
		MaxSolCost:  maxSol,
		TrackVolume: pump.OptionBool{Field0: options.TrackVolume},
	}

	// 批量检查 ATA 是否存在
	ataReqs := []ataRequest{
		{Payer: accts.User, Wallet: accts.User, Mint: accts.Mint, TokenProgram: accts.TokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: accts.User, Wallet: accts.BondingCurve, Mint: accts.Mint, TokenProgram: accts.TokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
	}
	instrs, err := ensureATABatch(ctx, rpc, ataReqs)
	if err != nil {
		return pump.BuyAccounts{}, pump.BuyArgs{}, nil, err
	}

	ix, err := pump.BuildBuy(accts, args)
	if err != nil {
		return pump.BuyAccounts{}, pump.BuyArgs{}, nil, err
	}
	instrs = append(instrs, ix)
	// Append Jito tip if configured
	instrs = appendJitoTipPump(instrs, user, options)
	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pump.BuyAccounts `json:"accounts"`
			Args     pump.BuyArgs     `json:"args"`
		}{accts, args})
	}
	return accts, args, instrs, nil
}

// PumpBuyExactSolIn constructs a buy instruction with exact SOL input.
//
// Unlike PumpBuy which specifies token amount, this function lets you specify
// exact SOL amount to spend and minimum tokens to receive (slippage protection).
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: buyer's public key
//   - mint: token mint address
//   - spendableSolIn: exact SOL to spend (lamports)
//   - minTokensOut: minimum tokens to receive (slippage protection)
//   - opts: optional configurations
//
// Returns accounts, args, instructions, and any error.
func PumpBuyExactSolIn(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey, spendableSolIn, minTokensOut uint64, opts ...Option) (pump.BuyExactSolInAccounts, pump.BuyExactSolInArgs, []solana.Instruction, error) {
	// Input validation
	if rpc == nil {
		return pump.BuyExactSolInAccounts{}, pump.BuyExactSolInArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.BuyExactSolInAccounts{}, pump.BuyExactSolInArgs{}, nil, err
	}
	if err := types.ValidatePublicKey("mint", mint); err != nil {
		return pump.BuyExactSolInAccounts{}, pump.BuyExactSolInArgs{}, nil, err
	}
	if spendableSolIn == 0 {
		return pump.BuyExactSolInAccounts{}, pump.BuyExactSolInArgs{}, nil, types.NewValidationError("spendableSolIn", "must be greater than 0")
	}

	options := &Options{TrackVolume: true}
	for _, opt := range opts {
		opt(options)
	}

	baseAccts, err := pumpAutofillBuy(ctx, rpc, user, mint)
	if err != nil {
		return pump.BuyExactSolInAccounts{}, pump.BuyExactSolInArgs{}, nil, err
	}
	applyOverrides(&baseAccts, options.Overrides)

	accts := pump.BuyExactSolInAccounts{
		Global:                  baseAccts.Global,
		FeeRecipient:            baseAccts.FeeRecipient,
		Mint:                    baseAccts.Mint,
		BondingCurve:            baseAccts.BondingCurve,
		AssociatedBondingCurve:  baseAccts.AssociatedBondingCurve,
		AssociatedUser:          baseAccts.AssociatedUser,
		User:                    baseAccts.User,
		SystemProgram:           baseAccts.SystemProgram,
		TokenProgram:            baseAccts.TokenProgram,
		CreatorVault:            baseAccts.CreatorVault,
		EventAuthority:          baseAccts.EventAuthority,
		Program:                 baseAccts.Program,
		GlobalVolumeAccumulator: baseAccts.GlobalVolumeAccumulator,
		UserVolumeAccumulator:   baseAccts.UserVolumeAccumulator,
		FeeConfig:               baseAccts.FeeConfig,
		FeeProgram:              baseAccts.FeeProgram,
	}

	args := pump.BuyExactSolInArgs{
		SpendableSolIn: spendableSolIn,
		MinTokensOut:   minTokensOut,
		TrackVolume: pump.OptionBool{
			Field0: options.TrackVolume,
		},
	}

	// 批量检查 ATA 是否存在
	ataReqs := []ataRequest{
		{Payer: accts.User, Wallet: accts.User, Mint: accts.Mint, TokenProgram: accts.TokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: accts.User, Wallet: accts.BondingCurve, Mint: accts.Mint, TokenProgram: accts.TokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
	}
	instrs, err := ensureATABatch(ctx, rpc, ataReqs)
	if err != nil {
		return pump.BuyExactSolInAccounts{}, pump.BuyExactSolInArgs{}, nil, err
	}

	ix, err := pump.BuildBuyExactSolIn(accts, args)
	if err != nil {
		return pump.BuyExactSolInAccounts{}, pump.BuyExactSolInArgs{}, nil, err
	}
	instrs = append(instrs, ix)
	// Append Jito tip if configured
	instrs = appendJitoTipPump(instrs, user, options)
	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pump.BuyExactSolInAccounts `json:"accounts"`
			Args     pump.BuyExactSolInArgs     `json:"args"`
		}{accts, args})
	}
	return accts, args, instrs, nil
}

// PumpSell constructs a pump sell instruction with auto-filled accounts.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: seller's public key
//   - mint: token mint address to sell
//   - amount: token amount to sell (base units)
//   - minSol: minimum SOL to receive (lamports, slippage protection)
//   - opts: optional configurations
//
// Returns accounts, args, single instruction, and any error.
// Note: For automatic slippage calculation, use PumpSellWithSlippage instead.
func PumpSell(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey, amount, minSol uint64, opts ...Option) (pump.SellAccounts, pump.SellArgs, solana.Instruction, error) {
	// Input validation
	if rpc == nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	if err := types.ValidatePublicKey("mint", mint); err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	if amount == 0 {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, types.NewValidationError("amount", "must be greater than 0")
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	accts, err := pumpAutofillSell(ctx, rpc, user, mint)
	if err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	applyOverrides(&accts, options.Overrides)

	args := pump.SellArgs{
		Amount:       amount,
		MinSolOutput: minSol,
	}

	ix, err := pump.BuildSell(accts, args)
	if err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pump.SellAccounts `json:"accounts"`
			Args     pump.SellArgs     `json:"args"`
		}{accts, args})
	}
	return accts, args, ix, nil
}

// PumpSellWithSlippage sells tokens with automatic slippage calculation.
//
// This function simulates the sell to estimate SOL output, then applies slippage
// to calculate minimum acceptable SOL. If selling all tokens, it also closes
// the ATA to reclaim rent.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - signer: wallet signer (must be able to sign transactions)
//   - mint: token mint address
//   - amount: token amount to sell (base units)
//   - slippageBps: slippage tolerance in basis points (100 = 1%, max 10000)
//   - opts: optional configurations
//
// Returns accounts, args, instructions (including ATA close if applicable), and error.
//
// Example:
//
//	// Sell 1M tokens with 1% slippage
//	accts, args, instrs, err := autofill.PumpSellWithSlippage(ctx, rpc, user, mint, 1_000_000, 100)
func PumpSellWithSlippage(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey, amount uint64, slippageBps uint64, opts ...Option) (pump.SellAccounts, pump.SellArgs, []solana.Instruction, error) {
	// Input validation
	if rpc == nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	if err := types.ValidatePublicKey("mint", mint); err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	if amount == 0 {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, types.NewValidationError("amount", "must be greater than 0")
	}
	if err := types.ValidateSlippage(slippageBps); err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	accts, _, ixBase, err := PumpSell(ctx, rpc, user, mint, amount, 0, opts...)
	if err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}

	// 批量检查 ATA 是否存在（同时获取余额）
	ataReqs := []ataRequest{
		{Payer: user, Wallet: accts.User, Mint: mint, TokenProgram: accts.TokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
	}
	ataResult, err := ensureATABatchWithBalances(ctx, rpc, ataReqs)
	if err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	instrs := ataResult.Instructions

	quoteOut, err := simulateSolOut(ctx, rpc, user, accts, amount, instrs, ixBase)
	if err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	minSol := applySlippage(quoteOut, slippageBps)

	args := pump.SellArgs{
		Amount:       amount,
		MinSolOutput: minSol,
	}
	ix, err := pump.BuildSell(accts, args)
	if err != nil {
		return pump.SellAccounts{}, pump.SellArgs{}, nil, err
	}
	instrs = append(instrs, ix)

	// Close ATA only if explicitly requested
	if options.CloseBaseATA {
		instrs = append(instrs, buildCloseAccount(accts.AssociatedUser, user, user, accts.TokenProgram))
	}
	// Append Jito tip if configured
	instrs = appendJitoTipPump(instrs, user, options)

	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pump.SellAccounts `json:"accounts"`
			Args     pump.SellArgs     `json:"args"`
		}{accts, args})
	}
	return accts, args, instrs, nil
}

// BuildAndSend executes the instruction with provided signer/txbuilder.
func BuildAndSend(ctx context.Context, builder *txbuilder.Builder, signer wallet.Signer, ix solana.Instruction) (solana.Signature, error) {
	if builder == nil || signer == nil {
		return solana.Signature{}, fmt.Errorf("builder and signer are required")
	}
	return builder.BuildSignSend(ctx, signer, nil, ix)
}

// BuildAndSimulate simulates the instruction without signature.
func BuildAndSimulate(ctx context.Context, rpc *sdkrpc.Client, builder *txbuilder.Builder, user solana.PublicKey, ix solana.Instruction) (*solanarpc.SimulateTransactionResponse, error) {
	if rpc == nil || builder == nil {
		return nil, fmt.Errorf("rpc and builder required")
	}
	tx, err := builder.BuildTransaction(ctx, user, ix)
	if err != nil {
		return nil, err
	}
	return rpc.SimulateTransaction(ctx, tx, &solanarpc.SimulateTransactionOpts{
		SigVerify: false,
	})
}

// simulateSolOut returns lamports delta of user main account after simulating sell ix (MinSolOutput=0).
func simulateSolOut(ctx context.Context, rpc *sdkrpc.Client, user solana.PublicKey, accounts pump.SellAccounts, amount uint64, prefix []solana.Instruction, baseIx solana.Instruction) (uint64, error) {
	preRes, err := rpc.Raw().GetBalance(ctx, user, solanarpc.CommitmentConfirmed)
	if err != nil {
		return 0, err
	}
	pre := preRes.Value
	instrs := append([]solana.Instruction{}, prefix...)
	if baseIx == nil {
		ix, err := pump.BuildSell(accounts, pump.SellArgs{
			Amount:       amount,
			MinSolOutput: 0,
		})
		if err != nil {
			return 0, err
		}
		instrs = append(instrs, ix)
	} else {
		instrs = append(instrs, baseIx)
	}
	builder := txbuilder.NewBuilder(rpc, solanarpc.CommitmentConfirmed)
	tx, err := builder.BuildTransaction(ctx, user, instrs...)
	if err != nil {
		return 0, err
	}
	res, err := rpc.SimulateTransaction(ctx, tx, &solanarpc.SimulateTransactionOpts{
		SigVerify: false,
		Accounts: &solanarpc.SimulateTransactionAccountsOpts{
			Encoding:  solana.EncodingBase64,
			Addresses: []solana.PublicKey{user},
		},
	})
	if err != nil {
		return 0, err
	}
	if res != nil && res.Value != nil && res.Value.Err != nil {
		return 0, types.ParseSimulationError(res.Value.Err, res.Value.Logs)
	}
	if res == nil || res.Value == nil || len(res.Value.Accounts) == 0 || res.Value.Accounts[0] == nil {
		return 0, fmt.Errorf("simulate result empty")
	}
	post := res.Value.Accounts[0].Lamports
	if post < pre {
		return 0, fmt.Errorf("simulate: lamports decreased %d -> %d", pre, post)
	}
	return post - pre, nil
}

// --- internal helpers ---

func pumpAutofillBuy(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey) (pump.BuyAccounts, error) {
	var accts pump.BuyAccounts

	accts = pump.BuyAccounts{
		Mint:          mint,
		User:          user,
		SystemProgram: constants.SystemProgramID,
		Program:       pump.ProgramKey,
		FeeProgram:    constants.PumpFeeProgramID,
	}
	// PDAs (non-account dependent)
	if pk, _, err := pump.DeriveBuyGlobalPDA(accts, pump.BuyArgs{}); err == nil {
		accts.Global = pk
	}
	if pk, _, err := pump.DeriveBuyBondingCurvePDA(accts, pump.BuyArgs{}); err == nil {
		accts.BondingCurve = pk
	}
	if pk, _, err := pump.DeriveBuyEventAuthorityPDA(accts, pump.BuyArgs{}); err == nil {
		accts.EventAuthority = pk
	}
	if pk, _, err := pump.DeriveBuyGlobalVolumeAccumulatorPDA(accts, pump.BuyArgs{}); err == nil {
		accts.GlobalVolumeAccumulator = pk
	}
	if pk, _, err := pump.DeriveBuyUserVolumeAccumulatorPDA(accts, pump.BuyArgs{}); err == nil {
		accts.UserVolumeAccumulator = pk
	}
	if pk, _, err := pump.DeriveBuyFeeConfigPDA(accts, pump.BuyArgs{}); err == nil {
		accts.FeeConfig = pk
	}

	// batch fetch required accounts (global, mint, bonding_curve)
	addrs := []solana.PublicKey{accts.Global, accts.Mint, accts.BondingCurve}
	amap, err := fetchAccountsBatch(ctx, rpc, addrs...)
	if err != nil {
		return accts, err
	}

	// parse global for fee recipient
	globalAcc := amap[accts.Global.String()]
	if globalAcc != nil && globalAcc.Data != nil {
		var globalState pump.Global
		if err := globalState.Unmarshal(globalAcc.Data.GetBinary()); err == nil {
			feeRecipient := firstNonZeroPK(append(globalState.FeeRecipients[:], globalState.FeeRecipient))
			if !isZeroPK(feeRecipient) {
				accts.FeeRecipient = feeRecipient
			}
		}
	}
	if isZeroPK(accts.FeeRecipient) {
		return accts, fmt.Errorf("fee recipient not found in global config for mint %s", mint)
	}

	// identify token program from mint owner
	mintAcc := amap[accts.Mint.String()]
	if mintAcc == nil {
		return accts, fmt.Errorf("mint account %s not found (may be invalid mint or RPC issue)", mint)
	}
	accts.TokenProgram = mintAcc.Owner

	// derive user ATA
	assocUser, _, err := findATAWithProgram(accts.User, accts.Mint, accts.TokenProgram, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive user ATA for mint %s: %w", mint, err)
	}
	accts.AssociatedUser = assocUser

	// derive AssociatedBondingCurve as ATA(bondingCurve, mint, tokenProgram)
	assocBC, _, err := findATAWithProgram(accts.BondingCurve, accts.Mint, accts.TokenProgram, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive bonding curve ATA for mint %s: %w", mint, err)
	}
	accts.AssociatedBondingCurve = assocBC

	// parse bonding_curve for creator vault
	bcAcc := amap[accts.BondingCurve.String()]
	if bcAcc == nil || bcAcc.Data == nil {
		return accts, fmt.Errorf("bonding_curve account %s not found for mint %s", accts.BondingCurve, mint)
	}
	var bc pump.BondingCurve
	if err := bc.Unmarshal(bcAcc.Data.GetBinary()); err != nil {
		return accts, fmt.Errorf("decode bonding_curve %s: %w", accts.BondingCurve, err)
	}
	if pk, _, err := solana.FindProgramAddress([][]byte{[]byte(constants.SeedCreatorVault), bc.Creator[:]}, pump.ProgramKey); err == nil {
		accts.CreatorVault = pk
	}

	return accts, nil
}

func pumpAutofillSell(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey) (pump.SellAccounts, error) {
	var accts pump.SellAccounts

	accts = pump.SellAccounts{
		Mint:          mint,
		User:          user,
		SystemProgram: constants.SystemProgramID,
		Program:       pump.ProgramKey,
		FeeProgram:    constants.PumpFeeProgramID,
	}
	// PDAs (non-account dependent)
	if pk, _, err := pump.DeriveSellGlobalPDA(accts, pump.SellArgs{}); err == nil {
		accts.Global = pk
	}
	if pk, _, err := pump.DeriveSellBondingCurvePDA(accts, pump.SellArgs{}); err == nil {
		accts.BondingCurve = pk
	}
	if pk, _, err := pump.DeriveSellEventAuthorityPDA(accts, pump.SellArgs{}); err == nil {
		accts.EventAuthority = pk
	}
	if pk, _, err := pump.DeriveSellFeeConfigPDA(accts, pump.SellArgs{}); err == nil {
		accts.FeeConfig = pk
	}

	// batch fetch required accounts (global, mint, bonding_curve)
	addrs := []solana.PublicKey{accts.Global, accts.Mint, accts.BondingCurve}
	amap, err := fetchAccountsBatch(ctx, rpc, addrs...)
	if err != nil {
		return accts, err
	}

	// parse global for fee recipient
	globalAcc := amap[accts.Global.String()]
	if globalAcc == nil || globalAcc.Data == nil {
		return accts, fmt.Errorf("global account %s not found for mint %s", accts.Global, mint)
	}
	var globalState pump.Global
	if err := globalState.Unmarshal(globalAcc.Data.GetBinary()); err != nil {
		return accts, fmt.Errorf("decode global %s: %w", accts.Global, err)
	}
	feeRecipient := firstNonZeroPK(append(globalState.FeeRecipients[:], globalState.FeeRecipient))
	if isZeroPK(feeRecipient) {
		return accts, fmt.Errorf("fee recipient not found in global config for mint %s", mint)
	}
	accts.FeeRecipient = feeRecipient

	// identify token program from mint owner
	mintAcc := amap[accts.Mint.String()]
	if mintAcc == nil {
		return accts, fmt.Errorf("mint account %s not found (may be invalid mint or RPC issue)", mint)
	}
	accts.TokenProgram = mintAcc.Owner

	// derive user ATA
	assocUser, _, err := findATAWithProgram(accts.User, accts.Mint, accts.TokenProgram, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive user ATA for mint %s: %w", mint, err)
	}
	accts.AssociatedUser = assocUser

	// derive AssociatedBondingCurve as ATA(bondingCurve, mint, tokenProgram)
	assocBC, _, err := findATAWithProgram(accts.BondingCurve, accts.Mint, accts.TokenProgram, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive bonding curve ATA for mint %s: %w", mint, err)
	}
	accts.AssociatedBondingCurve = assocBC

	// parse bonding_curve for creator vault
	bcAcc := amap[accts.BondingCurve.String()]
	if bcAcc == nil || bcAcc.Data == nil {
		return accts, fmt.Errorf("bonding_curve account %s not found for mint %s", accts.BondingCurve, mint)
	}
	var bc pump.BondingCurve
	if err := bc.Unmarshal(bcAcc.Data.GetBinary()); err != nil {
		return accts, fmt.Errorf("decode bonding_curve %s: %w", accts.BondingCurve, err)
	}
	if pk, _, err := solana.FindProgramAddress([][]byte{[]byte(constants.SeedCreatorVault), bc.Creator[:]}, pump.ProgramKey); err == nil {
		accts.CreatorVault = pk
	}

	return accts, nil
}

func applyOverrides(target interface{}, m map[string]solana.PublicKey) {
	if len(m) == 0 {
		return
	}
	applyPubkeyOverrides(target, m) // ignore error: field mismatch will panic; ensure map keys are valid
}

// PumpCreate creates a new SPL Token on Pump.fun bonding curve.
//
// This function uses the 'create' instruction which creates SPL Token (not Token-2022).
// For Token-2022 tokens, use PumpCreateV2 instead.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: creator's public key (fee payer and signer)
//   - name: token name (e.g., "My Token")
//   - symbol: token symbol (e.g., "MTK")
//   - uri: metadata URI (e.g., "https://example.com/metadata.json")
//   - opts: optional configurations (WithVanitySuffix, WithVanityPrefix, etc.)
//
// Returns:
//   - CreateAccounts: auto-filled account addresses
//   - CreateArgs: instruction arguments
//   - solana.Instruction: the create instruction
//   - solana.PrivateKey: the generated mint keypair (must be added as signer)
//   - error: validation or RPC errors
//
// Example:
//
//	accts, args, ix, mintKey, err := autofill.PumpCreate(ctx, rpc, user, "My Token", "MTK", "https://...")
//	// Sign with both user and mintKey
func PumpCreate(ctx context.Context, rpc *sdkrpc.Client, user solana.PublicKey, name, symbol, uri string, opts ...Option) (pump.CreateAccounts, pump.CreateArgs, solana.Instruction, solana.PrivateKey, error) {
	// Input validation
	if rpc == nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, nil, err
	}
	if name == "" {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, nil, types.NewValidationError("name", "cannot be empty")
	}
	if symbol == "" {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, nil, types.NewValidationError("symbol", "cannot be empty")
	}
	if uri == "" {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, nil, types.NewValidationError("uri", "cannot be empty")
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	// Generate mint keypair (with optional vanity address)
	mintKey, err := generateMintKey(ctx, options)
	if err != nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, nil, err
	}
	mint := mintKey.PublicKey()

	// Auto-fill accounts
	accts, err := pumpAutofillCreate(ctx, rpc, user, mint)
	if err != nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, nil, err
	}
	applyOverrides(&accts, options.Overrides)

	// Build args (creator defaults to user)
	args := pump.CreateArgs{
		Name:    name,
		Symbol:  symbol,
		Uri:     uri,
		Creator: user,
	}

	ix, err := pump.BuildCreate(accts, args)
	if err != nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, nil, err
	}

	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pump.CreateAccounts `json:"accounts"`
			Args     pump.CreateArgs     `json:"args"`
			Mint     string              `json:"mint"`
		}{accts, args, mint.String()})
	}

	return accts, args, ix, mintKey, nil
}

// generateMintKey generates a mint keypair with optional vanity address.
func generateMintKey(ctx context.Context, options *Options) (solana.PrivateKey, error) {
	if options.VanitySuffix != "" || options.VanityPrefix != "" {
		timeout := options.VanityTimeout
		if timeout == 0 {
			timeout = 5 * time.Minute
		}
		result, err := vanity.Generate(ctx, vanity.Options{
			Prefix:  options.VanityPrefix,
			Suffix:  options.VanitySuffix,
			Timeout: timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("generate vanity address: %w", err)
		}
		return result.PrivateKey, nil
	}
	mintKey, err := solana.NewRandomPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("generate mint keypair: %w", err)
	}
	return mintKey, nil
}

// PumpCreateWithMint creates an SPL Token with a pre-generated mint keypair.
//
// Use this when you want to control the mint address (e.g., for vanity addresses).
// For Token-2022 tokens, use PumpCreateV2WithMint instead.
func PumpCreateWithMint(ctx context.Context, rpc *sdkrpc.Client, user solana.PublicKey, mintKey solana.PrivateKey, name, symbol, uri string, opts ...Option) (pump.CreateAccounts, pump.CreateArgs, solana.Instruction, error) {
	if rpc == nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, err
	}
	if mintKey == nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, types.NewValidationError("mintKey", "cannot be nil")
	}
	if name == "" {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, types.NewValidationError("name", "cannot be empty")
	}
	if symbol == "" {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, types.NewValidationError("symbol", "cannot be empty")
	}
	if uri == "" {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, types.NewValidationError("uri", "cannot be empty")
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	mint := mintKey.PublicKey()
	accts, err := pumpAutofillCreate(ctx, rpc, user, mint)
	if err != nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, err
	}
	applyOverrides(&accts, options.Overrides)

	args := pump.CreateArgs{
		Name:    name,
		Symbol:  symbol,
		Uri:     uri,
		Creator: user,
	}

	ix, err := pump.BuildCreate(accts, args)
	if err != nil {
		return pump.CreateAccounts{}, pump.CreateArgs{}, nil, err
	}

	return accts, args, ix, nil
}

// pumpAutofillCreate auto-fills accounts for create instruction (SPL Token).
func pumpAutofillCreate(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey) (pump.CreateAccounts, error) {
	accts := pump.CreateAccounts{
		Mint:                   mint,
		User:                   user,
		SystemProgram:          constants.SystemProgramID,
		TokenProgram:           constants.TokenProgramID, // SPL Token
		AssociatedTokenProgram: constants.AssociatedTokenProgramID,
		Rent:                   constants.SysvarRentProgramID,
		MplTokenMetadata:       constants.MetadataProgramID,
		Program:                pump.ProgramKey,
	}

	// Derive MintAuthority PDA
	if pk, _, err := pump.DeriveCreateMintAuthorityPDA(accts, pump.CreateArgs{}); err == nil {
		accts.MintAuthority = pk
	}

	// Derive BondingCurve PDA
	if pk, _, err := pump.DeriveCreateBondingCurvePDA(accts, pump.CreateArgs{}); err == nil {
		accts.BondingCurve = pk
	}

	// Derive Global PDA
	if pk, _, err := pump.DeriveCreateGlobalPDA(accts, pump.CreateArgs{}); err == nil {
		accts.Global = pk
	}

	// Derive EventAuthority PDA
	if pk, _, err := pump.DeriveCreateEventAuthorityPDA(accts, pump.CreateArgs{}); err == nil {
		accts.EventAuthority = pk
	}

	// Derive Metadata PDA (metaplex standard)
	metadataSeeds := [][]byte{
		[]byte("metadata"),
		constants.MetadataProgramID[:],
		mint[:],
	}
	if pk, _, err := solana.FindProgramAddress(metadataSeeds, constants.MetadataProgramID); err == nil {
		accts.Metadata = pk
	}

	// Derive AssociatedBondingCurve (ATA using SPL Token)
	assocBC, _, err := findATAWithProgram(accts.BondingCurve, mint, constants.TokenProgramID, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive bonding curve ATA: %w", err)
	}
	accts.AssociatedBondingCurve = assocBC

	return accts, nil
}

// ========================================
// PumpCreateV2 - Token-2022 创建代币
// ========================================

// MayhemProgramID is the Mayhem program address used in create_v2.
var MayhemProgramID = solana.MustPublicKeyFromBase58("MAyhSmzXzV1pTf7LsNkrNwkWKTo4ougAJ1PPg47MD4e")

// PumpCreateV2 creates a new Token-2022 token on Pump.fun bonding curve.
//
// This function uses the 'create_v2' instruction which creates Token-2022 tokens.
// For SPL Token, use PumpCreate instead.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: creator's public key (fee payer and signer)
//   - name: token name
//   - symbol: token symbol
//   - uri: metadata URI
//   - isMayhemMode: enable mayhem mode (typically false)
//   - opts: optional configurations (WithVanitySuffix, WithVanityPrefix, etc.)
//
// Returns:
//   - CreateV2Accounts: auto-filled account addresses
//   - CreateV2Args: instruction arguments
//   - solana.Instruction: the create_v2 instruction
//   - solana.PrivateKey: the generated mint keypair (must be added as signer)
//   - error: validation or RPC errors
func PumpCreateV2(ctx context.Context, rpc *sdkrpc.Client, user solana.PublicKey, name, symbol, uri string, isMayhemMode bool, opts ...Option) (pump.CreateV2Accounts, pump.CreateV2Args, solana.Instruction, solana.PrivateKey, error) {
	if rpc == nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, nil, err
	}
	if name == "" {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, nil, types.NewValidationError("name", "cannot be empty")
	}
	if symbol == "" {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, nil, types.NewValidationError("symbol", "cannot be empty")
	}
	if uri == "" {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, nil, types.NewValidationError("uri", "cannot be empty")
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	// Generate mint keypair (with optional vanity address)
	mintKey, err := generateMintKey(ctx, options)
	if err != nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, nil, err
	}
	mint := mintKey.PublicKey()

	// Auto-fill accounts
	accts, err := pumpAutofillCreateV2(ctx, rpc, user, mint)
	if err != nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, nil, err
	}
	applyOverrides(&accts, options.Overrides)

	args := pump.CreateV2Args{
		Name:         name,
		Symbol:       symbol,
		Uri:          uri,
		Creator:      user,
		IsMayhemMode: isMayhemMode,
	}

	ix, err := pump.BuildCreateV2(accts, args)
	if err != nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, nil, err
	}

	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pump.CreateV2Accounts `json:"accounts"`
			Args     pump.CreateV2Args     `json:"args"`
			Mint     string                `json:"mint"`
		}{accts, args, mint.String()})
	}

	return accts, args, ix, mintKey, nil
}

// PumpCreateV2WithMint creates a Token-2022 token with a pre-generated mint keypair.
func PumpCreateV2WithMint(ctx context.Context, rpc *sdkrpc.Client, user solana.PublicKey, mintKey solana.PrivateKey, name, symbol, uri string, isMayhemMode bool, opts ...Option) (pump.CreateV2Accounts, pump.CreateV2Args, solana.Instruction, error) {
	if rpc == nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, err
	}
	if mintKey == nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, types.NewValidationError("mintKey", "cannot be nil")
	}
	if name == "" {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, types.NewValidationError("name", "cannot be empty")
	}
	if symbol == "" {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, types.NewValidationError("symbol", "cannot be empty")
	}
	if uri == "" {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, types.NewValidationError("uri", "cannot be empty")
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	mint := mintKey.PublicKey()
	accts, err := pumpAutofillCreateV2(ctx, rpc, user, mint)
	if err != nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, err
	}
	applyOverrides(&accts, options.Overrides)

	args := pump.CreateV2Args{
		Name:         name,
		Symbol:       symbol,
		Uri:          uri,
		Creator:      user,
		IsMayhemMode: isMayhemMode,
	}

	ix, err := pump.BuildCreateV2(accts, args)
	if err != nil {
		return pump.CreateV2Accounts{}, pump.CreateV2Args{}, nil, err
	}

	return accts, args, ix, nil
}

// pumpAutofillCreateV2 auto-fills accounts for create_v2 instruction (Token-2022).
func pumpAutofillCreateV2(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey) (pump.CreateV2Accounts, error) {
	accts := pump.CreateV2Accounts{
		Mint:                   mint,
		User:                   user,
		SystemProgram:          constants.SystemProgramID,
		TokenProgram:           constants.Token2022ProgramID, // Token-2022
		AssociatedTokenProgram: constants.AssociatedTokenProgramID,
		MayhemProgramId:        MayhemProgramID,
		Program:                pump.ProgramKey,
	}

	// Derive MintAuthority PDA
	if pk, _, err := pump.DeriveCreateV2MintAuthorityPDA(accts, pump.CreateV2Args{}); err == nil {
		accts.MintAuthority = pk
	}

	// Derive BondingCurve PDA
	if pk, _, err := pump.DeriveCreateV2BondingCurvePDA(accts, pump.CreateV2Args{}); err == nil {
		accts.BondingCurve = pk
	}

	// Derive Global PDA
	if pk, _, err := pump.DeriveCreateV2GlobalPDA(accts, pump.CreateV2Args{}); err == nil {
		accts.Global = pk
	}

	// Derive EventAuthority PDA
	if pk, _, err := pump.DeriveCreateV2EventAuthorityPDA(accts, pump.CreateV2Args{}); err == nil {
		accts.EventAuthority = pk
	}

	// Derive GlobalParams PDA (from Mayhem program)
	if pk, _, err := pump.DeriveCreateV2GlobalParamsPDA(accts, pump.CreateV2Args{}); err == nil {
		accts.GlobalParams = pk
	}

	// Derive SolVault PDA (from Mayhem program)
	if pk, _, err := pump.DeriveCreateV2SolVaultPDA(accts, pump.CreateV2Args{}); err == nil {
		accts.SolVault = pk
	}

	// Derive MayhemState PDA
	if pk, _, err := pump.DeriveCreateV2MayhemStatePDA(accts, pump.CreateV2Args{}); err == nil {
		accts.MayhemState = pk
	}

	// Derive MayhemTokenVault PDA
	if pk, _, err := pump.DeriveCreateV2MayhemTokenVaultPDA(accts, pump.CreateV2Args{}); err == nil {
		accts.MayhemTokenVault = pk
	}

	// Derive AssociatedBondingCurve (ATA using Token-2022)
	assocBC, _, err := findATAWithProgram(accts.BondingCurve, mint, constants.Token2022ProgramID, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive bonding curve ATA: %w", err)
	}
	accts.AssociatedBondingCurve = assocBC

	return accts, nil
}

// appendJitoTipPump appends a Jito tip transfer instruction if configured.
func appendJitoTipPump(instrs []solana.Instruction, from solana.PublicKey, options *Options) []solana.Instruction {
	if options == nil || options.JitoTipLamports == 0 {
		return instrs
	}
	tipAccount := options.JitoTipAccount
	if tipAccount.IsZero() {
		tipAccount = jito.GetRandomTipAccountLocal()
	}
	tipIx := system.NewTransferInstruction(options.JitoTipLamports, from, tipAccount).Build()
	return append(instrs, tipIx)
}
