// Package jito provides integration with Jito Block Engine for MEV-protected transactions.
//
// Jito offers enhanced transaction submission with features like:
//   - Priority transaction landing
//   - Bundle support for atomic multi-transaction execution
//   - Tip mechanism for validators
//
// For more information, see: https://github.com/jito-labs/jito-go-rpc
package jito

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gagliardetto/solana-go"
	jitorpc "github.com/jito-labs/jito-go-rpc"
)

// Default Jito Block Engine endpoints
const (
	MainnetBlockEngine = "https://mainnet.block-engine.jito.wtf/api/v1"
	TestnetBlockEngine = "https://testnet.block-engine.jito.wtf/api/v1"
)

// MainnetBlockEngines contains all available Jito mainnet endpoints.
// Using multiple endpoints helps avoid rate limiting.
var MainnetBlockEngines = []string{
	"https://mainnet.block-engine.jito.wtf/api/v1",
	"https://amsterdam.mainnet.block-engine.jito.wtf/api/v1",
	"https://frankfurt.mainnet.block-engine.jito.wtf/api/v1",
	"https://ny.mainnet.block-engine.jito.wtf/api/v1",
	"https://tokyo.mainnet.block-engine.jito.wtf/api/v1",
}

// Pre-defined Jito tip accounts (mainnet)
// These are the official Jito tip accounts that rarely change.
// Using these avoids RPC calls and rate limiting issues.
var MainnetTipAccounts = []solana.PublicKey{
	solana.MustPublicKeyFromBase58("96gYZGLnJYVFmbjzopPSU6QiEV5fGqZNyN9nmNhvrZU5"),
	solana.MustPublicKeyFromBase58("HFqU5x63VTqvQss8hp11i4wVV8bD44PvwucfZ2bU7gRe"),
	solana.MustPublicKeyFromBase58("Cw8CFyM9FkoMi7K7Crf6HNQqf4uEMzpKw6QNghXLvLkY"),
	solana.MustPublicKeyFromBase58("ADaUMid9yfUytqMBgopwjb2DTLSokTSzL1zt6iGPaS49"),
	solana.MustPublicKeyFromBase58("DfXygSm4jCyNCybVYYK6DwvWqjKee8pbDmJGcLWNDXjh"),
	solana.MustPublicKeyFromBase58("ADuUkR4vqLUMWXxW9gh6D6L8pMSawimctcNZ5pGwDcEt"),
	solana.MustPublicKeyFromBase58("DttWaMuVvTiduZRnguLF7jNxTgiMBZ1hyAumKUiL2KRL"),
	solana.MustPublicKeyFromBase58("3AVi9Tg9Uo68tJfuvoKvqKNWKkC5wPdSSdeBnizKZ6jT"),
}

// GetRandomTipAccountLocal returns a random tip account from the pre-defined list.
// This does not make any RPC calls and avoids rate limiting.
func GetRandomTipAccountLocal() solana.PublicKey {
	return MainnetTipAccounts[rand.Intn(len(MainnetTipAccounts))]
}

// Client wraps the Jito RPC client with multi-endpoint support and retry logic.
type Client struct {
	endpoints    []string
	uuid         string
	currentIndex uint32
	maxRetries   int
	retryDelay   time.Duration
}

// NewClient creates a new Jito client with the specified endpoint.
// Use MainnetBlockEngine or TestnetBlockEngine constants, or provide a custom URL.
// uuid is optional - pass empty string if not needed.
func NewClient(endpoint string, uuid string) *Client {
	if endpoint == "" {
		endpoint = MainnetBlockEngine
	}
	return &Client{
		endpoints:  []string{endpoint},
		uuid:       uuid,
		maxRetries: 3,
		retryDelay: 200 * time.Millisecond,
	}
}

// NewClientWithEndpoints creates a new Jito client with multiple endpoints for load balancing.
// Endpoints are tried in round-robin fashion, with automatic failover on rate limiting.
// uuid is optional - pass empty string if not needed.
//
// Example:
//
//	client := jito.NewClientWithEndpoints(jito.MainnetBlockEngines, "")
func NewClientWithEndpoints(endpoints []string, uuid string) *Client {
	if len(endpoints) == 0 {
		endpoints = MainnetBlockEngines
	}
	return &Client{
		endpoints:  endpoints,
		uuid:       uuid,
		maxRetries: len(endpoints) + 2, // Try all endpoints plus some retries
		retryDelay: 100 * time.Millisecond,
	}
}

