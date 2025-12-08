package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type globalOpts struct {
	rpcURL         string
	commitment     string
	feePayerPath   string
	signerEndpoint string
	skipPreflight  bool
	retryAttempts  int
	retryBackoffMs int
	rateLimitRPS   float64
	logLevel       string
	timeoutSec     int
}

func newRootCmd() *cobra.Command {
	opts := &globalOpts{}

	root := &cobra.Command{
		Use:   "pumpcli",
		Short: "Pump platform SDK CLI (pump + pump_amm)",
	}

	root.PersistentFlags().StringVar(&opts.rpcURL, "rpc-url", "", "RPC endpoint (default mainnet if empty)")
	root.PersistentFlags().StringVar(&opts.commitment, "commitment", "finalized", "RPC commitment level")
	root.PersistentFlags().StringVar(&opts.feePayerPath, "fee-payer", "", "path to solana-keygen json for fee payer")
	root.PersistentFlags().StringVar(&opts.signerEndpoint, "signer-endpoint", "", "remote signer endpoint (placeholder)")
	root.PersistentFlags().BoolVar(&opts.skipPreflight, "skip-preflight", false, "skip preflight checks")
	root.PersistentFlags().IntVar(&opts.retryAttempts, "retry-attempts", 3, "RPC retry attempts")
	root.PersistentFlags().IntVar(&opts.retryBackoffMs, "retry-backoff-ms", 150, "initial backoff in ms")
	root.PersistentFlags().Float64Var(&opts.rateLimitRPS, "rate-limit-rps", 8, "rate limit RPS (0 to disable)")
	root.PersistentFlags().StringVar(&opts.logLevel, "log-level", "info", "log level (debug|info|warn|error)")
	root.PersistentFlags().IntVar(&opts.timeoutSec, "timeout-sec", 20, "RPC timeout seconds")

	root.AddCommand(
		newConfigCmd(),
		newAccountCmd(opts),
		newPumpCmd(opts),
		newPumpAMMCmd(opts),
	)

	return root
}

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show default config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := sdkconfig.DefaultRPCConfig()
			fmt.Fprintf(cmd.OutOrStdout(), "network=%s\nrpc=%s\ncommitment=%s\n", cfg.Network, cfg.ResolveRPCURL(), cfg.Commitment)
			return nil
		},
	}
}

type runtimeDeps struct {
	builder *txbuilder.Builder
	signer  wallet.Signer
	rpc     *sdkrpc.Client
}

func newBuilder(cmd *cobra.Command, opts *globalOpts) (*runtimeDeps, error) {
	cfg := sdkconfig.DefaultRPCConfig()
	if opts != nil {
		if opts.rpcURL != "" {
			cfg.RPCURL = opts.rpcURL
		}
		if opts.commitment != "" {
			cfg.Commitment = opts.commitment
		}
		cfg.RateLimit.RPS = opts.rateLimitRPS
		cfg.Retry.MaxAttempts = opts.retryAttempts
		if opts.retryBackoffMs > 0 {
			cfg.Retry.InitialBackoff = time.Duration(opts.retryBackoffMs) * time.Millisecond
		}
		if opts.timeoutSec > 0 {
			cfg.Timeout = time.Duration(opts.timeoutSec) * time.Second
		}
	}
	level := parseLogLevel(opts.logLevel)
	cfg.Logger = zerolog.New(cmd.ErrOrStderr()).Level(level)

	client := sdkrpc.NewClient(cfg)
	commit := rpc.CommitmentType(cfg.Commitment)
	builder := txbuilder.NewBuilder(client, commit).WithSkipPreflight(opts != nil && opts.skipPreflight)

	var signer wallet.Signer
	switch {
	case opts != nil && opts.feePayerPath != "":
		local, err := wallet.NewLocalFromKeygen(opts.feePayerPath)
		if err != nil {
			return nil, err
		}
		signer = local
	case opts != nil && opts.signerEndpoint != "":
		signer = wallet.NewRemoteSigner(solana.PublicKey{}, func(ctx context.Context, message []byte) ([]byte, error) {
			return nil, fmt.Errorf("remote signer placeholder: %s", opts.signerEndpoint)
		})
	default:
		return nil, fmt.Errorf("fee payer is required (use --fee-payer)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.GetLatestBlockhash(ctx); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: rpc ping failed: %v\n", err)
	}

	return &runtimeDeps{builder: builder, signer: signer, rpc: client}, nil
}

func parseLogLevel(lvl string) zerolog.Level {
	switch strings.ToLower(lvl) {
	case "debug":
		return zerolog.DebugLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}
