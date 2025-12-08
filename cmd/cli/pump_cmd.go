package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/spf13/cobra"

	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// signer is an alias for wallet.Signer for CLI internal use.
type signer = wallet.Signer

// confirmationConfirmed is the default confirmation level.
const confirmationConfirmed = txbuilder.ConfirmationConfirmed

// newLocalSigner wraps a private key as a wallet.Signer.
func newLocalSigner(key solana.PrivateKey) wallet.Signer {
	return wallet.NewLocalFromPrivateKey(key)
}

func newPumpCmd(opts *globalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pump",
		Short: "pump bonding-curve interactions",
	}
	cmd.AddCommand(
		newPumpBuyCmd(opts),
		newPumpSellCmd(opts),
		newPumpCreateCmd(opts),
		newPumpCreateV2Cmd(opts),
		newPumpInfoCmd(opts),
		newPumpSimBuyCmd(opts),
		newPumpSimSellCmd(opts),
	)
	return cmd
}

func newPumpBuyCmd(opts *globalOpts) *cobra.Command {
	var (
		mintStr      string
		userStr      string
		amount       uint64
		maxSolCost   uint64
		trackVolume  bool
		overridePath string
		preview      bool
	)

	cmd := &cobra.Command{
		Use:   "buy",
		Short: "Buy tokens on pump bonding curve",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}

			accounts, err := autofillPumpBuy(ctx, deps, mintStr, userStr)
			if err != nil {
				return err
			}
			if overridePath != "" {
				mp, err := loadPubkeyMap(overridePath)
				if err != nil {
					return err
				}
				if err := applyPubkeyOverrides(&accounts, mp); err != nil {
					return err
				}
			}
			fillPumpBuyPDAs(&accounts)

			argsObj := pump.BuyArgs{
				Amount:     amount,
				MaxSolCost: maxSolCost,
				TrackVolume: pump.OptionBool{
					Field0: trackVolume,
				},
			}

			if preview {
				bz, _ := json.MarshalIndent(accounts, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(bz))
				return nil
			}

			ix, err := pump.BuildBuy(accounts, argsObj)
			if err != nil {
				return err
			}

			sig, err := deps.builder.BuildSignSend(ctx, deps.signer, nil, ix)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tx signature: %s\n", sig.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&mintStr, "mint", "", "mint pubkey")
	cmd.Flags().StringVar(&userStr, "user", "", "user pubkey (signer)")
	cmd.Flags().Uint64Var(&amount, "amount", 0, "amount of tokens to buy")
	cmd.Flags().Uint64Var(&maxSolCost, "max-sol-cost", 0, "max SOL spend")
	cmd.Flags().BoolVar(&trackVolume, "track-volume", true, "track volume flag")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	cmd.Flags().BoolVar(&preview, "preview", false, "only print derived accounts")
	_ = cmd.MarkFlagRequired("mint")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("amount")
	_ = cmd.MarkFlagRequired("max-sol-cost")

	return cmd
}

func fillPumpBuyPDAs(a *pump.BuyAccounts) {
	if a == nil {
		return
	}
	if isZero(a.Program) {
		a.Program = pump.ProgramKey
	}
	if isZero(a.SystemProgram) {
		a.SystemProgram = defaultSystemProgram()
	}
	if isZero(a.TokenProgram) {
		a.TokenProgram = defaultTokenProgram()
	}
	if isZero(a.Global) {
		if pk, _, err := pump.DeriveBuyGlobalPDA(*a, pump.BuyArgs{}); err == nil {
			a.Global = pk
		}
	}
	if isZero(a.BondingCurve) {
		if pk, _, err := pump.DeriveBuyBondingCurvePDA(*a, pump.BuyArgs{}); err == nil {
			a.BondingCurve = pk
		}
	}
	if isZero(a.AssociatedBondingCurve) {
		if pk, _, err := pump.DeriveBuyAssociatedBondingCurvePDA(*a, pump.BuyArgs{}); err == nil {
			a.AssociatedBondingCurve = pk
		}
	}
	if isZero(a.CreatorVault) {
		if pk, _, err := pump.DeriveBuyCreatorVaultPDA(*a, pump.BuyArgs{}); err == nil {
			a.CreatorVault = pk
		}
	}
	if isZero(a.EventAuthority) {
		if pk, _, err := pump.DeriveBuyEventAuthorityPDA(*a, pump.BuyArgs{}); err == nil {
			a.EventAuthority = pk
		}
	}
	if isZero(a.GlobalVolumeAccumulator) {
		if pk, _, err := pump.DeriveBuyGlobalVolumeAccumulatorPDA(*a, pump.BuyArgs{}); err == nil {
			a.GlobalVolumeAccumulator = pk
		}
	}
	if isZero(a.UserVolumeAccumulator) {
		if pk, _, err := pump.DeriveBuyUserVolumeAccumulatorPDA(*a, pump.BuyArgs{}); err == nil {
			a.UserVolumeAccumulator = pk
		}
	}
	if isZero(a.FeeConfig) {
		if pk, _, err := pump.DeriveBuyFeeConfigPDA(*a, pump.BuyArgs{}); err == nil {
			a.FeeConfig = pk
		}
	}
}