// WithRetries configures the number of retries and delay between retries.
func (c *Client) WithRetries(maxRetries int, retryDelay time.Duration) *Client {
	c.maxRetries = maxRetries
	c.retryDelay = retryDelay
	return c
}

// getNextClient returns a client for the next endpoint in round-robin fashion.
func (c *Client) getNextClient() *jitorpc.JitoJsonRpcClient {
	idx := atomic.AddUint32(&c.currentIndex, 1)
	endpoint := c.endpoints[int(idx)%len(c.endpoints)]
	return jitorpc.NewJitoJsonRpcClient(endpoint, c.uuid)
}

// isRateLimitError checks if the error is a rate limit error.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "Rate limit") ||
		strings.Contains(errStr, "congested") ||
		strings.Contains(errStr, "429")
}

// GetTipAccounts returns the list of tip accounts that can receive tips.
// Tips are used to incentivize validators to include your transaction.
func (c *Client) GetTipAccounts(ctx context.Context) ([]solana.PublicKey, error) {
	var lastErr error
	for i := 0; i < c.maxRetries; i++ {
		client := c.getNextClient()
		rawResp, err := client.GetTipAccounts()
		if err != nil {
			lastErr = err
			if isRateLimitError(err) {
				time.Sleep(c.retryDelay)
				continue
			}
			return nil, fmt.Errorf("get tip accounts: %w", err)
		}

		var accounts []string
		if err := json.Unmarshal(rawResp, &accounts); err != nil {
			return nil, fmt.Errorf("unmarshal tip accounts: %w", err)
		}

		result := make([]solana.PublicKey, 0, len(accounts))
		for _, acc := range accounts {
			pk, err := solana.PublicKeyFromBase58(acc)
			if err != nil {
				continue
			}
			result = append(result, pk)
		}
		return result, nil
	}
	return nil, fmt.Errorf("get tip accounts failed after %d retries: %w", c.maxRetries, lastErr)
}

// GetRandomTipAccount returns a random tip account for tipping.
// This makes an RPC call to get fresh tip accounts.
// If you encounter rate limiting, use GetRandomTipAccountLocal() instead.
func (c *Client) GetRandomTipAccount(ctx context.Context) (solana.PublicKey, error) {
	var lastErr error
	for i := 0; i < c.maxRetries; i++ {
		client := c.getNextClient()
		tipAcc, err := client.GetRandomTipAccount()
		if err != nil {
			lastErr = err
			if isRateLimitError(err) {
				time.Sleep(c.retryDelay)
				continue
			}
			return solana.PublicKey{}, fmt.Errorf("get random tip account: %w", err)
		}
		return solana.PublicKeyFromBase58(tipAcc.Address)
	}
	return solana.PublicKey{}, fmt.Errorf("get random tip account failed after %d retries: %w", c.maxRetries, lastErr)
}

// GetRandomTipAccountLocal returns a random tip account from the pre-defined list.
// This does not make any RPC calls and avoids rate limiting issues.
// Recommended for most use cases.
func (c *Client) GetRandomTipAccountLocal() solana.PublicKey {
	return GetRandomTipAccountLocal()
}

// SendResult contains the result of sending a transaction via Jito.
type SendResult struct {
	Signature solana.Signature
	BundleID  string
}

// SendTransaction sends a single transaction via Jito Block Engine.
// The transaction should be fully signed before calling this method.
// Automatically retries on rate limiting with endpoint rotation.
// Returns signature and bundle ID for confirmation.
func (c *Client) SendTransaction(ctx context.Context, tx *solana.Transaction) (solana.Signature, error) {
	result, err := c.SendTransactionWithBundleID(ctx, tx)
	if err != nil {
		return solana.Signature{}, err
	}
	return result.Signature, nil
}

// SendTransactionWithBundleID sends a transaction and returns both signature and bundle ID.
// Use the bundle ID with WaitForBundleConfirmation for faster confirmation.
func (c *Client) SendTransactionWithBundleID(ctx context.Context, tx *solana.Transaction) (SendResult, error) {
	// Serialize transaction to base64
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		return SendResult{}, fmt.Errorf("marshal transaction: %w", err)
	}
	txBase64 := base64.StdEncoding.EncodeToString(txBytes)

	var lastErr error
	for i := 0; i < c.maxRetries; i++ {
		client := c.getNextClient()

		// Send as a single-transaction bundle
		rawResp, err := client.SendBundle([][]string{{txBase64}})
		if err != nil {
			lastErr = err
			if isRateLimitError(err) {
				time.Sleep(c.retryDelay)
				continue
			}
			return SendResult{}, fmt.Errorf("jito send transaction: %w", err)
		}

		// Parse bundle ID response
		var bundleID string
		if err = json.Unmarshal(rawResp, &bundleID); err != nil {
			return SendResult{}, fmt.Errorf("unmarshal bundle response: %w", err)
		}

		// Return the first signature from the transaction
		var sig solana.Signature
		if len(tx.Signatures) > 0 {
			sig = tx.Signatures[0]
		}
		return SendResult{Signature: sig, BundleID: bundleID}, nil
	}
	return SendResult{}, fmt.Errorf("jito send transaction failed after %d retries: %w", c.maxRetries, lastErr)
}

