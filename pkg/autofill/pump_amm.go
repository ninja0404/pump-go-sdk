package autofill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/types"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// PumpAmmBuyWithSol buys tokens on Pump AMM with automatic slippage calculation.
//
// This is the recommended high-level function for AMM buys. It:
//   - Auto-wraps SOL to WSOL (only wraps the differential amount needed)
//   - Simulates the swap to estimate token output
//   - Applies slippage to calculate minimum acceptable tokens
//   - Unwraps any remaining WSOL back to SOL after the swap
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: buyer's public key
//   - pool: AMM pool address
//   - quoteLamports: SOL amount to spend (lamports)
//   - slippageBps: slippage tolerance in basis points (100 = 1%, max 10000)
//   - opts: optional configurations
//
// Returns:
//   - BuyExactQuoteInAccounts: auto-filled accounts
//   - BuyExactQuoteInArgs: instruction args with calculated minBaseOut
//   - []Instruction: complete instruction set
//   - uint64: simulated base token output (before slippage)
//   - error: validation or execution errors
//
// Example:
//
//	// Buy tokens with 0.01 SOL and 1% slippage
//	accts, args, instrs, simOut, err := autofill.PumpAmmBuyWithSol(ctx, rpc, user, pool, 10_000_000, 100)
func PumpAmmBuyWithSol(
	ctx context.Context,
	rpc *sdkrpc.Client,
	user, pool solana.PublicKey,
	quoteLamports uint64,
	slippageBps uint64,
	opts ...Option,
) (pumpamm.BuyExactQuoteInAccounts, pumpamm.BuyExactQuoteInArgs, []solana.Instruction, uint64, error) {
	// Input validation
	if rpc == nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, err
	}
	if err := types.ValidatePublicKey("pool", pool); err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, err
	}
	if quoteLamports == 0 {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, types.NewValidationError("quoteLamports", "must be greater than 0")
	}
	if err := types.ValidateSlippage(slippageBps); err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, err
	}

	options := &Options{TrackVolume: true}
	for _, opt := range opts {
		opt(options)
	}

	buyAccts, err := pumpAmmAutofillBuy(ctx, rpc, user, pool)
	if err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, err
	}
	exactAccts := toBuyExactAccounts(buyAccts)
	applyOverrides(&exactAccts, options.Overrides)

	// 批量检查 ATA 是否存在（同时获取余额）
	ataReqs := []ataRequest{
		{Payer: exactAccts.User, Wallet: exactAccts.User, Mint: exactAccts.BaseMint, TokenProgram: exactAccts.BaseTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: exactAccts.User, Wallet: exactAccts.User, Mint: exactAccts.QuoteMint, TokenProgram: exactAccts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: exactAccts.User, Wallet: exactAccts.ProtocolFeeRecipient, Mint: exactAccts.QuoteMint, TokenProgram: exactAccts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: exactAccts.User, Wallet: exactAccts.CoinCreatorVaultAuthority, Mint: exactAccts.QuoteMint, TokenProgram: exactAccts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
	}
	ataResult, err := ensureATABatchWithBalances(ctx, rpc, ataReqs)
	if err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, err
	}
	instrs := ataResult.Instructions
	existingQuote := ataResult.Balances[exactAccts.UserQuoteTokenAccount.String()]
	initialBase := ataResult.Balances[exactAccts.UserBaseTokenAccount.String()]

	// 自动 wrap SOL -> WSOL，仅补足差额
	if isWSOL(exactAccts.QuoteMint, exactAccts.QuoteTokenProgram) && quoteLamports > existingQuote {
		instrs = append(instrs, buildWrapWSOL(exactAccts.User, exactAccts.UserQuoteTokenAccount, quoteLamports-existingQuote)...)
	}

	// 先模拟：用 min_base=1 估算 base_out
	simArgs := pumpamm.BuyExactQuoteInArgs{
		SpendableQuoteIn: quoteLamports,
		MinBaseAmountOut: 1,
		TrackVolume:      pumpamm.OptionBool{Field0: options.TrackVolume},
	}
	simIx, err := pumpamm.BuildBuyExactQuoteIn(exactAccts, simArgs)
	if err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, err
	}
	baseOutSim, err := simulateBaseOut(ctx, rpc, user, exactAccts.UserBaseTokenAccount, initialBase, append(instrs, simIx)...)
	if err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, err
	}

	minBaseOut := applySlippage(baseOutSim, slippageBps)
	finalArgs := pumpamm.BuyExactQuoteInArgs{
		SpendableQuoteIn: quoteLamports,
		MinBaseAmountOut: minBaseOut,
		TrackVolume:      pumpamm.OptionBool{Field0: options.TrackVolume},
	}
	finalIx, err := pumpamm.BuildBuyExactQuoteIn(exactAccts, finalArgs)
	if err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, 0, err
	}
	instrs = append(instrs, finalIx)
	// Auto unwrap if user receives WSOL
	if exactAccts.BaseMint == constants.WSOLMint {
		instrs = append(instrs, buildCloseAccount(exactAccts.UserBaseTokenAccount, exactAccts.User, exactAccts.User, exactAccts.BaseTokenProgram))
	}
	// Append Jito tip if configured
	instrs = appendJitoTip(instrs, user, options)
	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts      pumpamm.BuyExactQuoteInAccounts `json:"accounts"`
			Args          pumpamm.BuyExactQuoteInArgs     `json:"args"`
			SimulatedBase uint64                          `json:"simulated_base_out"`
		}{exactAccts, finalArgs, baseOutSim})
	}
	return exactAccts, finalArgs, instrs, baseOutSim, nil
}

