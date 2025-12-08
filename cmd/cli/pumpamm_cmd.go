package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/spf13/cobra"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
)

func newPumpAMMCmd(opts *globalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pump-amm",
		Short: "pump_amm pool interactions",
	}
	cmd.AddCommand(
		newPumpAMMBuyCmd(opts),
		newPumpAMMBuySolCmd(opts),
		newPumpAMMBuyExactQuoteCmd(opts),
		newPumpAMMSellCmd(opts),
		newPumpAMMDepositCmd(opts),
		newPumpAMMWithdrawCmd(opts),
		newPumpAMMPoolInfoCmd(opts),
		newPumpAMMSimBuyCmd(opts),
		newPumpAMMAdminUpdateFeeCmd(opts),
	)
	return cmd
}

func newPumpAMMBuyCmd(opts *globalOpts) *cobra.Command {
	var (
		poolStr      string
		userStr      string
		baseOut      uint64
		maxQuoteIn   uint64
		trackVolume  bool
		overridePath string
		preview      bool
	)
	cmd := &cobra.Command{
		Use:   "buy",
		Short: "Buy base token from a pump_amm pool",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}

			accounts, err := autofillPumpAMMBuy(ctx, deps, poolStr, userStr)
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
			fillPumpAMMBuyDefaults(&accounts)

			argsObj := pumpamm.BuyArgs{
				BaseAmountOut:    baseOut,
				MaxQuoteAmountIn: maxQuoteIn,
				TrackVolume: pumpamm.OptionBool{
					Field0: trackVolume,
				},
			}

			if preview {
				bz, _ := json.MarshalIndent(accounts, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(bz))
				return nil
			}

			ix, err := pumpamm.BuildBuy(accounts, argsObj)
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

	cmd.Flags().Uint64Var(&baseOut, "base-out", 0, "base amount out")
	cmd.Flags().Uint64Var(&maxQuoteIn, "max-quote-in", 0, "max quote amount in")
	cmd.Flags().BoolVar(&trackVolume, "track-volume", true, "track volume flag")
	cmd.Flags().StringVar(&poolStr, "pool", "", "pool pubkey")
	cmd.Flags().StringVar(&userStr, "user", "", "user pubkey (signer)")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	cmd.Flags().BoolVar(&preview, "preview", false, "only print derived accounts")
	_ = cmd.MarkFlagRequired("pool")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("base-out")
	_ = cmd.MarkFlagRequired("max-quote-in")

	return cmd
}

func newPumpAMMBuySolCmd(opts *globalOpts) *cobra.Command {
	var (
		poolStr      string
		amountSol    uint64
		slippageBps  uint64
		trackVolume  bool
		overridePath string
		preview      bool
	)
	cmd := &cobra.Command{
		Use:   "buy-sol",
		Short: "Buy base with SOL budget (auto wrap, auto accounts)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			pool, err := parsePubkey("pool", poolStr)
			if err != nil {
				return err
			}
			var options []autofill.Option
			options = append(options, autofill.WithTrackVolume(trackVolume))
			if overridePath != "" {
				mp, err := loadPubkeyMap(overridePath)
				if err != nil {
					return err
				}
				overrides := make(map[string]solana.PublicKey, len(mp))
				for k, v := range mp {
					pk, err := parsePubkey(k, v)
					if err != nil {
						return err
					}
					overrides[k] = pk
				}
				options = append(options, autofill.WithOverrides(overrides))
			}

			accounts, argsObj, instrs, simBase, err := autofill.PumpAmmBuyWithSol(ctx, deps.rpc, deps.signer.PublicKey(), pool, amountSol, slippageBps, options...)
			if err != nil {
				return err
			}
			if preview {
				payload := struct {
					Accounts      pumpamm.BuyExactQuoteInAccounts `json:"accounts"`
					Args          pumpamm.BuyExactQuoteInArgs     `json:"args"`
					SimulatedBase uint64                          `json:"simulated_base_out"`
				}{accounts, argsObj, simBase}
				bz, _ := json.MarshalIndent(payload, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(bz))
				return nil
			}

			sig, err := deps.builder.BuildSignSend(ctx, deps.signer, nil, instrs...)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tx signature: %s\nsimulated_base_out: %d\nmin_base_out: %d\n", sig.String(), simBase, argsObj.MinBaseAmountOut)
			return nil
		},
	}

	cmd.Flags().Uint64Var(&amountSol, "amount-sol", 0, "SOL budget in lamports")
	cmd.Flags().Uint64Var(&slippageBps, "slippage-bps", 200, "slippage bps (max 10000)")
	cmd.Flags().BoolVar(&trackVolume, "track-volume", true, "track volume flag")
	cmd.Flags().StringVar(&poolStr, "pool", "", "pool pubkey")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	cmd.Flags().BoolVar(&preview, "preview", false, "only print derived accounts/args")
	_ = cmd.MarkFlagRequired("pool")
	_ = cmd.MarkFlagRequired("amount-sol")
	_ = cmd.MarkFlagRequired("slippage-bps")
	return cmd
}

