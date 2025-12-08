package config

import (
	"io"
	"time"

	"github.com/rs/zerolog"
)

// Network defines the target Solana cluster.
type Network string

const (
	NetworkMainnet Network = "mainnet"
	NetworkTestnet Network = "testnet"
	NetworkDevnet  Network = "devnet"
	NetworkCustom  Network = "custom"
)

// DefaultRPCURL returns the standard RPC endpoint for a known network.
func DefaultRPCURL(network Network) string {
	switch network {
	case NetworkMainnet:
		return "https://api.mainnet-beta.solana.com"
	case NetworkTestnet:
		return "https://api.testnet.solana.com"
	case NetworkDevnet:
		return "https://api.devnet.solana.com"
	default:
		return ""
	}
}

// RetryConfig controls RPC retry behavior.
type RetryConfig struct {
	Enabled        bool
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Jitter         bool
}

// RateLimitConfig throttles outbound RPC calls.
type RateLimitConfig struct {
	RPS   float64
	Burst int
}

// RPCConfig aggregates runtime settings for RPC usage.
type RPCConfig struct {
	Network    Network
	RPCURL     string
	Commitment string
	Timeout    time.Duration
	Retry      RetryConfig
	RateLimit  RateLimitConfig
	Logger     zerolog.Logger
}

// DefaultRPCConfig yields production-safe defaults (mainnet, finalized commitment).
func DefaultRPCConfig() RPCConfig {
	return RPCConfig{
		Network:    NetworkMainnet,
		RPCURL:     DefaultRPCURL(NetworkMainnet),
		Commitment: "finalized",
		Timeout:    20 * time.Second,
		Retry: RetryConfig{
			Enabled:        true,
			MaxAttempts:    3,
			InitialBackoff: 150 * time.Millisecond,
			MaxBackoff:     2 * time.Second,
			Jitter:         true,
		},
		RateLimit: RateLimitConfig{
			RPS:   8,
			Burst: 16,
		},
		Logger: zerolog.New(io.Discard),
	}
}

// ResolveRPCURL returns RPCURL if set, otherwise falls back to network defaults.
func (c RPCConfig) ResolveRPCURL() string {
	if c.RPCURL != "" {
		return c.RPCURL
	}
	return DefaultRPCURL(c.Network)
}