// PumpAmmBuyExactQuoteIn buys tokens with exact quote input and user-specified minimum output.
//
// Unlike PumpAmmBuyWithSol, this function does NOT simulate - you must provide
// the minBaseOut value yourself (e.g., from your own price calculation).
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: buyer's public key
//   - pool: AMM pool address
//   - quoteLamports: exact SOL to spend (lamports)
//   - minBaseOut: minimum tokens to receive (your slippage protection)
//   - opts: optional configurations
//
// Returns accounts, args, instructions, and any error.
func PumpAmmBuyExactQuoteIn(
	ctx context.Context,
	rpc *sdkrpc.Client,
	user, pool solana.PublicKey,
	quoteLamports uint64,
	minBaseOut uint64,
	opts ...Option,
) (pumpamm.BuyExactQuoteInAccounts, pumpamm.BuyExactQuoteInArgs, []solana.Instruction, error) {
	// Input validation
	if rpc == nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, err
	}
	if err := types.ValidatePublicKey("pool", pool); err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, err
	}
	if quoteLamports == 0 {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, types.NewValidationError("quoteLamports", "must be greater than 0")
	}

	options := &Options{TrackVolume: true}
	for _, opt := range opts {
		opt(options)
	}

	buyAccts, err := pumpAmmAutofillBuy(ctx, rpc, user, pool)
	if err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, err
	}
	exactAccts := toBuyExactAccounts(buyAccts)
	applyOverrides(&exactAccts, options.Overrides)

	// 批量检查 ATA 是否存在（同时获取余额）
	ataReqs := []ataRequest{
		{Payer: exactAccts.User, Wallet: exactAccts.User, Mint: exactAccts.BaseMint, TokenProgram: exactAccts.BaseTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: exactAccts.User, Wallet: exactAccts.User, Mint: exactAccts.QuoteMint, TokenProgram: exactAccts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: exactAccts.User, Wallet: exactAccts.ProtocolFeeRecipient, Mint: exactAccts.QuoteMint, TokenProgram: exactAccts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: exactAccts.User, Wallet: exactAccts.CoinCreatorVaultAuthority, Mint: exactAccts.QuoteMint, TokenProgram: exactAccts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
	}
	ataResult, err := ensureATABatchWithBalances(ctx, rpc, ataReqs)
	if err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, err
	}
	instrs := ataResult.Instructions

	// 自动 wrap SOL -> WSOL，仅补足差额（使用批量查询的余额）
	if isWSOL(exactAccts.QuoteMint, exactAccts.QuoteTokenProgram) && quoteLamports > 0 {
		existing := ataResult.Balances[exactAccts.UserQuoteTokenAccount.String()]
		if quoteLamports > existing {
			wrapLamports := quoteLamports - existing
			instrs = append(instrs, buildWrapWSOL(exactAccts.User, exactAccts.UserQuoteTokenAccount, wrapLamports)...)
		}
	}

	args := pumpamm.BuyExactQuoteInArgs{
		SpendableQuoteIn: quoteLamports,
		MinBaseAmountOut: minBaseOut,
		TrackVolume:      pumpamm.OptionBool{Field0: options.TrackVolume},
	}
	ix, err := pumpamm.BuildBuyExactQuoteIn(exactAccts, args)
	if err != nil {
		return pumpamm.BuyExactQuoteInAccounts{}, pumpamm.BuyExactQuoteInArgs{}, nil, err
	}
	instrs = append(instrs, ix)
	// Auto unwrap if user receives WSOL
	if exactAccts.BaseMint == constants.WSOLMint {
		instrs = append(instrs, buildCloseAccount(exactAccts.UserBaseTokenAccount, exactAccts.User, exactAccts.User, exactAccts.BaseTokenProgram))
	}
	// Append Jito tip if configured
	instrs = appendJitoTip(instrs, user, options)

	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pumpamm.BuyExactQuoteInAccounts `json:"accounts"`
			Args     pumpamm.BuyExactQuoteInArgs     `json:"args"`
		}{exactAccts, args})
	}
	return exactAccts, args, instrs, nil
}