func newPumpAMMBuyExactQuoteCmd(opts *globalOpts) *cobra.Command {
	var (
		poolStr      string
		amountQuote  uint64
		minBaseOut   uint64
		trackVolume  bool
		overridePath string
		preview      bool
	)
	cmd := &cobra.Command{
		Use:   "buy-exact-quote",
		Short: "Buy with fixed quote amount (no simulation)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			pool, err := parsePubkey("pool", poolStr)
			if err != nil {
				return err
			}
			var options []autofill.Option
			options = append(options, autofill.WithTrackVolume(trackVolume))
			if overridePath != "" {
				mp, err := loadPubkeyMap(overridePath)
				if err != nil {
					return err
				}
				overrides := make(map[string]solana.PublicKey, len(mp))
				for k, v := range mp {
					pk, err := parsePubkey(k, v)
					if err != nil {
						return err
					}
					overrides[k] = pk
				}
				options = append(options, autofill.WithOverrides(overrides))
			}

			accounts, argsObj, instrs, err := autofill.PumpAmmBuyExactQuoteIn(ctx, deps.rpc, deps.signer.PublicKey(), pool, amountQuote, minBaseOut, options...)
			if err != nil {
				return err
			}
			if preview {
				payload := struct {
					Accounts pumpamm.BuyExactQuoteInAccounts `json:"accounts"`
					Args     pumpamm.BuyExactQuoteInArgs     `json:"args"`
				}{accounts, argsObj}
				bz, _ := json.MarshalIndent(payload, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(bz))
				return nil
			}

			sig, err := deps.builder.BuildSignSend(ctx, deps.signer, nil, instrs...)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "tx signature: %s\nmin_base_out: %d\n", sig.String(), argsObj.MinBaseAmountOut)
			return nil
		},
	}

	cmd.Flags().Uint64Var(&amountQuote, "amount-quote", 0, "quote amount in lamports (spendable)")
	cmd.Flags().Uint64Var(&minBaseOut, "min-base-out", 0, "minimum base out (slippage guard)")
	cmd.Flags().BoolVar(&trackVolume, "track-volume", true, "track volume flag")
	cmd.Flags().StringVar(&poolStr, "pool", "", "pool pubkey")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	cmd.Flags().BoolVar(&preview, "preview", false, "only print derived accounts/args")
	_ = cmd.MarkFlagRequired("pool")
	_ = cmd.MarkFlagRequired("amount-quote")
	_ = cmd.MarkFlagRequired("min-base-out")
	return cmd
}

