package main

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"

	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
)

func autofillPumpAMMBuy(ctx context.Context, deps *runtimeDeps, poolStr, userStr string) (pumpamm.BuyAccounts, error) {
	pool, err := parsePubkey("pool", poolStr)
	if err != nil {
		return pumpamm.BuyAccounts{}, err
	}
	user, err := parsePubkey("user", userStr)
	if err != nil {
		return pumpamm.BuyAccounts{}, err
	}

	poolState, err := loadPool(ctx, deps, pool)
	if err != nil {
		return pumpamm.BuyAccounts{}, err
	}
	globalConfig, err := deriveGlobalConfigPDA()
	if err != nil {
		return pumpamm.BuyAccounts{}, err
	}
	globalCfg, err := fetchGlobalConfig(ctx, deps, globalConfig)
	if err != nil {
		return pumpamm.BuyAccounts{}, err
	}
	protocolRecipient := firstNonZero(globalCfg.ProtocolFeeRecipients[:])
	if protocolRecipient.IsZero() {
		return pumpamm.BuyAccounts{}, fmt.Errorf("protocol fee recipient not found in global_config")
	}

	userBaseATA, _, err := solana.FindAssociatedTokenAddress(user, poolState.BaseMint)
	if err != nil {
		return pumpamm.BuyAccounts{}, fmt.Errorf("derive user base ata: %w", err)
	}
	userQuoteATA, _, err := solana.FindAssociatedTokenAddress(user, poolState.QuoteMint)
	if err != nil {
		return pumpamm.BuyAccounts{}, fmt.Errorf("derive user quote ata: %w", err)
	}

	accounts := pumpamm.BuyAccounts{
		Pool:                   pool,
		User:                   user,
		GlobalConfig:           globalConfig,
		BaseMint:               poolState.BaseMint,
		QuoteMint:              poolState.QuoteMint,
		UserBaseTokenAccount:   userBaseATA,
		UserQuoteTokenAccount:  userQuoteATA,
		PoolBaseTokenAccount:   poolState.PoolBaseTokenAccount,
		PoolQuoteTokenAccount:  poolState.PoolQuoteTokenAccount,
		ProtocolFeeRecipient:   protocolRecipient,
		BaseTokenProgram:       defaultTokenProgram(),
		QuoteTokenProgram:      defaultTokenProgram(),
		SystemProgram:          defaultSystemProgram(),
		AssociatedTokenProgram: defaultAssociatedTokenProgram(),
		Program:                pumpamm.ProgramKey,
		FeeProgram:             constants.PumpFeeProgramID,
	}

	// Derived PDAs
	if pk, _, err := pumpamm.DeriveBuyEventAuthorityPDA(accounts, pumpamm.BuyArgs{}); err == nil {
		accounts.EventAuthority = pk
	}
	if pk, _, err := pumpamm.DeriveBuyGlobalVolumeAccumulatorPDA(accounts, pumpamm.BuyArgs{}); err == nil {
		accounts.GlobalVolumeAccumulator = pk
	}
	if pk, _, err := pumpamm.DeriveBuyUserVolumeAccumulatorPDA(accounts, pumpamm.BuyArgs{}); err == nil {
		accounts.UserVolumeAccumulator = pk
	}
	if pk, _, err := pumpamm.DeriveBuyFeeConfigPDA(accounts, pumpamm.BuyArgs{}); err == nil {
		accounts.FeeConfig = pk
	}
	if pk, _, err := pumpamm.DeriveBuyProtocolFeeRecipientTokenAccountPDA(accounts, pumpamm.BuyArgs{}); err == nil {
		accounts.ProtocolFeeRecipientTokenAccount = pk
	}
	// Coin creator vault PDA chain
	if !poolState.CoinCreator.IsZero() {
		accounts.CoinCreatorVaultAuthority = solana.PublicKey{}
		if pk, _, err := pumpamm.DeriveBuyCoinCreatorVaultAuthorityPDA(accounts, pumpamm.BuyArgs{}); err == nil {
			accounts.CoinCreatorVaultAuthority = pk
		}
		if !accounts.CoinCreatorVaultAuthority.IsZero() {
			if pk, _, err := pumpamm.DeriveBuyCoinCreatorVaultAtaPDA(accounts, pumpamm.BuyArgs{}); err == nil {
				accounts.CoinCreatorVaultAta = pk
			}
		}
	}

	return accounts, nil
}