// PumpAmmBuy constructs an AMM buy instruction with specified output amount.
//
// This function simulates the transaction (without signature) to determine the actual
// quote needed, then only wraps the required amount of SOL to WSOL.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: buyer's public key
//   - pool: AMM pool address
//   - baseOut: exact token amount to receive (base units)
//   - maxQuoteIn: maximum quote (SOL) to spend (lamports)
//   - opts: optional configurations
//
// Returns accounts, args, instructions, and any error.
func PumpAmmBuy(ctx context.Context, rpc *sdkrpc.Client, user, pool solana.PublicKey, baseOut, maxQuoteIn uint64, opts ...Option) (pumpamm.BuyAccounts, pumpamm.BuyArgs, []solana.Instruction, error) {
	// Input validation
	if rpc == nil {
		return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, err
	}
	if err := types.ValidatePublicKey("pool", pool); err != nil {
		return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, err
	}
	if err := types.ValidateBuyParams(baseOut, maxQuoteIn); err != nil {
		return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, err
	}

	options := &Options{TrackVolume: true}
	for _, opt := range opts {
		opt(options)
	}
	accts, err := pumpAmmAutofillBuy(ctx, rpc, user, pool)
	if err != nil {
		return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, err
	}
	applyOverrides(&accts, options.Overrides)

	args := pumpamm.BuyArgs{
		BaseAmountOut:    baseOut,
		MaxQuoteAmountIn: maxQuoteIn,
		TrackVolume:      pumpamm.OptionBool{Field0: options.TrackVolume},
	}

	// 批量检查 ATA 是否存在（同时获取余额）
	ataReqs := []ataRequest{
		{Payer: user, Wallet: user, Mint: accts.BaseMint, TokenProgram: accts.BaseTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: user, Wallet: user, Mint: accts.QuoteMint, TokenProgram: accts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: user, Wallet: accts.ProtocolFeeRecipient, Mint: accts.QuoteMint, TokenProgram: accts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
		{Payer: user, Wallet: accts.CoinCreatorVaultAuthority, Mint: accts.QuoteMint, TokenProgram: accts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID},
	}
	ataResult, err := ensureATABatchWithBalances(ctx, rpc, ataReqs)
	if err != nil {
		return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, err
	}
	ensureInstrs := ataResult.Instructions
	existingQuote := ataResult.Balances[accts.UserQuoteTokenAccount.String()]

	// 模拟交易以获取实际需要的 quote 数量（无需签名）
	var actualQuoteNeeded uint64
	if accts.QuoteMint == constants.WSOLMint && accts.QuoteTokenProgram == constants.TokenProgramID {
		simInstrs := append([]solana.Instruction{}, ensureInstrs...)
		if maxQuoteIn > existingQuote {
			simInstrs = append(simInstrs, buildWrapWSOL(user, accts.UserQuoteTokenAccount, maxQuoteIn-existingQuote)...)
		}
		simIx, err := pumpamm.BuildBuy(accts, args)
		if err != nil {
			return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, err
		}
		simInstrs = append(simInstrs, simIx)

		preBalance := existingQuote + (maxQuoteIn - existingQuote)
		quoteConsumed, err := simulateQuoteConsumedNoSign(ctx, rpc, user, accts.UserQuoteTokenAccount, preBalance, simInstrs...)
		if err != nil {
			return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, fmt.Errorf("simulate quote consumed: %w", err)
		}
		actualQuoteNeeded = quoteConsumed
	}

	// 构建最终指令：只 wrap 实际需要的金额
	var instrs []solana.Instruction
	instrs = append(instrs, ensureInstrs...)
	if accts.QuoteMint == constants.WSOLMint && accts.QuoteTokenProgram == constants.TokenProgramID && actualQuoteNeeded > 0 {
		if actualQuoteNeeded > existingQuote {
			wrapLamports := actualQuoteNeeded - existingQuote
			instrs = append(instrs, buildWrapWSOL(user, accts.UserQuoteTokenAccount, wrapLamports)...)
		}
	}

	ix, err := pumpamm.BuildBuy(accts, args)
	if err != nil {
		return pumpamm.BuyAccounts{}, pumpamm.BuyArgs{}, nil, err
	}
	instrs = append(instrs, ix)

	// Auto unwrap if user receives WSOL
	if accts.BaseMint == constants.WSOLMint {
		instrs = append(instrs, buildCloseAccount(accts.UserBaseTokenAccount, user, user, accts.BaseTokenProgram))
	}
	// Auto unwrap remaining WSOL after buy
	if accts.QuoteMint == constants.WSOLMint {
		instrs = append(instrs, buildCloseAccount(accts.UserQuoteTokenAccount, user, user, accts.QuoteTokenProgram))
	}
	// Append Jito tip if configured
	instrs = appendJitoTip(instrs, user, options)

	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pumpamm.BuyAccounts `json:"accounts"`
			Args     pumpamm.BuyArgs     `json:"args"`
		}{accts, args})
	}
	return accts, args, instrs, nil
}

