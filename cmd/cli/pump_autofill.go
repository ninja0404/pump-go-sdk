package main

import (
	"context"
	"time"

	"github.com/gagliardetto/solana-go"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
)

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

func autofillPumpCreateV2(ctx context.Context, deps *runtimeDeps, user solana.PublicKey, name, symbol, uri string, isMayhemMode, cashbackEnabled bool, quoteMint solana.PublicKey, vanitySuffix, vanityPrefix string, vanityTimeout time.Duration) (pump.CreateV2Accounts, pump.CreateV2Args, solana.Instruction, solana.PrivateKey, error) {
	var opts []autofill.Option
	opts = append(opts, autofill.WithCashbackEnabled(cashbackEnabled))
	if !quoteMint.IsZero() {
		opts = append(opts, autofill.WithQuoteMint(quoteMint))
	}
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
