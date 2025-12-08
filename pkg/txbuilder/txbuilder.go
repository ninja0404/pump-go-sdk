package txbuilder

import (
	"context"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/jito"
	wraprpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// ConfirmationLevel represents transaction confirmation depth.
type ConfirmationLevel string

const (
	ConfirmationProcessed ConfirmationLevel = "processed"
	ConfirmationConfirmed ConfirmationLevel = "confirmed"
	ConfirmationFinalized ConfirmationLevel = "finalized"
)

// Builder ties together RPC, fee payer, and signing.
type Builder struct {
	client        *wraprpc.Client
	commitment    solanarpc.CommitmentType
	skipPreflight bool
	jitoClient    *jito.Client
}

// NewBuilder constructs a builder with the provided client and commitment.
func NewBuilder(client *wraprpc.Client, commitment solanarpc.CommitmentType) *Builder {
	if commitment == "" {
		commitment = solanarpc.CommitmentConfirmed
	}
	return &Builder{client: client, commitment: commitment}
}

// WithSkipPreflight configures whether to skip preflight.
func (b *Builder) WithSkipPreflight(skip bool) *Builder {
	b.skipPreflight = skip
	return b
}

// WithJito configures Jito client for MEV-protected transactions.
// Pass nil to disable Jito and use standard RPC.
func (b *Builder) WithJito(jitoClient *jito.Client) *Builder {
	b.jitoClient = jitoClient
	return b
}

// HasJito returns true if Jito client is configured.
func (b *Builder) HasJito() bool {
	return b.jitoClient != nil
}

// JitoClient returns the configured Jito client, or nil if not configured.
func (b *Builder) JitoClient() *jito.Client {
	return b.jitoClient
}

// BuildTransaction builds a transaction with fresh blockhash.
func (b *Builder) BuildTransaction(ctx context.Context, feePayer solana.PublicKey, instructions ...solana.Instruction) (*solana.Transaction, error) {
	if b.client == nil {
		return nil, fmt.Errorf("rpc client is nil")
	}
	if len(instructions) == 0 {
		return nil, fmt.Errorf("requires at least one instruction")
	}

	latest, err := b.client.GetLatestBlockhash(ctx)
	if err != nil {
		return nil, fmt.Errorf("get latest blockhash: %w", err)
	}

	builder := solana.NewTransactionBuilder().
		SetRecentBlockHash(latest.Value.Blockhash).
		SetFeePayer(feePayer)

	for _, ix := range instructions {
		builder.AddInstruction(ix)
	}

	tx, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("build transaction: %w", err)
	}
	return tx, nil
}

// SignTransaction signs using the provided signers in account-key order.
func SignTransaction(ctx context.Context, tx *solana.Transaction, signers ...wallet.Signer) error {
	if tx == nil {
		return fmt.Errorf("transaction is nil")
	}
	required := int(tx.Message.Header.NumRequiredSignatures)
	if required == 0 {
		return nil
	}
	if len(tx.Message.AccountKeys) < required {
		return fmt.Errorf("not enough account keys for required signatures")
	}

	signerMap := make(map[string]wallet.Signer, len(signers))
	for _, s := range signers {
		signerMap[s.PublicKey().String()] = s
	}

	messageBytes, err := tx.Message.MarshalBinary()
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}

	tx.Signatures = make([]solana.Signature, required)
	for i := 0; i < required; i++ {
		pk := tx.Message.AccountKeys[i]
		signer, ok := signerMap[pk.String()]
		if !ok {
			return fmt.Errorf("missing signer for %s", pk.String())
		}
		sig, err := signer.SignMessage(ctx, messageBytes)
		if err != nil {
			return fmt.Errorf("sign message for %s: %w", pk.String(), err)
		}
		tx.Signatures[i] = sig
	}
	return nil
}

// Send sends a signed transaction.
// If Jito client is configured, uses Jito Block Engine; otherwise uses standard RPC.
func (b *Builder) Send(ctx context.Context, tx *solana.Transaction) (solana.Signature, error) {
	// Use Jito if configured
	if b.jitoClient != nil {
		return b.SendViaJito(ctx, tx)
	}
	return b.SendViaRPC(ctx, tx)
}

// SendViaRPC sends a signed transaction via standard RPC.
func (b *Builder) SendViaRPC(ctx context.Context, tx *solana.Transaction) (solana.Signature, error) {
	if b.client == nil {
		return solana.Signature{}, fmt.Errorf("rpc client is nil")
	}
	opts := solanarpc.TransactionOpts{
		SkipPreflight:       b.skipPreflight,
		PreflightCommitment: b.commitment,
	}
	sig, err := b.client.SendTransaction(ctx, tx, opts)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("send transaction: %w", err)
	}
	return sig, nil
}

// SendViaJito sends a signed transaction via Jito Block Engine.
// This provides MEV protection and potentially faster inclusion.
func (b *Builder) SendViaJito(ctx context.Context, tx *solana.Transaction) (solana.Signature, error) {
	if b.jitoClient == nil {
		return solana.Signature{}, fmt.Errorf("jito client is not configured")
	}
	sig, err := b.jitoClient.SendTransaction(ctx, tx)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("jito send transaction: %w", err)
	}
	return sig, nil
}