func newPumpAMMSellCmd(opts *globalOpts) *cobra.Command {
	var (
		poolStr      string
		userStr      string
		baseIn       uint64
		minQuoteOut  uint64
		overridePath string
		preview      bool
	)
	cmd := &cobra.Command{
		Use:   "sell",
		Short: "Sell base token to a pump_amm pool",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}

			accounts, err := autofillPumpAMMSell(ctx, deps, poolStr, userStr)
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
			fillPumpAMMSellDefaults(&accounts)

			argsObj := pumpamm.SellArgs{
				BaseAmountIn:      baseIn,
				MinQuoteAmountOut: minQuoteOut,
			}

			if preview {
				bz, _ := json.MarshalIndent(accounts, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(bz))
				return nil
			}

			ix, err := pumpamm.BuildSell(accounts, argsObj)
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
	cmd.Flags().Uint64Var(&baseIn, "base-in", 0, "base amount in")
	cmd.Flags().Uint64Var(&minQuoteOut, "min-quote-out", 0, "minimum quote amount out")
	cmd.Flags().StringVar(&poolStr, "pool", "", "pool pubkey")
	cmd.Flags().StringVar(&userStr, "user", "", "user pubkey (signer)")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	cmd.Flags().BoolVar(&preview, "preview", false, "only print derived accounts")
	_ = cmd.MarkFlagRequired("pool")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("base-in")
	_ = cmd.MarkFlagRequired("min-quote-out")
	return cmd
}