func newPumpSellCmd(opts *globalOpts) *cobra.Command {
	var (
		mintStr      string
		userStr      string
		amount       uint64
		minSolOutput uint64
		overridePath string
		preview      bool
	)

	cmd := &cobra.Command{
		Use:   "sell",
		Short: "Sell tokens on pump bonding curve",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}

			accounts, err := autofillPumpSell(ctx, deps, mintStr, userStr)
			if err != nil {
				return err
			}
			if overridePath != "" {
				mp, err := loadPubkeyMap(overridePath)
				if err != nil {
					return err
				}
				if err := applyPubkeyOverrides(&accounts, mp); err != nil {
					return err
				}
			}
			fillPumpSellPDAs(&accounts)

			argsObj := pump.SellArgs{
				Amount:       amount,
				MinSolOutput: minSolOutput,
			}

			if preview {
				bz, _ := json.MarshalIndent(accounts, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(bz))
				return nil
			}

			ix, err := pump.BuildSell(accounts, argsObj)
			if err != nil {
				return err
			}

			sig, err := deps.builder.BuildSignSend(ctx, deps.signer, nil, ix)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tx signature: %s\n", sig.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&mintStr, "mint", "", "mint pubkey")
	cmd.Flags().StringVar(&userStr, "user", "", "user pubkey (signer)")
	cmd.Flags().Uint64Var(&amount, "amount", 0, "token amount to sell")
	cmd.Flags().Uint64Var(&minSolOutput, "min-sol-output", 0, "minimum SOL output")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	cmd.Flags().BoolVar(&preview, "preview", false, "only print derived accounts")
	_ = cmd.MarkFlagRequired("mint")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("amount")
	_ = cmd.MarkFlagRequired("min-sol-output")

	return cmd
}

func fillPumpSellPDAs(a *pump.SellAccounts) {
	if a == nil {
		return
	}
	if isZero(a.Program) {
		a.Program = pump.ProgramKey
	}
	if isZero(a.SystemProgram) {
		a.SystemProgram = defaultSystemProgram()
	}
	if isZero(a.TokenProgram) {
		a.TokenProgram = defaultTokenProgram()
	}
	if isZero(a.Global) {
		if pk, _, err := pump.DeriveSellGlobalPDA(*a, pump.SellArgs{}); err == nil {
			a.Global = pk
		}
	}
	if isZero(a.BondingCurve) {
		if pk, _, err := pump.DeriveSellBondingCurvePDA(*a, pump.SellArgs{}); err == nil {
			a.BondingCurve = pk
		}
	}
	if isZero(a.AssociatedBondingCurve) {
		if pk, _, err := pump.DeriveSellAssociatedBondingCurvePDA(*a, pump.SellArgs{}); err == nil {
			a.AssociatedBondingCurve = pk
		}
	}
	if isZero(a.CreatorVault) {
		if pk, _, err := pump.DeriveSellCreatorVaultPDA(*a, pump.SellArgs{}); err == nil {
			a.CreatorVault = pk
		}
	}
	if isZero(a.EventAuthority) {
		if pk, _, err := pump.DeriveSellEventAuthorityPDA(*a, pump.SellArgs{}); err == nil {
			a.EventAuthority = pk
		}
	}
	if isZero(a.FeeConfig) {
		if pk, _, err := pump.DeriveSellFeeConfigPDA(*a, pump.SellArgs{}); err == nil {
			a.FeeConfig = pk
		}
	}
}