// WaitForBundleConfirmation waits for a bundle to be confirmed via Jito.
// This is faster than waiting for RPC confirmation as Jito knows immediately
// when a bundle is landed.
func (c *Client) WaitForBundleConfirmation(ctx context.Context, bundleID string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			statuses, err := c.GetBundleStatuses(ctx, []string{bundleID})
			if err != nil {
				// Retry on error (might be rate limited)
				continue
			}
			if statuses == nil || len(statuses.Value) == 0 {
				continue
			}
			status := statuses.Value[0]
			// Check confirmation status
			switch status.ConfirmationStatus {
			case "confirmed", "finalized":
				return nil
			}
			// Check for errors
			if status.Err.Ok == nil {
				// Bundle failed
				return fmt.Errorf("bundle failed: %v", status.Err)
			}
		}
	}
}

// SendBundle sends multiple transactions as an atomic bundle via Jito Block Engine.
// All transactions in the bundle will either all succeed or all fail together.
// Transactions should be fully signed before calling this method.
// Returns the bundle ID.
// Automatically retries on rate limiting with endpoint rotation.
func (c *Client) SendBundle(ctx context.Context, txs []*solana.Transaction) (string, error) {
	if len(txs) == 0 {
		return "", fmt.Errorf("bundle requires at least one transaction")
	}

	// Serialize all transactions to base64
	txStrings := make([]string, 0, len(txs))
	for _, tx := range txs {
		txBytes, err := tx.MarshalBinary()
		if err != nil {
			return "", fmt.Errorf("marshal transaction: %w", err)
		}
		txStrings = append(txStrings, base64.StdEncoding.EncodeToString(txBytes))
	}

	var lastErr error
	for i := 0; i < c.maxRetries; i++ {
		client := c.getNextClient()

		rawResp, err := client.SendBundle([][]string{txStrings})
		if err != nil {
			lastErr = err
			if isRateLimitError(err) {
				time.Sleep(c.retryDelay)
				continue
			}
			return "", fmt.Errorf("jito send bundle: %w", err)
		}

		var bundleID string
		if err := json.Unmarshal(rawResp, &bundleID); err != nil {
			return "", fmt.Errorf("unmarshal bundle response: %w", err)
		}
		return bundleID, nil
	}
	return "", fmt.Errorf("jito send bundle failed after %d retries: %w", c.maxRetries, lastErr)
}

// GetBundleStatuses returns the statuses of submitted bundles.
func (c *Client) GetBundleStatuses(ctx context.Context, bundleIDs []string) (*jitorpc.BundleStatusResponse, error) {
	var lastErr error
	for i := 0; i < c.maxRetries; i++ {
		client := c.getNextClient()
		statuses, err := client.GetBundleStatuses(bundleIDs)
		if err != nil {
			lastErr = err
			if isRateLimitError(err) {
				time.Sleep(c.retryDelay)
				continue
			}
			return nil, fmt.Errorf("get bundle statuses: %w", err)
		}
		return statuses, nil
	}
	return nil, fmt.Errorf("get bundle statuses failed after %d retries: %w", c.maxRetries, lastErr)
}

// GetInflightBundleStatuses returns the statuses of in-flight bundles.
func (c *Client) GetInflightBundleStatuses(ctx context.Context, bundleIDs []string) (json.RawMessage, error) {
	var lastErr error
	for i := 0; i < c.maxRetries; i++ {
		client := c.getNextClient()
		statuses, err := client.GetInflightBundleStatuses(bundleIDs)
		if err != nil {
			lastErr = err
			if isRateLimitError(err) {
				time.Sleep(c.retryDelay)
				continue
			}
			return nil, fmt.Errorf("get inflight bundle statuses: %w", err)
		}
		return statuses, nil
	}
	return nil, fmt.Errorf("get inflight bundle statuses failed after %d retries: %w", c.maxRetries, lastErr)
}
