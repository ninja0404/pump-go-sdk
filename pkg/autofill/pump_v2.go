package autofill

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gagliardetto/solana-go"

	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/types"
)

type pumpV2AccountSet struct {
	Global                             solana.PublicKey
	BaseMint                           solana.PublicKey
	QuoteMint                          solana.PublicKey
	BaseTokenProgram                   solana.PublicKey
	QuoteTokenProgram                  solana.PublicKey
	FeeRecipient                       solana.PublicKey
	AssociatedQuoteFeeRecipient        solana.PublicKey
	BuybackFeeRecipient                solana.PublicKey
	AssociatedQuoteBuybackFeeRecipient solana.PublicKey
	BondingCurve                       solana.PublicKey
	AssociatedBaseBondingCurve         solana.PublicKey
	AssociatedQuoteBondingCurve        solana.PublicKey
	User                               solana.PublicKey
	AssociatedBaseUser                 solana.PublicKey
	AssociatedQuoteUser                solana.PublicKey
	CreatorVault                       solana.PublicKey
	AssociatedCreatorVault             solana.PublicKey
	SharingConfig                      solana.PublicKey
	GlobalVolumeAccumulator            solana.PublicKey
	UserVolumeAccumulator              solana.PublicKey
	AssociatedUserVolumeAccumulator    solana.PublicKey
	FeeConfig                          solana.PublicKey
	EventAuthority                     solana.PublicKey
}