func newPumpCreateCmd(opts *globalOpts) *cobra.Command {
	var (
		name          string
		symbol        string
		uri           string
		preview       bool
		vanitySuffix  string
		vanityPrefix  string
		vanityTimeout int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new SPL Token on Pump.fun bonding curve",
		Long: `Create a new SPL Token on Pump.fun bonding curve.

This command uses the 'create' instruction which creates SPL Token.
For Token-2022 tokens, use 'pump create-v2' instead.

Vanity Address:
  - Use --suffix to generate address ending with specific characters (e.g., 'pump')
  - Use --prefix to generate address starting with specific characters
  - Longer patterns take exponentially longer to generate`,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout := 30 * time.Second
			if vanitySuffix != "" || vanityPrefix != "" {
				timeout = time.Duration(vanityTimeout) * time.Second
				fmt.Fprintf(cmd.OutOrStdout(), "🔍 Searching for vanity address")
				if vanityPrefix != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " prefix='%s'", vanityPrefix)
				}
				if vanitySuffix != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " suffix='%s'", vanitySuffix)
				}
				fmt.Fprintf(cmd.OutOrStdout(), " (timeout: %ds)...\n", vanityTimeout)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout+30*time.Second)
			defer cancel()

			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			user := deps.signer.PublicKey()

			startTime := time.Now()

			accounts, argsObj, ix, mintKey, err := autofillPumpCreate(
				ctx, deps, user, name, symbol, uri,
				vanitySuffix, vanityPrefix, time.Duration(vanityTimeout)*time.Second,
			)
			if err != nil {
				return err
			}

			if vanitySuffix != "" || vanityPrefix != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "✅ Found vanity address in %s\n\n", time.Since(startTime))
			}

			if preview {
				out := struct {
					Accounts pump.CreateAccounts `json:"accounts"`
					Args     pump.CreateArgs     `json:"args"`
					Mint     string              `json:"mint"`
				}{accounts, argsObj, mintKey.PublicKey().String()}
				bz, _ := json.MarshalIndent(out, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(bz))
				return nil
			}

			mintSigner := newLocalSigner(mintKey)
			sig, err := deps.builder.BuildSignSendAndConfirm(
				ctx, deps.signer, []signer{mintSigner}, confirmationConfirmed, ix,
			)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "✅ SPL Token created!\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Transaction: %s\n", sig.String())
			fmt.Fprintf(cmd.OutOrStdout(), "Mint: %s\n", mintKey.PublicKey().String())
			fmt.Fprintf(cmd.OutOrStdout(), "BondingCurve: %s\n", accounts.BondingCurve.String())
			fmt.Fprintf(cmd.OutOrStdout(), "\n⚠️  Save the mint private key:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", mintKey.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "token name (e.g., 'My Token')")
	cmd.Flags().StringVar(&symbol, "symbol", "", "token symbol (e.g., 'MTK')")
	cmd.Flags().StringVar(&uri, "uri", "", "metadata URI (e.g., 'https://example.com/metadata.json')")
	cmd.Flags().BoolVar(&preview, "preview", false, "only print accounts without sending transaction")
	cmd.Flags().StringVar(&vanitySuffix, "suffix", "", "vanity address suffix (e.g., 'pump')")
	cmd.Flags().StringVar(&vanityPrefix, "prefix", "", "vanity address prefix")
	cmd.Flags().IntVar(&vanityTimeout, "vanity-timeout", 300, "vanity address search timeout in seconds")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("symbol")
	_ = cmd.MarkFlagRequired("uri")

	return cmd
}

