package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
)

func autofillPumpBuy(ctx context.Context, deps *runtimeDeps, mintStr, userStr string) (pump.BuyAccounts, error) {
	mint, err := parsePubkey("mint", mintStr)
	if err != nil {
		return pump.BuyAccounts{}, err
	}
	user, err := parsePubkey("user", userStr)
	if err != nil {
		return pump.BuyAccounts{}, err
	}

	global, err := derivePumpGlobalPDA()
	if err != nil {
		return pump.BuyAccounts{}, err
	}
	globalState, err := fetchPumpGlobal(ctx, deps, global)
	if err != nil {
		return pump.BuyAccounts{}, err
	}
	feeRecipient := firstNonZeroPK(append(globalState.FeeRecipients[:], globalState.FeeRecipient))
	if isZeroPK(feeRecipient) {
		return pump.BuyAccounts{}, fmt.Errorf("fee recipient not found in global")
	}

	associatedUser, _, err := solana.FindAssociatedTokenAddress(user, mint)
	if err != nil {
		return pump.BuyAccounts{}, fmt.Errorf("derive associated user ata: %w", err)
	}

	accounts := pump.BuyAccounts{
		Mint:           mint,
		User:           user,
		AssociatedUser: associatedUser,
		FeeRecipient:   feeRecipient,
		SystemProgram:  defaultSystemProgram(),
		TokenProgram:   defaultTokenProgram(),
		Program:        pump.ProgramKey,
	}

	// Derive PDAs
	if pk, _, err := pump.DeriveBuyGlobalPDA(accounts, pump.BuyArgs{}); err == nil {
		accounts.Global = pk
	}
	if pk, _, err := pump.DeriveBuyBondingCurvePDA(accounts, pump.BuyArgs{}); err == nil {
		accounts.BondingCurve = pk
	}
	if pk, _, err := pump.DeriveBuyAssociatedBondingCurvePDA(accounts, pump.BuyArgs{}); err == nil {
		accounts.AssociatedBondingCurve = pk
	}
	if pk, _, err := pump.DeriveBuyCreatorVaultPDA(accounts, pump.BuyArgs{}); err == nil {
		accounts.CreatorVault = pk
	}
	if pk, _, err := pump.DeriveBuyEventAuthorityPDA(accounts, pump.BuyArgs{}); err == nil {
		accounts.EventAuthority = pk
	}
	if pk, _, err := pump.DeriveBuyGlobalVolumeAccumulatorPDA(accounts, pump.BuyArgs{}); err == nil {
		accounts.GlobalVolumeAccumulator = pk
	}
	if pk, _, err := pump.DeriveBuyUserVolumeAccumulatorPDA(accounts, pump.BuyArgs{}); err == nil {
		accounts.UserVolumeAccumulator = pk
	}
	if pk, _, err := pump.DeriveBuyFeeConfigPDA(accounts, pump.BuyArgs{}); err == nil {
		accounts.FeeConfig = pk
	}
	accounts.FeeProgram = constants.PumpFeeProgramID
	return accounts, nil
}