// PumpAmmSell constructs an AMM sell instruction.
//
// This is the low-level sell function. For automatic slippage calculation
// and ATA management, prefer PumpAmmSellWithSlippage.
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: seller's public key
//   - pool: AMM pool address
//   - baseIn: token amount to sell (base units)
//   - minQuoteOut: minimum quote (SOL) to receive (lamports)
//   - opts: optional configurations
//
// Returns accounts, args, single instruction, and any error.
func PumpAmmSell(ctx context.Context, rpc *sdkrpc.Client, user, pool solana.PublicKey, baseIn, minQuoteOut uint64, opts ...Option) (pumpamm.SellAccounts, pumpamm.SellArgs, solana.Instruction, error) {
	// Input validation
	if rpc == nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}
	if err := types.ValidatePublicKey("pool", pool); err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	accts, err := pumpAmmAutofillSell(ctx, rpc, user, pool)
	if err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}
	applyOverrides(&accts, options.Overrides)

	args := pumpamm.SellArgs{
		BaseAmountIn:      baseIn,
		MinQuoteAmountOut: minQuoteOut,
	}

	ix, err := pumpamm.BuildSell(accts, args)
	if err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}
	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pumpamm.SellAccounts `json:"accounts"`
			Args     pumpamm.SellArgs     `json:"args"`
		}{accts, args})
	}
	return accts, args, ix, nil
}

// PumpAmmSellWithSlippage sells tokens on Pump AMM with automatic slippage calculation.
//
// This is the recommended high-level function for AMM sells. It:
//   - Simulates the swap to estimate quote (SOL) output
//   - Applies slippage to calculate minimum acceptable quote
//   - Closes the base token ATA if selling all tokens (reclaims rent)
//   - Unwraps WSOL to SOL if quote is WSOL
//
// Parameters:
//   - ctx: context for RPC calls
//   - rpc: RPC client wrapper
//   - user: seller's public key
//   - pool: AMM pool address
//   - baseIn: token amount to sell (base units)
//   - slippageBps: slippage tolerance in basis points (100 = 1%, max 10000)
//   - opts: optional configurations
//
// Returns accounts, args, complete instruction set, and any error.
//
// Example:
//
//	// Sell 1M tokens with 1% slippage
//	accts, args, instrs, err := autofill.PumpAmmSellWithSlippage(ctx, rpc, user, pool, 1_000_000, 100)
func PumpAmmSellWithSlippage(ctx context.Context, rpc *sdkrpc.Client, user, pool solana.PublicKey, baseIn uint64, slippageBps uint64, opts ...Option) (pumpamm.SellAccounts, pumpamm.SellArgs, []solana.Instruction, error) {
	// Input validation
	if rpc == nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}
	if err := types.ValidatePublicKey("pool", pool); err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}
	if baseIn == 0 {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, types.NewValidationError("baseIn", "must be greater than 0")
	}
	if err := types.ValidateSlippage(slippageBps); err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	accts, _, errIx, err := PumpAmmSell(ctx, rpc, user, pool, baseIn, 0, opts...)
	if err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}

	// Build known ATA set for fast lookup
	knownATASet := make(map[string]bool, len(options.KnownATAs))
	for _, ata := range options.KnownATAs {
		knownATASet[ata.String()] = true
	}

	// Build ATA requests, skipping known ATAs
	var ataReqs []ataRequest
	if !knownATASet[accts.UserQuoteTokenAccount.String()] {
		ataReqs = append(ataReqs, ataRequest{Payer: user, Wallet: accts.User, Mint: accts.QuoteMint, TokenProgram: accts.QuoteTokenProgram, ATAProgram: constants.AssociatedTokenProgramID})
	}
	if !knownATASet[accts.UserBaseTokenAccount.String()] {
		ataReqs = append(ataReqs, ataRequest{Payer: user, Wallet: accts.User, Mint: accts.BaseMint, TokenProgram: accts.BaseTokenProgram, ATAProgram: constants.AssociatedTokenProgramID})
	}

	var ensureInstrs []solana.Instruction
	if len(ataReqs) > 0 {
		ataResult, err := ensureATABatchWithBalances(ctx, rpc, ataReqs)
		if err != nil {
			return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
		}
		ensureInstrs = ataResult.Instructions
	}

	// Get expected quote output (simulate or use provided value)
	var quoteOut uint64
	if options.ExpectedQuoteOut > 0 {
		// Skip simulation, use provided expected output
		quoteOut = options.ExpectedQuoteOut
	} else {
		// Simulate to get expected output
		var err error
		quoteOut, err = simulateAmmQuoteOut(ctx, rpc, user, accts, baseIn, append(ensureInstrs, errIx)...)
		if err != nil {
			return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
		}
	}
	minQuote := applySlippage(quoteOut, slippageBps)

	args := pumpamm.SellArgs{
		BaseAmountIn:      baseIn,
		MinQuoteAmountOut: minQuote,
	}
	ix, err := pumpamm.BuildSell(accts, args)
	if err != nil {
		return pumpamm.SellAccounts{}, pumpamm.SellArgs{}, nil, err
	}
	instrs := append(ensureInstrs, ix)

	// Close base ATA only if explicitly requested
	if options.CloseBaseATA {
		instrs = append(instrs, buildCloseAccount(accts.UserBaseTokenAccount, user, user, accts.BaseTokenProgram))
	}
	// Auto unwrap if user receives WSOL (sell base -> receive quote WSOL)
	if accts.QuoteMint == constants.WSOLMint {
		instrs = append(instrs, buildCloseAccount(accts.UserQuoteTokenAccount, user, user, accts.QuoteTokenProgram))
	}
	// Append Jito tip if configured
	instrs = appendJitoTip(instrs, user, options)

	if options.Preview != nil {
		_ = json.NewEncoder(options.Preview).Encode(struct {
			Accounts pumpamm.SellAccounts `json:"accounts"`
			Args     pumpamm.SellArgs     `json:"args"`
		}{accts, args})
	}
	return accts, args, instrs, nil
}