func newPumpCreateV2Cmd(opts *globalOpts) *cobra.Command {
	var (
		name          string
		symbol        string
		uri           string
		preview       bool
		isMayhemMode  bool
		vanitySuffix  string
		vanityPrefix  string
		vanityTimeout int
	)

	cmd := &cobra.Command{
		Use:   "create-v2",
		Short: "Create a new Token-2022 on Pump.fun bonding curve",
		Long: `Create a new Token-2022 on Pump.fun bonding curve.

This command uses the 'create_v2' instruction which creates Token-2022 tokens.
For SPL Token, use 'pump create' instead.

Vanity Address:
  - Use --suffix to generate address ending with specific characters (e.g., 'pump')
  - Use --prefix to generate address starting with specific characters
  - Longer patterns take exponentially longer to generate`,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout := 30 * time.Second
			if vanitySuffix != "" || vanityPrefix != "" {
				timeout = time.Duration(vanityTimeout) * time.Second
				fmt.Fprintf(cmd.OutOrStdout(), "🔍 Searching for vanity address")
				if vanityPrefix != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " prefix='%s'", vanityPrefix)
				}
				if vanitySuffix != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " suffix='%s'", vanitySuffix)
				}
				fmt.Fprintf(cmd.OutOrStdout(), " (timeout: %ds)...\n", vanityTimeout)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout+30*time.Second)
			defer cancel()

			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			user := deps.signer.PublicKey()

			startTime := time.Now()

			accounts, argsObj, ix, mintKey, err := autofillPumpCreateV2(
				ctx, deps, user, name, symbol, uri, isMayhemMode,
				vanitySuffix, vanityPrefix, time.Duration(vanityTimeout)*time.Second,
			)
			if err != nil {
				return err
			}

			if vanitySuffix != "" || vanityPrefix != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "✅ Found vanity address in %s\n\n", time.Since(startTime))
			}

			if preview {
				out := struct {
					Accounts pump.CreateV2Accounts `json:"accounts"`
					Args     pump.CreateV2Args     `json:"args"`
					Mint     string                `json:"mint"`
				}{accounts, argsObj, mintKey.PublicKey().String()}
				bz, _ := json.MarshalIndent(out, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(bz))
				return nil
			}

			mintSigner := newLocalSigner(mintKey)
			sig, err := deps.builder.BuildSignSendAndConfirm(
				ctx, deps.signer, []signer{mintSigner}, confirmationConfirmed, ix,
			)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "✅ Token-2022 created!\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Transaction: %s\n", sig.String())
			fmt.Fprintf(cmd.OutOrStdout(), "Mint: %s\n", mintKey.PublicKey().String())
			fmt.Fprintf(cmd.OutOrStdout(), "BondingCurve: %s\n", accounts.BondingCurve.String())
			fmt.Fprintf(cmd.OutOrStdout(), "\n⚠️  Save the mint private key:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", mintKey.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "token name (e.g., 'My Token')")
	cmd.Flags().StringVar(&symbol, "symbol", "", "token symbol (e.g., 'MTK')")
	cmd.Flags().StringVar(&uri, "uri", "", "metadata URI (e.g., 'https://example.com/metadata.json')")
	cmd.Flags().BoolVar(&preview, "preview", false, "only print accounts without sending transaction")
	cmd.Flags().BoolVar(&isMayhemMode, "mayhem", false, "enable mayhem mode")
	cmd.Flags().StringVar(&vanitySuffix, "suffix", "", "vanity address suffix (e.g., 'pump')")
	cmd.Flags().StringVar(&vanityPrefix, "prefix", "", "vanity address prefix")
	cmd.Flags().IntVar(&vanityTimeout, "vanity-timeout", 300, "vanity address search timeout in seconds")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("symbol")
	_ = cmd.MarkFlagRequired("uri")

	return cmd
}

func fillPumpCreatePDAs(a *pump.CreateAccounts) {
	if a == nil {
		return
	}
	if isZero(a.Global) {
		if pk, _, err := pump.DeriveCreateGlobalPDA(*a, pump.CreateArgs{}); err == nil {
			a.Global = pk
		}
	}
	if isZero(a.BondingCurve) {
		if pk, _, err := pump.DeriveCreateBondingCurvePDA(*a, pump.CreateArgs{}); err == nil {
			a.BondingCurve = pk
		}
	}
	if isZero(a.AssociatedBondingCurve) {
		if pk, _, err := pump.DeriveCreateAssociatedBondingCurvePDA(*a, pump.CreateArgs{}); err == nil {
			a.AssociatedBondingCurve = pk
		}
	}
	if isZero(a.EventAuthority) {
		if pk, _, err := pump.DeriveCreateEventAuthorityPDA(*a, pump.CreateArgs{}); err == nil {
			a.EventAuthority = pk
		}
	}
}

