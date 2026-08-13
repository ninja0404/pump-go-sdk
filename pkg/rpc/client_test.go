package rpc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/config"
)

func TestSendTransactionDoesNotRetry(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"rejected"}}`))
	}))
	defer server.Close()

	rpcConfig := config.DefaultRPCConfig()
	rpcConfig.RPCURL = server.URL
	rpcConfig.RateLimit.RPS = 0
	rpcConfig.Retry.Enabled = true
	rpcConfig.Retry.MaxAttempts = 3
	client := NewClient(rpcConfig)

	payer := solana.NewWallet()
	recipient := solana.NewWallet()
	instruction := system.NewTransferInstruction(1, payer.PublicKey(), recipient.PublicKey()).Build()
	tx, err := solana.NewTransaction(
		[]solana.Instruction{instruction},
		solana.Hash{},
		solana.TransactionPayer(payer.PublicKey()),
	)
	if err != nil {
		t.Fatalf("build transaction: %v", err)
	}
	if _, err := tx.Sign(func(publicKey solana.PublicKey) *solana.PrivateKey {
		if payer.PublicKey().Equals(publicKey) {
			return &payer.PrivateKey
		}
		return nil
	}); err != nil {
		t.Fatalf("sign transaction: %v", err)
	}

	_, err = client.SendTransaction(context.Background(), tx, solanarpc.TransactionOpts{})
	if err == nil {
		t.Fatal("expected send transaction error")
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("sendTransaction request count = %d, want 1", got)
	}
}

func TestCallStillRetriesReadOperations(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("temporary failure")
	rpcConfig := config.DefaultRPCConfig()
	rpcConfig.Timeout = 0
	rpcConfig.RateLimit.RPS = 0
	rpcConfig.Retry.Enabled = true
	rpcConfig.Retry.MaxAttempts = 3
	rpcConfig.Retry.InitialBackoff = 0
	rpcConfig.Retry.Jitter = false
	client := NewClient(rpcConfig)

	var attempts int
	err := client.call(context.Background(), "read", func(context.Context) error {
		attempts++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("call error = %v, want wrapped sentinel", err)
	}
	if attempts != 3 {
		t.Fatalf("read attempts = %d, want 3", attempts)
	}
}