// BuildAndSendAmm executes the instruction with provided signer/txbuilder.
func BuildAndSendAmm(ctx context.Context, builder *txbuilder.Builder, signer wallet.Signer, ix solana.Instruction) (solana.Signature, error) {
	if builder == nil || signer == nil {
		return solana.Signature{}, fmt.Errorf("builder and signer are required")
	}
	return builder.BuildSignSend(ctx, signer, nil, ix)
}

// BuildAndSimulateAmm simulates the instruction without signature.
func BuildAndSimulateAmm(ctx context.Context, rpc *sdkrpc.Client, builder *txbuilder.Builder, user solana.PublicKey, ix solana.Instruction) (*solanarpc.SimulateTransactionResponse, error) {
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

// --- internal helpers ---

func pumpAmmAutofillBuy(ctx context.Context, rpc *sdkrpc.Client, user, pool solana.PublicKey) (pumpamm.BuyAccounts, error) {
	var accts pumpamm.BuyAccounts

	globalConfig, err := deriveAmmGlobalConfigPDA()
	if err != nil {
		return accts, fmt.Errorf("derive global_config PDA: %w", err)
	}

	core, err := fetchAmmCore(ctx, rpc, pool, globalConfig)
	if err != nil {
		return accts, fmt.Errorf("fetch amm core for pool %s: %w", pool, err)
	}
	protocolRecipient := firstNonZeroPK(core.GlobalConfig.ProtocolFeeRecipients[:])
	if isZeroPK(protocolRecipient) {
		return accts, fmt.Errorf("protocol fee recipient not found in global_config for pool %s", pool)
	}

	userBaseATA, _, err := findATAWithProgram(user, core.Pool.BaseMint, core.BaseTokenProgram, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive user base ATA for mint %s: %w", core.Pool.BaseMint, err)
	}
	userQuoteATA, _, err := findATAWithProgram(user, core.Pool.QuoteMint, core.QuoteTokenProgram, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive user quote ATA for mint %s: %w", core.Pool.QuoteMint, err)
	}

	accts = pumpamm.BuyAccounts{
		Pool:                      pool,
		User:                      user,
		GlobalConfig:              globalConfig,
		BaseMint:                  core.Pool.BaseMint,
		QuoteMint:                 core.Pool.QuoteMint,
		UserBaseTokenAccount:      userBaseATA,
		UserQuoteTokenAccount:     userQuoteATA,
		PoolBaseTokenAccount:      core.Pool.PoolBaseTokenAccount,
		PoolQuoteTokenAccount:     core.Pool.PoolQuoteTokenAccount,
		ProtocolFeeRecipient:      protocolRecipient,
		BaseTokenProgram:          core.BaseTokenProgram,
		QuoteTokenProgram:         core.QuoteTokenProgram,
		SystemProgram:             constants.SystemProgramID,
		AssociatedTokenProgram:    constants.AssociatedTokenProgramID,
		Program:                   pumpamm.ProgramKey,
		FeeProgram:                constants.PumpAmmFeeProgramID,
		CoinCreatorVaultAuthority: pumpamm.ProgramKey, // placeholder; may be overwritten below
	}

	if pk, _, err := pumpamm.DeriveBuyEventAuthorityPDA(accts, pumpamm.BuyArgs{}); err == nil {
		accts.EventAuthority = pk
	}
	if pk, _, err := pumpamm.DeriveBuyGlobalVolumeAccumulatorPDA(accts, pumpamm.BuyArgs{}); err == nil {
		accts.GlobalVolumeAccumulator = pk
	}
	if pk, _, err := pumpamm.DeriveBuyUserVolumeAccumulatorPDA(accts, pumpamm.BuyArgs{}); err == nil {
		accts.UserVolumeAccumulator = pk
	}
	if pk, _, err := pumpamm.DeriveBuyFeeConfigPDA(accts, pumpamm.BuyArgs{}); err == nil {
		accts.FeeConfig = pk
	}
	// coin_creator_vault_authority should be derived with core.Pool.CoinCreator
	if pk, _, err := solana.FindProgramAddress([][]byte{[]byte(constants.SeedCreatorVaultAmm), core.Pool.CoinCreator[:]}, pumpamm.ProgramKey); err == nil {
		accts.CoinCreatorVaultAuthority = pk
		if pk2, _, err := pumpamm.DeriveBuyCoinCreatorVaultAtaPDA(accts, pumpamm.BuyArgs{}); err == nil {
			accts.CoinCreatorVaultAta = pk2
		}
	}
	if pk, _, err := pumpamm.DeriveBuyProtocolFeeRecipientTokenAccountPDA(accts, pumpamm.BuyArgs{}); err == nil {
		accts.ProtocolFeeRecipientTokenAccount = pk
	}

	return accts, nil
}

func pumpAmmAutofillSell(ctx context.Context, rpc *sdkrpc.Client, user, pool solana.PublicKey) (pumpamm.SellAccounts, error) {
	var accts pumpamm.SellAccounts

	globalConfig, err := deriveAmmGlobalConfigPDA()
	if err != nil {
		return accts, err
	}

	core, err := fetchAmmCore(ctx, rpc, pool, globalConfig)
	if err != nil {
		return accts, fmt.Errorf("fetch amm core for pool %s: %w", pool, err)
	}
	protocolRecipient := firstNonZeroPK(core.GlobalConfig.ProtocolFeeRecipients[:])
	if isZeroPK(protocolRecipient) {
		return accts, fmt.Errorf("protocol fee recipient not found in global_config for pool %s", pool)
	}

	userBaseATA, _, err := findATAWithProgram(user, core.Pool.BaseMint, core.BaseTokenProgram, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive user base ATA for mint %s: %w", core.Pool.BaseMint, err)
	}
	userQuoteATA, _, err := findATAWithProgram(user, core.Pool.QuoteMint, core.QuoteTokenProgram, constants.AssociatedTokenProgramID)
	if err != nil {
		return accts, fmt.Errorf("derive user quote ATA for mint %s: %w", core.Pool.QuoteMint, err)
	}

	accts = pumpamm.SellAccounts{
		Pool:                   pool,
		User:                   user,
		GlobalConfig:           globalConfig,
		BaseMint:               core.Pool.BaseMint,
		QuoteMint:              core.Pool.QuoteMint,
		UserBaseTokenAccount:   userBaseATA,
		UserQuoteTokenAccount:  userQuoteATA,
		PoolBaseTokenAccount:   core.Pool.PoolBaseTokenAccount,
		PoolQuoteTokenAccount:  core.Pool.PoolQuoteTokenAccount,
		ProtocolFeeRecipient:   protocolRecipient,
		BaseTokenProgram:       core.BaseTokenProgram,
		QuoteTokenProgram:      core.QuoteTokenProgram,
		SystemProgram:          constants.SystemProgramID,
		AssociatedTokenProgram: constants.AssociatedTokenProgramID,
		Program:                pumpamm.ProgramKey,
		FeeProgram:             constants.PumpAmmFeeProgramID,
	}

	if pk, _, err := pumpamm.DeriveSellEventAuthorityPDA(accts, pumpamm.SellArgs{}); err == nil {
		accts.EventAuthority = pk
	}
	if pk, _, err := pumpamm.DeriveSellFeeConfigPDA(accts, pumpamm.SellArgs{}); err == nil {
		accts.FeeConfig = pk
	}
	if pk, _, err := pumpamm.DeriveSellProtocolFeeRecipientTokenAccountPDA(accts, pumpamm.SellArgs{}); err == nil {
		accts.ProtocolFeeRecipientTokenAccount = pk
	}

	if pk, _, err := solana.FindProgramAddress([][]byte{[]byte(constants.SeedCreatorVaultAmm), core.Pool.CoinCreator[:]}, pumpamm.ProgramKey); err == nil {
		accts.CoinCreatorVaultAuthority = pk
		if pk2, _, err := pumpamm.DeriveSellCoinCreatorVaultAtaPDA(accts, pumpamm.SellArgs{}); err == nil {
			accts.CoinCreatorVaultAta = pk2
		}
	}
	return accts, nil
}

func deriveAmmGlobalConfigPDA() (solana.PublicKey, error) {
	pk, _, err := solana.FindProgramAddress([][]byte{[]byte(constants.SeedGlobalConfig)}, pumpamm.ProgramKey)
	return pk, err
}

// ammCoreResult holds decoded pool state and token program info.
type ammCoreResult struct {
	Pool              pumpamm.Pool
	GlobalConfig      pumpamm.GlobalConfig
	BaseTokenProgram  solana.PublicKey
	QuoteTokenProgram solana.PublicKey
}

// fetchAmmCore 批量获取 pool/global_config 并解码，减少 RPC。
// 同时获取 baseMint 和 quoteMint 的 owner（token program）。
func fetchAmmCore(ctx context.Context, rpc *sdkrpc.Client, pool, globalConfig solana.PublicKey) (ammCoreResult, error) {
	var result ammCoreResult
	result.BaseTokenProgram = constants.TokenProgramID
	result.QuoteTokenProgram = constants.TokenProgramID

	// 批量查询：pool, global_config
	amap, err := fetchAccountsBatch(ctx, rpc, pool, globalConfig)
	if err != nil {
		return result, err
	}

	poolAcc := amap[pool.String()]
	if poolAcc == nil || poolAcc.Data == nil {
		return result, fmt.Errorf("pool account %s not found (may be invalid pool address or RPC issue)", pool)
	}
	if err := result.Pool.Unmarshal(poolAcc.Data.GetBinary()); err != nil {
		return result, fmt.Errorf("decode pool %s: %w", pool, err)
	}

	globalAcc := amap[globalConfig.String()]
	if globalAcc == nil || globalAcc.Data == nil {
		return result, fmt.Errorf("global_config account %s not found", globalConfig)
	}
	if err := result.GlobalConfig.Unmarshal(globalAcc.Data.GetBinary()); err != nil {
		return result, fmt.Errorf("decode global_config %s: %w", globalConfig, err)
	}

	// 第二次批量查询：baseMint 和 quoteMint 的 owner
	mintAddrs := []solana.PublicKey{result.Pool.BaseMint, result.Pool.QuoteMint}
	mintMap, err := fetchAccountsBatch(ctx, rpc, mintAddrs...)
	if err != nil {
		return result, err
	}
	if acc := mintMap[result.Pool.BaseMint.String()]; acc != nil {
		result.BaseTokenProgram = acc.Owner
	}
	if acc := mintMap[result.Pool.QuoteMint.String()]; acc != nil {
		result.QuoteTokenProgram = acc.Owner
	}

	return result, nil
}

// --- helpers ---

func toBuyExactAccounts(a pumpamm.BuyAccounts) pumpamm.BuyExactQuoteInAccounts {
	return pumpamm.BuyExactQuoteInAccounts{
		Pool:                             a.Pool,
		User:                             a.User,
		GlobalConfig:                     a.GlobalConfig,
		BaseMint:                         a.BaseMint,
		QuoteMint:                        a.QuoteMint,
		UserBaseTokenAccount:             a.UserBaseTokenAccount,
		UserQuoteTokenAccount:            a.UserQuoteTokenAccount,
		PoolBaseTokenAccount:             a.PoolBaseTokenAccount,
		PoolQuoteTokenAccount:            a.PoolQuoteTokenAccount,
		ProtocolFeeRecipient:             a.ProtocolFeeRecipient,
		ProtocolFeeRecipientTokenAccount: a.ProtocolFeeRecipientTokenAccount,
		BaseTokenProgram:                 a.BaseTokenProgram,
		QuoteTokenProgram:                a.QuoteTokenProgram,
		SystemProgram:                    a.SystemProgram,
		AssociatedTokenProgram:           a.AssociatedTokenProgram,
		EventAuthority:                   a.EventAuthority,
		Program:                          a.Program,
		CoinCreatorVaultAta:              a.CoinCreatorVaultAta,
		CoinCreatorVaultAuthority:        a.CoinCreatorVaultAuthority,
		GlobalVolumeAccumulator:          a.GlobalVolumeAccumulator,
		UserVolumeAccumulator:            a.UserVolumeAccumulator,
		FeeConfig:                        a.FeeConfig,
		FeeProgram:                       a.FeeProgram,
	}
}

func isWSOL(mint, tokenProgram solana.PublicKey) bool {
	return mint == constants.WSOLMint && tokenProgram == constants.TokenProgramID
}

func applySlippage(amount uint64, slippageBps uint64) uint64 {
	if slippageBps >= 10_000 {
		return 0
	}
	return amount * (10_000 - slippageBps) / 10_000
}

func fetchTokenAmount(ctx context.Context, rpc *sdkrpc.Client, account solana.PublicKey) (uint64, error) {
	info, err := rpc.Raw().GetAccountInfo(ctx, account)
	if err != nil {
		// RPC error might indicate account doesn't exist, return 0 instead of error
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "could not find") {
			return 0, nil
		}
		return 0, fmt.Errorf("fetch token account %s: %w", account, err)
	}
	if info == nil || info.Value == nil || info.Value.Data == nil {
		// Account doesn't exist yet, return 0
		return 0, nil
	}
	data := info.Value.Data.GetBinary()
	if len(data) == 0 {
		// Account exists but empty, return 0
		return 0, nil
	}
	dec := bin.NewBinDecoder(data)
	var acc token.Account
	if err := dec.Decode(&acc); err != nil {
		return 0, fmt.Errorf("decode token account %s: %w", account, err)
	}
	return acc.Amount, nil
}