func newPumpInfoCmd(opts *globalOpts) *cobra.Command {
	var mintStr string
	return &cobra.Command{
		Use:   "info",
		Short: "Fetch bonding curve/global info",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			mint, err := parsePubkey("mint", mintStr)
			if err != nil {
				return err
			}
			accts := pump.BuyAccounts{Mint: mint}
			bondingCurve, _, _ := pump.DeriveBuyBondingCurvePDA(accts, pump.BuyArgs{})
			global, _, _ := pump.DeriveBuyGlobalPDA(accts, pump.BuyArgs{})

			targets := []struct {
				label string
				pk    solana.PublicKey
			}{
				{"bonding_curve", bondingCurve},
				{"global", global},
			}
			for _, t := range targets {
				data, err := fetchAccountData(ctx, deps, t.pk)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%s fetch error: %v\n", t.label, err)
					continue
				}
				name, decoded, err := decodeKnownAccount(data)
				if err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "%s decode error: %v\n", t.label, err)
					continue
				}
				bz, _ := json.MarshalIndent(decoded, "", "  ")
				fmt.Fprintf(cmd.OutOrStdout(), "%s (%s):\n%s\n", t.label, name, string(bz))
			}
			return nil
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if mintStr == "" {
				return fmt.Errorf("mint is required")
			}
			return nil
		},
	}
}

func newPumpSimBuyCmd(opts *globalOpts) *cobra.Command {
	var (
		mintStr      string
		userStr      string
		amount       uint64
		maxSolCost   uint64
		trackVolume  bool
		overridePath string
	)
	cmd := &cobra.Command{
		Use:   "simulate-buy",
		Short: "Simulate pump buy",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			accounts, err := autofillPumpBuy(ctx, deps, mintStr, userStr)
			if err != nil {
				return err
			}
			if overridePath != "" {
				mp, err := loadPubkeyMap(overridePath)
				if err != nil {
					return err
				}
				if err := applyPubkeyOverrides(&accounts, mp); err != nil {
					return err
				}
			}
			argsObj := pump.BuyArgs{
				Amount:     amount,
				MaxSolCost: maxSolCost,
				TrackVolume: pump.OptionBool{
					Field0: trackVolume,
				},
			}
			ix, err := pump.BuildBuy(accounts, argsObj)
			if err != nil {
				return err
			}
			res, err := simulateInstruction(ctx, deps, ix, opts.commitment)
			if err != nil {
				return err
			}
			printSimResult(cmd, res)
			return nil
		},
	}
	cmd.Flags().StringVar(&mintStr, "mint", "", "mint pubkey")
	cmd.Flags().StringVar(&userStr, "user", "", "user pubkey (signer)")
	cmd.Flags().Uint64Var(&amount, "amount", 0, "amount of tokens to buy")
	cmd.Flags().Uint64Var(&maxSolCost, "max-sol-cost", 0, "max SOL spend")
	cmd.Flags().BoolVar(&trackVolume, "track-volume", true, "track volume flag")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	_ = cmd.MarkFlagRequired("mint")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("amount")
	_ = cmd.MarkFlagRequired("max-sol-cost")
	return cmd
}

func newPumpSimSellCmd(opts *globalOpts) *cobra.Command {
	var (
		mintStr      string
		userStr      string
		amount       uint64
		minSolOutput uint64
		overridePath string
	)
	cmd := &cobra.Command{
		Use:   "simulate-sell",
		Short: "Simulate pump sell",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			accounts, err := autofillPumpSell(ctx, deps, mintStr, userStr)
			if err != nil {
				return err
			}
			if overridePath != "" {
				mp, err := loadPubkeyMap(overridePath)
				if err != nil {
					return err
				}
				if err := applyPubkeyOverrides(&accounts, mp); err != nil {
					return err
				}
			}
			argsObj := pump.SellArgs{
				Amount:       amount,
				MinSolOutput: minSolOutput,
			}
			ix, err := pump.BuildSell(accounts, argsObj)
			if err != nil {
				return err
			}
			res, err := simulateInstruction(ctx, deps, ix, opts.commitment)
			if err != nil {
				return err
			}
			printSimResult(cmd, res)
			return nil
		},
	}
	cmd.Flags().StringVar(&mintStr, "mint", "", "mint pubkey")
	cmd.Flags().StringVar(&userStr, "user", "", "user pubkey (signer)")
	cmd.Flags().Uint64Var(&amount, "amount", 0, "token amount to sell")
	cmd.Flags().Uint64Var(&minSolOutput, "min-sol-output", 0, "minimum SOL output")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	_ = cmd.MarkFlagRequired("mint")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("amount")
	_ = cmd.MarkFlagRequired("min-sol-output")
	return cmd
}

func isZero(pk solana.PublicKey) bool {
	return pk == (solana.PublicKey{})
}