func autofillPumpSell(ctx context.Context, deps *runtimeDeps, mintStr, userStr string) (pump.SellAccounts, error) {
	mint, err := parsePubkey("mint", mintStr)
	if err != nil {
		return pump.SellAccounts{}, err
	}
	user, err := parsePubkey("user", userStr)
	if err != nil {
		return pump.SellAccounts{}, err
	}

	global, err := derivePumpGlobalPDA()
	if err != nil {
		return pump.SellAccounts{}, err
	}
	globalState, err := fetchPumpGlobal(ctx, deps, global)
	if err != nil {
		return pump.SellAccounts{}, err
	}
	feeRecipient := firstNonZeroPK(append(globalState.FeeRecipients[:], globalState.FeeRecipient))
	if isZeroPK(feeRecipient) {
		return pump.SellAccounts{}, fmt.Errorf("fee recipient not found in global")
	}

	associatedUser, _, err := solana.FindAssociatedTokenAddress(user, mint)
	if err != nil {
		return pump.SellAccounts{}, fmt.Errorf("derive associated user ata: %w", err)
	}

	accounts := pump.SellAccounts{
		Mint:           mint,
		User:           user,
		AssociatedUser: associatedUser,
		FeeRecipient:   feeRecipient,
		SystemProgram:  defaultSystemProgram(),
		TokenProgram:   defaultTokenProgram(),
		Program:        pump.ProgramKey,
		FeeProgram:     constants.PumpFeeProgramID,
	}

	if pk, _, err := pump.DeriveSellGlobalPDA(accounts, pump.SellArgs{}); err == nil {
		accounts.Global = pk
	}
	if pk, _, err := pump.DeriveSellBondingCurvePDA(accounts, pump.SellArgs{}); err == nil {
		accounts.BondingCurve = pk
	}
	if pk, _, err := pump.DeriveSellAssociatedBondingCurvePDA(accounts, pump.SellArgs{}); err == nil {
		accounts.AssociatedBondingCurve = pk
	}
	if pk, _, err := pump.DeriveSellCreatorVaultPDA(accounts, pump.SellArgs{}); err == nil {
		accounts.CreatorVault = pk
	}
	if pk, _, err := pump.DeriveSellEventAuthorityPDA(accounts, pump.SellArgs{}); err == nil {
		accounts.EventAuthority = pk
	}
	if pk, _, err := pump.DeriveSellFeeConfigPDA(accounts, pump.SellArgs{}); err == nil {
		accounts.FeeConfig = pk
	}

	return accounts, nil
}

func derivePumpGlobalPDA() (solana.PublicKey, error) {
	seeds := [][]byte{[]byte("global")}
	pk, _, err := solana.FindProgramAddress(seeds, pump.ProgramKey)
	return pk, err
}

func fetchPumpGlobal(ctx context.Context, deps *runtimeDeps, addr solana.PublicKey) (pump.Global, error) {
	var out pump.Global
	data, err := fetchAccountData(ctx, deps, addr)
	if err != nil {
		return out, err
	}
	if err := out.Unmarshal(data); err != nil {
		return out, err
	}
	return out, nil
}

func firstNonZeroPK(list []solana.PublicKey) solana.PublicKey {
	for _, pk := range list {
		if !isZeroPK(pk) {
			return pk
		}
	}
	return solana.PublicKey{}
}

func autofillPumpCreate(ctx context.Context, deps *runtimeDeps, user solana.PublicKey, name, symbol, uri string, vanitySuffix, vanityPrefix string, vanityTimeout time.Duration) (pump.CreateAccounts, pump.CreateArgs, solana.Instruction, solana.PrivateKey, error) {
	var opts []autofill.Option
	if vanitySuffix != "" {
		opts = append(opts, autofill.WithVanitySuffix(vanitySuffix))
	}
	if vanityPrefix != "" {
		opts = append(opts, autofill.WithVanityPrefix(vanityPrefix))
	}
	if vanityTimeout > 0 {
		opts = append(opts, autofill.WithVanityTimeout(vanityTimeout))
	}
	return autofill.PumpCreate(ctx, deps.rpc, user, name, symbol, uri, opts...)
}

func autofillPumpCreateV2(ctx context.Context, deps *runtimeDeps, user solana.PublicKey, name, symbol, uri string, isMayhemMode bool, vanitySuffix, vanityPrefix string, vanityTimeout time.Duration) (pump.CreateV2Accounts, pump.CreateV2Args, solana.Instruction, solana.PrivateKey, error) {
	var opts []autofill.Option
	if vanitySuffix != "" {
		opts = append(opts, autofill.WithVanitySuffix(vanitySuffix))
	}
	if vanityPrefix != "" {
		opts = append(opts, autofill.WithVanityPrefix(vanityPrefix))
	}
	if vanityTimeout > 0 {
		opts = append(opts, autofill.WithVanityTimeout(vanityTimeout))
	}
	return autofill.PumpCreateV2(ctx, deps.rpc, user, name, symbol, uri, isMayhemMode, opts...)
}