// SendViaJitoAndConfirm sends via Jito and waits for bundle confirmation.
func (b *Builder) SendViaJitoAndConfirm(ctx context.Context, tx *solana.Transaction) (solana.Signature, error) {
	if b.jitoClient == nil {
		return solana.Signature{}, fmt.Errorf("jito client is not configured")
	}
	result, err := b.jitoClient.SendTransactionWithBundleID(ctx, tx)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("jito send transaction: %w", err)
	}
	// Wait for bundle confirmation via Jito
	if err = b.jitoClient.WaitForBundleConfirmation(ctx, result.BundleID); err != nil {
		return result.Signature, fmt.Errorf("jito confirmation failed: %w, signature: %v", err, result.Signature)
	}
	return result.Signature, nil
}

// SendBundleViaJito sends multiple transactions as an atomic bundle via Jito.
// All transactions will either all succeed or all fail together.
func (b *Builder) SendBundleViaJito(ctx context.Context, txs []*solana.Transaction) (string, error) {
	if b.jitoClient == nil {
		return "", fmt.Errorf("jito client is not configured")
	}
	bundleID, err := b.jitoClient.SendBundle(ctx, txs)
	if err != nil {
		return "", fmt.Errorf("jito send bundle: %w", err)
	}
	return bundleID, nil
}

// BuildSignSend builds, signs, and sends a transaction.
func (b *Builder) BuildSignSend(ctx context.Context, feePayer wallet.Signer, signers []wallet.Signer, instructions ...solana.Instruction) (solana.Signature, error) {
	if feePayer == nil {
		return solana.Signature{}, fmt.Errorf("fee payer is required")
	}
	tx, err := b.BuildTransaction(ctx, feePayer.PublicKey(), instructions...)
	if err != nil {
		return solana.Signature{}, err
	}
	allSigners := append([]wallet.Signer{feePayer}, signers...)
	if err := SignTransaction(ctx, tx, allSigners...); err != nil {
		return solana.Signature{}, err
	}
	return b.Send(ctx, tx)
}

// SendAndConfirm sends a signed transaction and waits for confirmation.
// Uses Jito for sending if configured, but always uses standard RPC for confirmation
// (Jito's GetBundleStatuses is unreliable).
func (b *Builder) SendAndConfirm(ctx context.Context, tx *solana.Transaction, level ConfirmationLevel) (solana.Signature, error) {
	// Send via Jito or RPC
	sig, err := b.Send(ctx, tx)
	if err != nil {
		return solana.Signature{}, err
	}
	// Always use standard RPC for confirmation (more reliable)
	if err = b.WaitForConfirmation(ctx, sig, level); err != nil {
		return sig, fmt.Errorf("confirmation failed: %w, sig: %v", err, sig)
	}
	return sig, nil
}

// BuildSignSendAndConfirm builds, signs, sends, and waits for confirmation.
func (b *Builder) BuildSignSendAndConfirm(ctx context.Context, feePayer wallet.Signer, signers []wallet.Signer, level ConfirmationLevel, instructions ...solana.Instruction) (solana.Signature, error) {
	if feePayer == nil {
		return solana.Signature{}, fmt.Errorf("fee payer is required")
	}
	tx, err := b.BuildTransaction(ctx, feePayer.PublicKey(), instructions...)
	if err != nil {
		return solana.Signature{}, err
	}
	allSigners := append([]wallet.Signer{feePayer}, signers...)
	if err = SignTransaction(ctx, tx, allSigners...); err != nil {
		return solana.Signature{}, err
	}
	return b.SendAndConfirm(ctx, tx, level)
}

// WaitForConfirmation polls transaction status until confirmed or timeout.
func (b *Builder) WaitForConfirmation(ctx context.Context, sig solana.Signature, level ConfirmationLevel) error {
	if b.client == nil {
		return fmt.Errorf("rpc client is nil")
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := b.client.Raw().GetSignatureStatuses(ctx, true, sig)
			if err != nil {
				continue // retry on transient errors
			}
			if resp == nil || len(resp.Value) == 0 || resp.Value[0] == nil {
				continue // not yet visible
			}
			status := resp.Value[0]
			if status.Err != nil {
				return fmt.Errorf("transaction failed: %v", status.Err)
			}
			// check confirmation level
			switch level {
			case ConfirmationProcessed:
				return nil // any status means processed
			case ConfirmationConfirmed:
				if status.ConfirmationStatus == solanarpc.ConfirmationStatusConfirmed ||
					status.ConfirmationStatus == solanarpc.ConfirmationStatusFinalized {
					return nil
				}
			case ConfirmationFinalized:
				if status.ConfirmationStatus == solanarpc.ConfirmationStatusFinalized {
					return nil
				}
			default:
				return nil
			}
		}
	}
}

func toCommitment(level ConfirmationLevel) solanarpc.CommitmentType {
	switch level {
	case ConfirmationProcessed:
		return solanarpc.CommitmentProcessed
	case ConfirmationConfirmed:
		return solanarpc.CommitmentConfirmed
	case ConfirmationFinalized:
		return solanarpc.CommitmentFinalized
	default:
		return solanarpc.CommitmentConfirmed
	}
}
