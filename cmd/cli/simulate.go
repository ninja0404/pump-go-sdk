package main

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/spf13/cobra"

	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
)

func simulateInstruction(ctx context.Context, deps *runtimeDeps, ix solana.Instruction, commitment string) (*solanarpc.SimulateTransactionResponse, error) {
	if deps == nil || deps.builder == nil || deps.signer == nil || deps.rpc == nil {
		return nil, fmt.Errorf("runtime deps not ready")
	}
	tx, err := deps.builder.BuildTransaction(ctx, deps.signer.PublicKey(), ix)
	if err != nil {
		return nil, fmt.Errorf("build tx: %w", err)
	}
	if err := txbuilder.SignTransaction(ctx, tx, deps.signer); err != nil {
		return nil, fmt.Errorf("sign tx: %w", err)
	}
	opts := &solanarpc.SimulateTransactionOpts{
		SigVerify: true,
	}
	if commitment != "" {
		opts.Commitment = solanarpc.CommitmentType(commitment)
	}
	return deps.rpc.SimulateTransaction(ctx, tx, opts)
}

func printSimResult(cmd *cobra.Command, res *solanarpc.SimulateTransactionResponse) {
	if res == nil || res.Value == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "no simulation result\n")
		return
	}
	if res.Value.Err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "simulation error: %v\n", res.Value.Err)
	}
	if len(res.Value.Logs) > 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "logs:")
		for _, l := range res.Value.Logs {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", l)
		}
	}
}
