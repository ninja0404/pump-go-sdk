package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
)

func newAccountCmd(opts *globalOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "account [pubkey]",
		Short: "Inspect an account (pump / pump_amm)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pub, err := parsePubkey("account", args[0])
			if err != nil {
				return err
			}
			cfg := sdkconfigFromOpts(opts, cmd)
			client := sdkrpc.NewClient(cfg)

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			acc, err := client.Raw().GetAccountInfo(ctx, pub)
			if err != nil {
				return fmt.Errorf("fetch account: %w", err)
			}
			if acc == nil || acc.Value == nil || acc.Value.Data == nil {
				return fmt.Errorf("account not found or empty")
			}
			data := acc.Value.Data.GetBinary()
			name, decoded, err := decodeKnownAccount(data)
			if err != nil {
				return err
			}
			bz, _ := json.MarshalIndent(decoded, "", "  ")
			fmt.Fprintf(cmd.OutOrStdout(), "account=%s program=%s\n%s\n", name, acc.Value.Owner, string(bz))
			return nil
		},
	}
}

func decodeKnownAccount(data []byte) (string, interface{}, error) {
	if len(data) < 8 {
		return "", nil, fmt.Errorf("account data too short")
	}
	decoders := []struct {
		name string
		disc []byte
		new  func() interface {
			Unmarshal([]byte) error
		}
	}{
		{"pump.BondingCurve", pump.BondingCurveDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pump.BondingCurve{} }},
		{"pump.FeeConfig", pump.FeeConfigDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pump.FeeConfig{} }},
		{"pump.Global", pump.GlobalDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pump.Global{} }},
		{"pump.GlobalVolumeAccumulator", pump.GlobalVolumeAccumulatorDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pump.GlobalVolumeAccumulator{} }},
		{"pump.UserVolumeAccumulator", pump.UserVolumeAccumulatorDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pump.UserVolumeAccumulator{} }},
		{"pumpamm.BondingCurve", pumpamm.BondingCurveDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pumpamm.BondingCurve{} }},
		{"pumpamm.FeeConfig", pumpamm.FeeConfigDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pumpamm.FeeConfig{} }},
		{"pumpamm.GlobalConfig", pumpamm.GlobalConfigDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pumpamm.GlobalConfig{} }},
		{"pumpamm.GlobalVolumeAccumulator", pumpamm.GlobalVolumeAccumulatorDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pumpamm.GlobalVolumeAccumulator{} }},
		{"pumpamm.Pool", pumpamm.PoolDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pumpamm.Pool{} }},
		{"pumpamm.UserVolumeAccumulator", pumpamm.UserVolumeAccumulatorDiscriminator, func() interface{ Unmarshal([]byte) error } { return &pumpamm.UserVolumeAccumulator{} }},
	}

	for _, d := range decoders {
		if bytes.Equal(data[:8], d.disc) {
			inst := d.new()
			if err := inst.Unmarshal(data); err != nil {
				return d.name, nil, err
			}
			return d.name, inst, nil
		}
	}
	return "", nil, fmt.Errorf("unknown discriminator")
}

func sdkconfigFromOpts(opts *globalOpts, cmd *cobra.Command) sdkconfig.RPCConfig {
	cfg := sdkconfig.DefaultRPCConfig()
	if opts != nil {
		if opts.rpcURL != "" {
			cfg.RPCURL = opts.rpcURL
		}
		if opts.commitment != "" {
			cfg.Commitment = opts.commitment
		}
		if opts.rateLimitRPS > 0 {
			cfg.RateLimit.RPS = opts.rateLimitRPS
		}
		if opts.retryAttempts > 0 {
			cfg.Retry.MaxAttempts = opts.retryAttempts
		}
		if opts.retryBackoffMs > 0 {
			cfg.Retry.InitialBackoff = time.Duration(opts.retryBackoffMs) * time.Millisecond
		}
		if opts.timeoutSec > 0 {
			cfg.Timeout = time.Duration(opts.timeoutSec) * time.Second
		}
	}
	cfg.Logger = zerolog.New(cmd.ErrOrStderr()).Level(parseLogLevel(opts.logLevel))
	return cfg
}