func simulateBaseOut(ctx context.Context, rpc *sdkrpc.Client, user, baseATA solana.PublicKey, initialBase uint64, instrs ...solana.Instruction) (uint64, error) {
	if len(instrs) == 0 {
		return 0, fmt.Errorf("no instructions to simulate")
	}
	builder := txbuilder.NewBuilder(rpc, solanarpc.CommitmentConfirmed)
	tx, err := builder.BuildTransaction(ctx, user, instrs...)
	if err != nil {
		return 0, fmt.Errorf("build tx for simulate: %w", err)
	}
	res, err := rpc.SimulateTransaction(ctx, tx, &solanarpc.SimulateTransactionOpts{
		SigVerify: false,
		Accounts: &solanarpc.SimulateTransactionAccountsOpts{
			Encoding:  solana.EncodingBase64,
			Addresses: []solana.PublicKey{baseATA},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("simulate tx: %w", err)
	}
	if res == nil || res.Value == nil {
		return 0, fmt.Errorf("simulate tx: empty result")
	}
	if res.Value.Err != nil {
		return 0, types.ParseSimulationError(res.Value.Err, res.Value.Logs)
	}
	if len(res.Value.Accounts) == 0 || res.Value.Accounts[0] == nil || res.Value.Accounts[0].Data == nil {
		return 0, fmt.Errorf("simulate tx: missing account data for %s", baseATA)
	}
	data := res.Value.Accounts[0].Data.GetBinary()
	if len(data) == 0 {
		return 0, fmt.Errorf("simulate tx: account %s empty data", baseATA)
	}
	dec := bin.NewBinDecoder(data)
	var acc token.Account
	if err := dec.Decode(&acc); err != nil {
		return 0, fmt.Errorf("simulate tx: decode token account %s: %w", baseATA, err)
	}
	if acc.Amount < initialBase {
		return 0, fmt.Errorf("simulate tx: base token decreased (%d -> %d)", initialBase, acc.Amount)
	}
	return acc.Amount - initialBase, nil
}

// simulateAmmQuoteOut 返回用户 quote ATA 增量（卖出 base -> quote）。
func simulateAmmQuoteOut(ctx context.Context, rpc *sdkrpc.Client, user solana.PublicKey, accounts pumpamm.SellAccounts, baseIn uint64, instrs ...solana.Instruction) (uint64, error) {
	pre, err := fetchTokenAmount(ctx, rpc, accounts.UserQuoteTokenAccount)
	if err != nil {
		return 0, err
	}
	if len(instrs) == 0 {
		ix, err := pumpamm.BuildSell(accounts, pumpamm.SellArgs{
			BaseAmountIn:      baseIn,
			MinQuoteAmountOut: 0,
		})
		if err != nil {
			return 0, err
		}
		instrs = append(instrs, ix)
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
			Addresses: []solana.PublicKey{accounts.UserQuoteTokenAccount},
		},
	})
	if err != nil {
		return 0, err
	}
	if res == nil || res.Value == nil {
		return 0, fmt.Errorf("simulate result empty response")
	}
	if res.Value.Err != nil {
		return 0, types.ParseSimulationError(res.Value.Err, res.Value.Logs)
	}
	if len(res.Value.Accounts) == 0 || res.Value.Accounts[0] == nil || res.Value.Accounts[0].Data == nil {
		return 0, fmt.Errorf("simulate result empty (quote ATA missing?), logs: %v", res.Value.Logs)
	}
	data := res.Value.Accounts[0].Data.GetBinary()
	if len(data) == 0 {
		return 0, fmt.Errorf("simulate result empty data")
	}
	dec := bin.NewBinDecoder(data)
	var acc token.Account
	if err := dec.Decode(&acc); err != nil {
		return 0, err
	}
	if acc.Amount < pre {
		return 0, fmt.Errorf("simulate: quote decreased %d -> %d", pre, acc.Amount)
	}
	return acc.Amount - pre, nil
}