func autofillPumpAMMSell(ctx context.Context, deps *runtimeDeps, poolStr, userStr string) (pumpamm.SellAccounts, error) {
	pool, err := parsePubkey("pool", poolStr)
	if err != nil {
		return pumpamm.SellAccounts{}, err
	}
	user, err := parsePubkey("user", userStr)
	if err != nil {
		return pumpamm.SellAccounts{}, err
	}

	poolState, err := loadPool(ctx, deps, pool)
	if err != nil {
		return pumpamm.SellAccounts{}, err
	}
	globalConfig, err := deriveGlobalConfigPDA()
	if err != nil {
		return pumpamm.SellAccounts{}, err
	}
	globalCfg, err := fetchGlobalConfig(ctx, deps, globalConfig)
	if err != nil {
		return pumpamm.SellAccounts{}, err
	}
	protocolRecipient := firstNonZero(globalCfg.ProtocolFeeRecipients[:])
	if protocolRecipient.IsZero() {
		return pumpamm.SellAccounts{}, fmt.Errorf("protocol fee recipient not found in global_config")
	}

	userBaseATA, _, err := solana.FindAssociatedTokenAddress(user, poolState.BaseMint)
	if err != nil {
		return pumpamm.SellAccounts{}, fmt.Errorf("derive user base ata: %w", err)
	}
	userQuoteATA, _, err := solana.FindAssociatedTokenAddress(user, poolState.QuoteMint)
	if err != nil {
		return pumpamm.SellAccounts{}, fmt.Errorf("derive user quote ata: %w", err)
	}

	accounts := pumpamm.SellAccounts{
		Pool:                   pool,
		User:                   user,
		GlobalConfig:           globalConfig,
		BaseMint:               poolState.BaseMint,
		QuoteMint:              poolState.QuoteMint,
		UserBaseTokenAccount:   userBaseATA,
		UserQuoteTokenAccount:  userQuoteATA,
		PoolBaseTokenAccount:   poolState.PoolBaseTokenAccount,
		PoolQuoteTokenAccount:  poolState.PoolQuoteTokenAccount,
		ProtocolFeeRecipient:   protocolRecipient,
		BaseTokenProgram:       defaultTokenProgram(),
		QuoteTokenProgram:      defaultTokenProgram(),
		SystemProgram:          defaultSystemProgram(),
		AssociatedTokenProgram: defaultAssociatedTokenProgram(),
		Program:                pumpamm.ProgramKey,
		FeeProgram:             constants.PumpFeeProgramID,
	}

	if pk, _, err := pumpamm.DeriveSellEventAuthorityPDA(accounts, pumpamm.SellArgs{}); err == nil {
		accounts.EventAuthority = pk
	}
	if pk, _, err := pumpamm.DeriveSellFeeConfigPDA(accounts, pumpamm.SellArgs{}); err == nil {
		accounts.FeeConfig = pk
	}
	if pk, _, err := pumpamm.DeriveSellProtocolFeeRecipientTokenAccountPDA(accounts, pumpamm.SellArgs{}); err == nil {
		accounts.ProtocolFeeRecipientTokenAccount = pk
	}
	if pk, _, err := pumpamm.DeriveSellCoinCreatorVaultAuthorityPDA(accounts, pumpamm.SellArgs{}); err == nil {
		accounts.CoinCreatorVaultAuthority = pk
		if pk2, _, err := pumpamm.DeriveSellCoinCreatorVaultAtaPDA(accounts, pumpamm.SellArgs{}); err == nil {
			accounts.CoinCreatorVaultAta = pk2
		}
	}
	return accounts, nil
}

func loadPool(ctx context.Context, deps *runtimeDeps, pool solana.PublicKey) (pumpamm.Pool, error) {
	var out pumpamm.Pool
	data, err := fetchAccountData(ctx, deps, pool)
	if err != nil {
		return out, err
	}
	if err := out.Unmarshal(data); err != nil {
		return out, err
	}
	return out, nil
}

func deriveGlobalConfigPDA() (solana.PublicKey, error) {
	seeds := [][]byte{[]byte("global_config")}
	pk, _, err := solana.FindProgramAddress(seeds, pumpamm.ProgramKey)
	return pk, err
}

func fetchGlobalConfig(ctx context.Context, deps *runtimeDeps, addr solana.PublicKey) (pumpamm.GlobalConfig, error) {
	var out pumpamm.GlobalConfig
	data, err := fetchAccountData(ctx, deps, addr)
	if err != nil {
		return out, err
	}
	if err := out.Unmarshal(data); err != nil {
		return out, err
	}
	return out, nil
}

func firstNonZero(list []solana.PublicKey) solana.PublicKey {
	for _, pk := range list {
		if !isZeroPK(pk) {
			return pk
		}
	}
	return solana.PublicKey{}
}