func newPumpAMMDepositCmd(opts *globalOpts) *cobra.Command {
	var (
		lpOut            uint64
		maxBaseIn        uint64
		maxQuoteIn       uint64
		accountsJSONPath string
	)
	cmd := &cobra.Command{
		Use:   "add-liquidity",
		Short: "Deposit liquidity into pool",
		RunE: func(cmd *cobra.Command, args []string) error {
			if accountsJSONPath == "" {
				return fmt.Errorf("accounts-json is required for add-liquidity")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()
			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			accounts, err := loadAccountsJSON[pumpamm.DepositAccounts](accountsJSONPath)
			if err != nil {
				return err
			}
			fillPumpAMMEventAuthorityDeposit(&accounts)
			argsObj := pumpamm.DepositArgs{
				LpTokenAmountOut: lpOut,
				MaxBaseAmountIn:  maxBaseIn,
				MaxQuoteAmountIn: maxQuoteIn,
			}
			ix, err := pumpamm.BuildDeposit(accounts, argsObj)
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
	cmd.Flags().Uint64Var(&lpOut, "lp-out", 0, "lp token amount out")
	cmd.Flags().Uint64Var(&maxBaseIn, "max-base-in", 0, "max base amount in")
	cmd.Flags().Uint64Var(&maxQuoteIn, "max-quote-in", 0, "max quote amount in")
	cmd.Flags().StringVar(&accountsJSONPath, "accounts-json", "", "path to accounts json with all required accounts")
	_ = cmd.MarkFlagRequired("lp-out")
	_ = cmd.MarkFlagRequired("max-base-in")
	_ = cmd.MarkFlagRequired("max-quote-in")
	_ = cmd.MarkFlagRequired("accounts-json")
	return cmd
}

func newPumpAMMWithdrawCmd(opts *globalOpts) *cobra.Command {
	var (
		lpIn             uint64
		minBaseOut       uint64
		minQuoteOut      uint64
		accountsJSONPath string
	)
	cmd := &cobra.Command{
		Use:   "remove-liquidity",
		Short: "Withdraw liquidity from pool",
		RunE: func(cmd *cobra.Command, args []string) error {
			if accountsJSONPath == "" {
				return fmt.Errorf("accounts-json is required for remove-liquidity")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()
			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			accounts, err := loadAccountsJSON[pumpamm.WithdrawAccounts](accountsJSONPath)
			if err != nil {
				return err
			}
			fillPumpAMMEventAuthorityWithdraw(&accounts)
			argsObj := pumpamm.WithdrawArgs{
				LpTokenAmountIn:   lpIn,
				MinBaseAmountOut:  minBaseOut,
				MinQuoteAmountOut: minQuoteOut,
			}
			ix, err := pumpamm.BuildWithdraw(accounts, argsObj)
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
	cmd.Flags().Uint64Var(&lpIn, "lp-in", 0, "lp token amount in")
	cmd.Flags().Uint64Var(&minBaseOut, "min-base-out", 0, "minimum base amount out")
	cmd.Flags().Uint64Var(&minQuoteOut, "min-quote-out", 0, "minimum quote amount out")
	cmd.Flags().StringVar(&accountsJSONPath, "accounts-json", "", "path to accounts json with all required accounts")
	_ = cmd.MarkFlagRequired("lp-in")
	_ = cmd.MarkFlagRequired("min-base-out")
	_ = cmd.MarkFlagRequired("min-quote-out")
	_ = cmd.MarkFlagRequired("accounts-json")
	return cmd
}

func newPumpAMMPoolInfoCmd(opts *globalOpts) *cobra.Command {
	var poolStr string
	return &cobra.Command{
		Use:   "pool-info",
		Short: "Fetch pool account info",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			pk, err := parsePubkey("pool", poolStr)
			if err != nil {
				return err
			}
			data, err := fetchAccountData(ctx, deps, pk)
			if err != nil {
				return err
			}
			name, decoded, err := decodeKnownAccount(data)
			if err != nil {
				return err
			}
			bz, _ := json.MarshalIndent(decoded, "", "  ")
			fmt.Fprintf(cmd.OutOrStdout(), "pool (%s):\n%s\n", name, string(bz))
			return nil
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if poolStr == "" {
				return fmt.Errorf("pool is required")
			}
			return nil
		},
	}
}

func newPumpAMMSimBuyCmd(opts *globalOpts) *cobra.Command {
	var (
		poolStr      string
		userStr      string
		baseOut      uint64
		maxQuoteIn   uint64
		trackVolume  bool
		overridePath string
	)
	cmd := &cobra.Command{
		Use:   "simulate-buy",
		Short: "Simulate pump_amm buy",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			accounts, err := autofillPumpAMMBuy(ctx, deps, poolStr, userStr)
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
			fillPumpAMMBuyDefaults(&accounts)
			argsObj := pumpamm.BuyArgs{
				BaseAmountOut:    baseOut,
				MaxQuoteAmountIn: maxQuoteIn,
				TrackVolume: pumpamm.OptionBool{
					Field0: trackVolume,
				},
			}
			ix, err := pumpamm.BuildBuy(accounts, argsObj)
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
	cmd.Flags().Uint64Var(&baseOut, "base-out", 0, "base amount out")
	cmd.Flags().Uint64Var(&maxQuoteIn, "max-quote-in", 0, "max quote amount in")
	cmd.Flags().BoolVar(&trackVolume, "track-volume", true, "track volume flag")
	cmd.Flags().StringVar(&poolStr, "pool", "", "pool pubkey")
	cmd.Flags().StringVar(&userStr, "user", "", "user pubkey (signer)")
	cmd.Flags().StringVar(&overridePath, "override-json", "", "optional partial accounts override json")
	_ = cmd.MarkFlagRequired("pool")
	_ = cmd.MarkFlagRequired("user")
	_ = cmd.MarkFlagRequired("base-out")
	_ = cmd.MarkFlagRequired("max-quote-in")
	return cmd
}

func newPumpAMMAdminUpdateFeeCmd(opts *globalOpts) *cobra.Command {
	var (
		lpFeeBps        uint64
		protocolFeeBps  uint64
		coinCreatorBps  uint64
		recipientsStr   string
		adminCreatorStr string
		accountsJSON    string
	)
	cmd := &cobra.Command{
		Use:   "admin-update-fee-config",
		Short: "Admin: update fee config",
		RunE: func(cmd *cobra.Command, args []string) error {
			if accountsJSON == "" {
				return fmt.Errorf("accounts-json is required")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()
			deps, err := newBuilder(cmd, opts)
			if err != nil {
				return err
			}
			accts, err := loadAccountsJSON[pumpamm.UpdateFeeConfigAccounts](accountsJSON)
			if err != nil {
				return err
			}
			if isZeroPK(accts.EventAuthority) {
				if pk, _, err := pumpamm.DeriveUpdateFeeConfigEventAuthorityPDA(accts, pumpamm.UpdateFeeConfigArgs{}); err == nil {
					accts.EventAuthority = pk
				}
			}
			if isZeroPK(accts.Program) {
				accts.Program = pumpamm.ProgramKey
			}
			recipients, err := parseRecipients(recipientsStr)
			if err != nil {
				return err
			}
			adminCreator, err := parsePubkey("admin-set-coin-creator-authority", adminCreatorStr)
			if err != nil {
				return err
			}
			argsObj := pumpamm.UpdateFeeConfigArgs{
				LpFeeBasisPoints:             lpFeeBps,
				ProtocolFeeBasisPoints:       protocolFeeBps,
				ProtocolFeeRecipients:        recipients,
				CoinCreatorFeeBasisPoints:    coinCreatorBps,
				AdminSetCoinCreatorAuthority: adminCreator,
			}
			ix, err := pumpamm.BuildUpdateFeeConfig(accts, argsObj)
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
	cmd.Flags().Uint64Var(&lpFeeBps, "lp-fee-bps", 0, "LP fee bps")
	cmd.Flags().Uint64Var(&protocolFeeBps, "protocol-fee-bps", 0, "protocol fee bps")
	cmd.Flags().Uint64Var(&coinCreatorBps, "coin-creator-fee-bps", 0, "coin creator fee bps")
	cmd.Flags().StringVar(&recipientsStr, "recipients", "", "comma-separated protocol fee recipients (<=8)")
	cmd.Flags().StringVar(&adminCreatorStr, "admin-set-coin-creator-authority", "", "admin_set_coin_creator_authority pubkey")
	cmd.Flags().StringVar(&accountsJSON, "accounts-json", "", "path to accounts json (admin/global_config/event_authority/program)")
	_ = cmd.MarkFlagRequired("recipients")
	_ = cmd.MarkFlagRequired("admin-set-coin-creator-authority")
	_ = cmd.MarkFlagRequired("accounts-json")
	return cmd
}

func parseRecipients(list string) ([8]solana.PublicKey, error) {
	var out [8]solana.PublicKey
	if strings.TrimSpace(list) == "" {
		return out, fmt.Errorf("recipients required")
	}
	parts := strings.Split(list, ",")
	if len(parts) > 8 {
		return out, fmt.Errorf("max 8 recipients")
	}
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pk, err := parsePubkey(fmt.Sprintf("recipient[%d]", i), p)
		if err != nil {
			return out, err
		}
		out[i] = pk
	}
	return out, nil
}

func fillPumpAMMEventAuthorityDeposit(a *pumpamm.DepositAccounts) {
	if a == nil {
		return
	}
	if isZeroPK(a.Program) {
		a.Program = pumpamm.ProgramKey
	}
	if isZeroPK(a.EventAuthority) {
		if pk, _, err := pumpamm.DeriveDepositEventAuthorityPDA(*a, pumpamm.DepositArgs{}); err == nil {
			a.EventAuthority = pk
		}
	}
}

func fillPumpAMMEventAuthorityWithdraw(a *pumpamm.WithdrawAccounts) {
	if a == nil {
		return
	}
	if isZeroPK(a.Program) {
		a.Program = pumpamm.ProgramKey
	}
	if isZeroPK(a.EventAuthority) {
		if pk, _, err := pumpamm.DeriveWithdrawEventAuthorityPDA(*a, pumpamm.WithdrawArgs{}); err == nil {
			a.EventAuthority = pk
		}
	}
}

func fillPumpAMMBuyDefaults(a *pumpamm.BuyAccounts) {
	if a == nil {
		return
	}
	if isZeroPK(a.EventAuthority) {
		if pk, _, err := pumpamm.DeriveBuyEventAuthorityPDA(*a, pumpamm.BuyArgs{}); err == nil {
			a.EventAuthority = pk
		}
	}
	if isZeroPK(a.Program) {
		a.Program = pumpamm.ProgramKey
	}
}

func fillPumpAMMSellDefaults(a *pumpamm.SellAccounts) {
	if a == nil {
		return
	}
	if isZeroPK(a.Program) {
		a.Program = pumpamm.ProgramKey
	}
	if isZeroPK(a.EventAuthority) {
		if pk, _, err := pumpamm.DeriveSellEventAuthorityPDA(*a, pumpamm.SellArgs{}); err == nil {
			a.EventAuthority = pk
		}
	}
}
