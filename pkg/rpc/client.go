package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/ninja0404/pump-go-sdk/pkg/config"
)

// Client wraps solana-go rpc.Client with retry, timeout, and rate limiting.
type Client struct {
	raw     *solanarpc.Client
	cfg     config.RPCConfig
	limiter *rate.Limiter
	log     zerolog.Logger
}

// NewClient builds a configured Client.
func NewClient(cfg config.RPCConfig) *Client {
	endpoint := cfg.ResolveRPCURL()
	rpcClient := solanarpc.New(endpoint)

	var limiter *rate.Limiter
	if cfg.RateLimit.RPS > 0 {
		burst := cfg.RateLimit.Burst
		if burst == 0 {
			burst = int(cfg.RateLimit.RPS * 2)
		}
		limiter = rate.NewLimiter(rate.Limit(cfg.RateLimit.RPS), burst)
	}

	log := cfg.Logger
	if log.GetLevel() == zerolog.NoLevel {
		log = zerolog.Nop()
	}

	return &Client{
		raw:     rpcClient,
		cfg:     cfg,
		limiter: limiter,
		log:     log,
	}
}

// Raw exposes the underlying solana-go client.
func (c *Client) Raw() *solanarpc.Client {
	return c.raw
}

// GetLatestBlockhash fetches the latest finalized blockhash by default.
func (c *Client) GetLatestBlockhash(ctx context.Context) (*solanarpc.GetLatestBlockhashResult, error) {
	var out *solanarpc.GetLatestBlockhashResult
	err := c.call(ctx, "getLatestBlockhash", func(ctx context.Context) error {
		var err error
		out, err = c.raw.GetLatestBlockhash(ctx, solanarpc.CommitmentType(c.cfg.Commitment))
		return err
	})
	return out, err
}

// SendTransaction submits a signed transaction.
func (c *Client) SendTransaction(ctx context.Context, tx *solana.Transaction, opts solanarpc.TransactionOpts) (solana.Signature, error) {
	var sig solana.Signature
	err := c.call(ctx, "sendTransaction", func(ctx context.Context) error {
		var err error
		sig, err = c.raw.SendTransactionWithOpts(ctx, tx, opts)
		return err
	})
	return sig, err
}

// SimulateTransaction simulates a transaction for debugging.
func (c *Client) SimulateTransaction(ctx context.Context, tx *solana.Transaction, opts *solanarpc.SimulateTransactionOpts) (*solanarpc.SimulateTransactionResponse, error) {
	var res *solanarpc.SimulateTransactionResponse
	err := c.call(ctx, "simulateTransaction", func(ctx context.Context) error {
		var err error
		res, err = c.raw.SimulateTransactionWithOpts(ctx, tx, opts)
		return err
	})
	return res, err
}

func (c *Client) call(ctx context.Context, op string, fn func(context.Context) error) error {
	ctx = c.withTimeout(ctx)

	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
	}

	if !c.cfg.Retry.Enabled {
		return fn(ctx)
	}

	attempts := c.cfg.Retry.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	var err error
	for i := 0; i < attempts; i++ {
		err = fn(ctx)
		if err == nil {
			return nil
		}

		if !retryable(err) || i == attempts-1 {
			break
		}
		backoff := c.backoff(i)
		c.log.Debug().
			Str("op", op).
			Int("attempt", i+1).
			Dur("backoff", backoff).
			Err(err).
			Msg("rpc retry")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return fmt.Errorf("%s failed after %d attempts: %w", op, attempts, err)
}

func (c *Client) withTimeout(ctx context.Context) context.Context {
	if c.cfg.Timeout <= 0 {
		return ctx
	}
	ctxWithTimeout, _ := context.WithTimeout(ctx, c.cfg.Timeout)
	return ctxWithTimeout
}

func (c *Client) backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := c.cfg.Retry.InitialBackoff
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay > c.cfg.Retry.MaxBackoff && c.cfg.Retry.MaxBackoff > 0 {
			delay = c.cfg.Retry.MaxBackoff
			break
		}
	}
	if c.cfg.Retry.Jitter {
		jitter := rand.Int63n(int64(delay / 2))
		delay = delay/2 + time.Duration(jitter)
	}
	return delay
}

func retryable(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Conservative: retry on all other errors to keep liveness unless caller decides otherwise.
	return true
}
