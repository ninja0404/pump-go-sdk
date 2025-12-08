package wallet

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
)

// Signer performs detached signatures for transaction messages.
type Signer interface {
	PublicKey() solana.PublicKey
	SignMessage(ctx context.Context, message []byte) (solana.Signature, error)
}

// Local wraps a local private key.
type Local struct {
	key solana.PrivateKey
}

// NewLocalFromKeygen loads a solana-keygen JSON file.
func NewLocalFromKeygen(path string) (Local, error) {
	key, err := solana.PrivateKeyFromSolanaKeygenFile(path)
	if err != nil {
		return Local{}, fmt.Errorf("load keypair: %w", err)
	}
	return Local{key: key}, nil
}

// NewLocalFromBase58 constructs a local signer from base58-encoded key.
func NewLocalFromBase58(privateKey string) (Local, error) {
	key, err := solana.PrivateKeyFromBase58(privateKey)
	if err != nil {
		return Local{}, fmt.Errorf("decode base58 key: %w", err)
	}
	return Local{key: key}, nil
}

// NewLocalFromPrivateKey constructs a local signer from existing private key.
func NewLocalFromPrivateKey(key solana.PrivateKey) Local {
	return Local{key: key}
}

// PublicKey returns the associated public key.
func (l Local) PublicKey() solana.PublicKey {
	return l.key.PublicKey()
}

// SignMessage signs the provided message bytes.
func (l Local) SignMessage(ctx context.Context, message []byte) (solana.Signature, error) {
	select {
	case <-ctx.Done():
		return solana.Signature{}, ctx.Err()
	default:
		sig, err := l.key.Sign(message)
		if err != nil {
			return solana.Signature{}, fmt.Errorf("sign message: %w", err)
		}
		return sig, nil
	}
}

// RemoteSigner signs by delegating to an external signer function.
type RemoteSigner struct {
	pub      solana.PublicKey
	SignFunc func(ctx context.Context, message []byte) ([]byte, error)
}

// NewRemoteSigner constructs a remote signer.
func NewRemoteSigner(pub solana.PublicKey, fn func(ctx context.Context, message []byte) ([]byte, error)) RemoteSigner {
	return RemoteSigner{
		pub:      pub,
		SignFunc: fn,
	}
}

// PublicKey returns the attached public key.
func (r RemoteSigner) PublicKey() solana.PublicKey {
	return r.pub
}

// SignMessage obtains a signature from the remote function.
func (r RemoteSigner) SignMessage(ctx context.Context, message []byte) (solana.Signature, error) {
	if r.SignFunc == nil {
		return solana.Signature{}, fmt.Errorf("sign func not set")
	}
	raw, err := r.SignFunc(ctx, message)
	if err != nil {
		return solana.Signature{}, fmt.Errorf("remote sign: %w", err)
	}
	if len(raw) != solana.SignatureLength {
		return solana.Signature{}, fmt.Errorf("invalid signature length: got %d", len(raw))
	}
	var sig solana.Signature
	copy(sig[:], raw)
	return sig, nil
}