// simulateQuoteConsumedNoSign simulates a buy transaction without signature to get actual quote consumed.
func simulateQuoteConsumedNoSign(ctx context.Context, rpc *sdkrpc.Client, user, quoteATA solana.PublicKey, preBalance uint64, instrs ...solana.Instruction) (uint64, error) {
	builder := txbuilder.NewBuilder(rpc, solanarpc.CommitmentConfirmed)
	tx, err := builder.BuildTransaction(ctx, user, instrs...)
	if err != nil {
		return 0, err
	}
	// No signing needed - use SigVerify: false
	res, err := rpc.SimulateTransaction(ctx, tx, &solanarpc.SimulateTransactionOpts{
		SigVerify: false, // Skip signature verification
		Accounts: &solanarpc.SimulateTransactionAccountsOpts{
			Encoding:  solana.EncodingBase64,
			Addresses: []solana.PublicKey{quoteATA},
		},
	})
	if err != nil {
		return 0, err
	}
	if res == nil || res.Value == nil {
		return 0, fmt.Errorf("simulate result empty response")
	}
	if res.Value.Err != nil {
		return 0, types.ParseSimulationError(res.Value.Err, res.Value.Logs)
	}
	if len(res.Value.Accounts) == 0 || res.Value.Accounts[0] == nil || res.Value.Accounts[0].Data == nil {
		return 0, fmt.Errorf("simulate result empty (quote ATA missing?), logs: %v", res.Value.Logs)
	}
	data := res.Value.Accounts[0].Data.GetBinary()
	if len(data) == 0 {
		return 0, fmt.Errorf("simulate result empty data")
	}
	dec := bin.NewBinDecoder(data)
	var acc token.Account
	if err := dec.Decode(&acc); err != nil {
		return 0, err
	}
	// Quote consumed = preBalance - postBalance
	if preBalance < acc.Amount {
		return 0, nil // No quote consumed (shouldn't happen)
	}
	return preBalance - acc.Amount, nil
}

// appendJitoTip appends a Jito tip transfer instruction if configured.
func appendJitoTip(instrs []solana.Instruction, from solana.PublicKey, options *Options) []solana.Instruction {
	if options == nil || options.JitoTipLamports == 0 {
		return instrs
	}
	tipAccount := options.JitoTipAccount
	if tipAccount.IsZero() {
		return instrs // No tip account configured
	}
	tipIx := system.NewTransferInstruction(options.JitoTipLamports, from, tipAccount).Build()
	return append(instrs, tipIx)
}