// PumpBuyV2 builds a current unified Pump buy for SOL or non-native quote mints.
func PumpBuyV2(
	ctx context.Context,
	rpc *sdkrpc.Client,
	user, mint solana.PublicKey,
	amount, maxQuoteCost uint64,
	opts ...Option,
) (pump.BuyV2Accounts, pump.BuyV2Args, []solana.Instruction, error) {
	if rpc == nil {
		return pump.BuyV2Accounts{}, pump.BuyV2Args{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.BuyV2Accounts{}, pump.BuyV2Args{}, nil, err
	}
	if err := types.ValidatePublicKey("mint", mint); err != nil {
		return pump.BuyV2Accounts{}, pump.BuyV2Args{}, nil, err
	}
	if err := types.ValidateBuyParams(amount, maxQuoteCost); err != nil {
		return pump.BuyV2Accounts{}, pump.BuyV2Args{}, nil, err
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	accountSet, err := fetchPumpV2AccountSet(ctx, rpc, user, mint)
	if err != nil {
		return pump.BuyV2Accounts{}, pump.BuyV2Args{}, nil, err
	}
	accounts := accountSet.buyAccounts()
	applyOverrides(&accounts, options.Overrides)
	args := pump.BuyV2Args{Amount: amount, MaxSolCost: maxQuoteCost}

	instructions, err := ensurePumpV2UserATAs(ctx, rpc, accountSet, true)
	if err != nil {
		return pump.BuyV2Accounts{}, pump.BuyV2Args{}, nil, err
	}
	instruction, err := pump.BuildBuyV2(accounts, args)
	if err != nil {
		return pump.BuyV2Accounts{}, pump.BuyV2Args{}, nil, err
	}
	instructions = finalizeInstructionsPump(append(instructions, instruction), user, options)
	writePumpV2Preview(options, accounts, args)
	return accounts, args, instructions, nil
}

// PumpBuyExactQuoteInV2 builds a current Pump buy with an exact quote input.
func PumpBuyExactQuoteInV2(
	ctx context.Context,
	rpc *sdkrpc.Client,
	user, mint solana.PublicKey,
	spendableQuoteIn, minTokensOut uint64,
	opts ...Option,
) (pump.BuyExactQuoteInV2Accounts, pump.BuyExactQuoteInV2Args, []solana.Instruction, error) {
	if rpc == nil {
		return pump.BuyExactQuoteInV2Accounts{}, pump.BuyExactQuoteInV2Args{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.BuyExactQuoteInV2Accounts{}, pump.BuyExactQuoteInV2Args{}, nil, err
	}
	if err := types.ValidatePublicKey("mint", mint); err != nil {
		return pump.BuyExactQuoteInV2Accounts{}, pump.BuyExactQuoteInV2Args{}, nil, err
	}
	if spendableQuoteIn == 0 {
		return pump.BuyExactQuoteInV2Accounts{}, pump.BuyExactQuoteInV2Args{}, nil, types.NewValidationError("spendableQuoteIn", "must be greater than 0")
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	accountSet, err := fetchPumpV2AccountSet(ctx, rpc, user, mint)
	if err != nil {
		return pump.BuyExactQuoteInV2Accounts{}, pump.BuyExactQuoteInV2Args{}, nil, err
	}
	accounts := accountSet.buyExactQuoteAccounts()
	applyOverrides(&accounts, options.Overrides)
	args := pump.BuyExactQuoteInV2Args{
		SpendableQuoteIn: spendableQuoteIn,
		MinTokensOut:     minTokensOut,
	}

	instructions, err := ensurePumpV2UserATAs(ctx, rpc, accountSet, true)
	if err != nil {
		return pump.BuyExactQuoteInV2Accounts{}, pump.BuyExactQuoteInV2Args{}, nil, err
	}
	instruction, err := pump.BuildBuyExactQuoteInV2(accounts, args)
	if err != nil {
		return pump.BuyExactQuoteInV2Accounts{}, pump.BuyExactQuoteInV2Args{}, nil, err
	}
	instructions = finalizeInstructionsPump(append(instructions, instruction), user, options)
	writePumpV2Preview(options, accounts, args)
	return accounts, args, instructions, nil
}

// PumpSellV2 builds a current unified Pump sell for SOL or non-native quote mints.
func PumpSellV2(
	ctx context.Context,
	rpc *sdkrpc.Client,
	user, mint solana.PublicKey,
	amount, minQuoteOutput uint64,
	opts ...Option,
) (pump.SellV2Accounts, pump.SellV2Args, []solana.Instruction, error) {
	if rpc == nil {
		return pump.SellV2Accounts{}, pump.SellV2Args{}, nil, types.ErrNilRPC
	}
	if err := types.ValidatePublicKey("user", user); err != nil {
		return pump.SellV2Accounts{}, pump.SellV2Args{}, nil, err
	}
	if err := types.ValidatePublicKey("mint", mint); err != nil {
		return pump.SellV2Accounts{}, pump.SellV2Args{}, nil, err
	}
	if amount == 0 {
		return pump.SellV2Accounts{}, pump.SellV2Args{}, nil, types.NewValidationError("amount", "must be greater than 0")
	}

	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	accountSet, err := fetchPumpV2AccountSet(ctx, rpc, user, mint)
	if err != nil {
		return pump.SellV2Accounts{}, pump.SellV2Args{}, nil, err
	}
	accounts := accountSet.sellAccounts()
	applyOverrides(&accounts, options.Overrides)
	args := pump.SellV2Args{Amount: amount, MinSolOutput: minQuoteOutput}

	instructions, err := ensurePumpV2UserATAs(ctx, rpc, accountSet, false)
	if err != nil {
		return pump.SellV2Accounts{}, pump.SellV2Args{}, nil, err
	}
	instruction, err := pump.BuildSellV2(accounts, args)
	if err != nil {
		return pump.SellV2Accounts{}, pump.SellV2Args{}, nil, err
	}
	instructions = finalizeInstructionsPump(append(instructions, instruction), user, options)
	writePumpV2Preview(options, accounts, args)
	return accounts, args, instructions, nil
}

func fetchPumpV2AccountSet(ctx context.Context, rpc *sdkrpc.Client, user, mint solana.PublicKey) (pumpV2AccountSet, error) {
	var result pumpV2AccountSet
	globalKey, err := derivePDA(pump.ProgramKey, []byte(constants.SeedGlobal))
	if err != nil {
		return result, fmt.Errorf("derive global: %w", err)
	}
	bondingCurveKey, err := derivePDA(pump.ProgramKey, []byte(constants.SeedBondingCurve), mint[:])
	if err != nil {
		return result, fmt.Errorf("derive bonding curve: %w", err)
	}
	accounts, err := fetchAccountsBatch(ctx, rpc, globalKey, bondingCurveKey, mint)
	if err != nil {
		return result, err
	}

	globalAccount := accounts[globalKey.String()]
	if globalAccount == nil || globalAccount.Data == nil {
		return result, fmt.Errorf("global account %s not found", globalKey)
	}
	if globalAccount.Owner != pump.ProgramKey {
		return result, fmt.Errorf("global account %s has unexpected owner %s", globalKey, globalAccount.Owner)
	}
	var global pump.Global
	if err := global.Unmarshal(globalAccount.Data.GetBinary()); err != nil {
		return result, fmt.Errorf("decode global %s: %w", globalKey, err)
	}

	bondingCurveAccount := accounts[bondingCurveKey.String()]
	if bondingCurveAccount == nil || bondingCurveAccount.Data == nil {
		return result, fmt.Errorf("bonding curve %s not found for mint %s", bondingCurveKey, mint)
	}
	if bondingCurveAccount.Owner != pump.ProgramKey {
		return result, fmt.Errorf("bonding curve %s has unexpected owner %s", bondingCurveKey, bondingCurveAccount.Owner)
	}
	var bondingCurve pump.BondingCurve
	if err := bondingCurve.Unmarshal(bondingCurveAccount.Data.GetBinary()); err != nil {
		return result, fmt.Errorf("decode bonding curve %s: %w", bondingCurveKey, err)
	}

	baseMintAccount := accounts[mint.String()]
	if baseMintAccount == nil {
		return result, fmt.Errorf("base mint account %s not found", mint)
	}
	baseTokenProgram := baseMintAccount.Owner
	if !isSupportedTokenProgram(baseTokenProgram) {
		return result, fmt.Errorf("base mint %s has unsupported token program %s", mint, baseTokenProgram)
	}

	quoteMint := pumpV2QuoteMint(bondingCurve.QuoteMint)
	quoteTokenProgram := constants.TokenProgramID
	if quoteMint != constants.WSOLMint {
		quoteAccounts, err := fetchAccountsBatch(ctx, rpc, quoteMint)
		if err != nil {
			return result, err
		}
		quoteMintAccount := quoteAccounts[quoteMint.String()]
		if quoteMintAccount == nil {
			return result, fmt.Errorf("quote mint account %s not found", quoteMint)
		}
		quoteTokenProgram = quoteMintAccount.Owner
		if !isSupportedTokenProgram(quoteTokenProgram) {
			return result, fmt.Errorf("quote mint %s has unsupported token program %s", quoteMint, quoteTokenProgram)
		}
	}

	return buildPumpV2AccountSet(user, mint, baseTokenProgram, quoteMint, quoteTokenProgram, global, bondingCurve)
}

func buildPumpV2AccountSet(
	user, mint, baseTokenProgram, quoteMint, quoteTokenProgram solana.PublicKey,
	global pump.Global,
	bondingCurve pump.BondingCurve,
) (pumpV2AccountSet, error) {
	feeRecipient, err := pumpFeeRecipient(global, bondingCurve.IsMayhemMode)
	if err != nil {
		return pumpV2AccountSet{}, err
	}
	buybackFeeRecipient, err := randomFeeRecipient("buyback fee recipient", global.BuybackFeeRecipients[:])
	if err != nil {
		return pumpV2AccountSet{}, err
	}
	accounts := pumpV2AccountSet{
		BaseMint:            mint,
		QuoteMint:           quoteMint,
		BaseTokenProgram:    baseTokenProgram,
		QuoteTokenProgram:   quoteTokenProgram,
		User:                user,
		FeeRecipient:        feeRecipient,
		BuybackFeeRecipient: buybackFeeRecipient,
	}

	if accounts.Global, err = derivePDA(pump.ProgramKey, []byte(constants.SeedGlobal)); err != nil {
		return accounts, fmt.Errorf("derive global: %w", err)
	}
	if accounts.BondingCurve, err = derivePDA(pump.ProgramKey, []byte(constants.SeedBondingCurve), mint[:]); err != nil {
		return accounts, fmt.Errorf("derive bonding curve: %w", err)
	}
	if accounts.CreatorVault, err = derivePDA(pump.ProgramKey, []byte(constants.SeedCreatorVault), bondingCurve.Creator[:]); err != nil {
		return accounts, fmt.Errorf("derive creator vault: %w", err)
	}
	if accounts.SharingConfig, err = derivePDA(constants.PumpFeeProgramID, []byte(constants.SeedSharingConfig), mint[:]); err != nil {
		return accounts, fmt.Errorf("derive sharing config: %w", err)
	}
	if accounts.GlobalVolumeAccumulator, err = derivePDA(pump.ProgramKey, []byte(constants.SeedGlobalVolumeAccumulator)); err != nil {
		return accounts, fmt.Errorf("derive global volume accumulator: %w", err)
	}
	if accounts.UserVolumeAccumulator, err = derivePDA(pump.ProgramKey, []byte(constants.SeedUserVolumeAccumulator), user[:]); err != nil {
		return accounts, fmt.Errorf("derive user volume accumulator: %w", err)
	}
	if accounts.FeeConfig, err = derivePDA(constants.PumpFeeProgramID, []byte("fee_config"), pump.ProgramKey[:]); err != nil {
		return accounts, fmt.Errorf("derive fee config: %w", err)
	}
	if accounts.EventAuthority, err = derivePDA(pump.ProgramKey, []byte(constants.SeedEventAuthority)); err != nil {
		return accounts, fmt.Errorf("derive event authority: %w", err)
	}

	ataInputs := []struct {
		destination *solana.PublicKey
		owner       solana.PublicKey
		mint        solana.PublicKey
		program     solana.PublicKey
	}{
		{&accounts.AssociatedQuoteFeeRecipient, accounts.FeeRecipient, quoteMint, quoteTokenProgram},
		{&accounts.AssociatedQuoteBuybackFeeRecipient, accounts.BuybackFeeRecipient, quoteMint, quoteTokenProgram},
		{&accounts.AssociatedBaseBondingCurve, accounts.BondingCurve, mint, baseTokenProgram},
		{&accounts.AssociatedQuoteBondingCurve, accounts.BondingCurve, quoteMint, quoteTokenProgram},
		{&accounts.AssociatedBaseUser, user, mint, baseTokenProgram},
		{&accounts.AssociatedQuoteUser, user, quoteMint, quoteTokenProgram},
		{&accounts.AssociatedCreatorVault, accounts.CreatorVault, quoteMint, quoteTokenProgram},
		{&accounts.AssociatedUserVolumeAccumulator, accounts.UserVolumeAccumulator, quoteMint, quoteTokenProgram},
	}
	for _, input := range ataInputs {
		ata, _, err := findATAWithProgram(input.owner, input.mint, input.program, constants.AssociatedTokenProgramID)
		if err != nil {
			return accounts, fmt.Errorf("derive ATA for owner %s and mint %s: %w", input.owner, input.mint, err)
		}
		*input.destination = ata
	}
	return accounts, nil
}

func (a pumpV2AccountSet) buyAccounts() pump.BuyV2Accounts {
	return pump.BuyV2Accounts{
		Global:                             a.Global,
		BaseMint:                           a.BaseMint,
		QuoteMint:                          a.QuoteMint,
		BaseTokenProgram:                   a.BaseTokenProgram,
		QuoteTokenProgram:                  a.QuoteTokenProgram,
		AssociatedTokenProgram:             constants.AssociatedTokenProgramID,
		FeeRecipient:                       a.FeeRecipient,
		AssociatedQuoteFeeRecipient:        a.AssociatedQuoteFeeRecipient,
		BuybackFeeRecipient:                a.BuybackFeeRecipient,
		AssociatedQuoteBuybackFeeRecipient: a.AssociatedQuoteBuybackFeeRecipient,
		BondingCurve:                       a.BondingCurve,
		AssociatedBaseBondingCurve:         a.AssociatedBaseBondingCurve,
		AssociatedQuoteBondingCurve:        a.AssociatedQuoteBondingCurve,
		User:                               a.User,
		AssociatedBaseUser:                 a.AssociatedBaseUser,
		AssociatedQuoteUser:                a.AssociatedQuoteUser,
		CreatorVault:                       a.CreatorVault,
		AssociatedCreatorVault:             a.AssociatedCreatorVault,
		SharingConfig:                      a.SharingConfig,
		GlobalVolumeAccumulator:            a.GlobalVolumeAccumulator,
		UserVolumeAccumulator:              a.UserVolumeAccumulator,
		AssociatedUserVolumeAccumulator:    a.AssociatedUserVolumeAccumulator,
		FeeConfig:                          a.FeeConfig,
		FeeProgram:                         constants.PumpFeeProgramID,
		SystemProgram:                      constants.SystemProgramID,
		EventAuthority:                     a.EventAuthority,
		Program:                            pump.ProgramKey,
	}
}

func (a pumpV2AccountSet) buyExactQuoteAccounts() pump.BuyExactQuoteInV2Accounts {
	buy := a.buyAccounts()
	return pump.BuyExactQuoteInV2Accounts{
		Global:                             buy.Global,
		BaseMint:                           buy.BaseMint,
		QuoteMint:                          buy.QuoteMint,
		BaseTokenProgram:                   buy.BaseTokenProgram,
		QuoteTokenProgram:                  buy.QuoteTokenProgram,
		AssociatedTokenProgram:             buy.AssociatedTokenProgram,
		FeeRecipient:                       buy.FeeRecipient,
		AssociatedQuoteFeeRecipient:        buy.AssociatedQuoteFeeRecipient,
		BuybackFeeRecipient:                buy.BuybackFeeRecipient,
		AssociatedQuoteBuybackFeeRecipient: buy.AssociatedQuoteBuybackFeeRecipient,
		BondingCurve:                       buy.BondingCurve,
		AssociatedBaseBondingCurve:         buy.AssociatedBaseBondingCurve,
		AssociatedQuoteBondingCurve:        buy.AssociatedQuoteBondingCurve,
		User:                               buy.User,
		AssociatedBaseUser:                 buy.AssociatedBaseUser,
		AssociatedQuoteUser:                buy.AssociatedQuoteUser,
		CreatorVault:                       buy.CreatorVault,
		AssociatedCreatorVault:             buy.AssociatedCreatorVault,
		SharingConfig:                      buy.SharingConfig,
		GlobalVolumeAccumulator:            buy.GlobalVolumeAccumulator,
		UserVolumeAccumulator:              buy.UserVolumeAccumulator,
		AssociatedUserVolumeAccumulator:    buy.AssociatedUserVolumeAccumulator,
		FeeConfig:                          buy.FeeConfig,
		FeeProgram:                         buy.FeeProgram,
		SystemProgram:                      buy.SystemProgram,
		EventAuthority:                     buy.EventAuthority,
		Program:                            buy.Program,
	}
}

func (a pumpV2AccountSet) sellAccounts() pump.SellV2Accounts {
	buy := a.buyAccounts()
	return pump.SellV2Accounts{
		Global:                             buy.Global,
		BaseMint:                           buy.BaseMint,
		QuoteMint:                          buy.QuoteMint,
		BaseTokenProgram:                   buy.BaseTokenProgram,
		QuoteTokenProgram:                  buy.QuoteTokenProgram,
		AssociatedTokenProgram:             buy.AssociatedTokenProgram,
		FeeRecipient:                       buy.FeeRecipient,
		AssociatedQuoteFeeRecipient:        buy.AssociatedQuoteFeeRecipient,
		BuybackFeeRecipient:                buy.BuybackFeeRecipient,
		AssociatedQuoteBuybackFeeRecipient: buy.AssociatedQuoteBuybackFeeRecipient,
		BondingCurve:                       buy.BondingCurve,
		AssociatedBaseBondingCurve:         buy.AssociatedBaseBondingCurve,
		AssociatedQuoteBondingCurve:        buy.AssociatedQuoteBondingCurve,
		User:                               buy.User,
		AssociatedBaseUser:                 buy.AssociatedBaseUser,
		AssociatedQuoteUser:                buy.AssociatedQuoteUser,
		CreatorVault:                       buy.CreatorVault,
		AssociatedCreatorVault:             buy.AssociatedCreatorVault,
		SharingConfig:                      buy.SharingConfig,
		UserVolumeAccumulator:              buy.UserVolumeAccumulator,
		AssociatedUserVolumeAccumulator:    buy.AssociatedUserVolumeAccumulator,
		FeeConfig:                          buy.FeeConfig,
		FeeProgram:                         buy.FeeProgram,
		SystemProgram:                      buy.SystemProgram,
		EventAuthority:                     buy.EventAuthority,
		Program:                            buy.Program,
	}
}

func ensurePumpV2UserATAs(ctx context.Context, rpc *sdkrpc.Client, accounts pumpV2AccountSet, isBuy bool) ([]solana.Instruction, error) {
	requests := make([]ataRequest, 0, 2)
	if isBuy {
		requests = append(requests, ataRequest{
			Payer:        accounts.User,
			Wallet:       accounts.User,
			Mint:         accounts.BaseMint,
			TokenProgram: accounts.BaseTokenProgram,
			ATAProgram:   constants.AssociatedTokenProgramID,
		})
	}
	if accounts.QuoteMint != constants.WSOLMint {
		requests = append(requests, ataRequest{
			Payer:        accounts.User,
			Wallet:       accounts.User,
			Mint:         accounts.QuoteMint,
			TokenProgram: accounts.QuoteTokenProgram,
			ATAProgram:   constants.AssociatedTokenProgramID,
		})
	}
	if len(requests) == 0 {
		return nil, nil
	}
	return ensureATABatch(ctx, rpc, requests)
}

func pumpV2QuoteMint(quoteMint solana.PublicKey) solana.PublicKey {
	if quoteMint.IsZero() || quoteMint == constants.WSOLMint {
		return constants.WSOLMint
	}
	return quoteMint
}

func isSupportedTokenProgram(program solana.PublicKey) bool {
	return program == constants.TokenProgramID || program == constants.Token2022ProgramID
}

func derivePDA(program solana.PublicKey, seeds ...[]byte) (solana.PublicKey, error) {
	key, _, err := solana.FindProgramAddress(seeds, program)
	return key, err
}

func writePumpV2Preview(options *Options, accounts, args any) {
	if options.Preview == nil {
		return
	}
	_ = json.NewEncoder(options.Preview).Encode(struct {
		Accounts any `json:"accounts"`
		Args     any `json:"args"`
	}{accounts, args})
}
