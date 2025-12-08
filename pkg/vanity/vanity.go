// Package vanity provides vanity address generation utilities.
package vanity

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gagliardetto/solana-go"
)

// Result represents a vanity address search result.
type Result struct {
	PrivateKey solana.PrivateKey
	PublicKey  solana.PublicKey
	Attempts   uint64
	Duration   time.Duration
}

// Options configures vanity address generation.
type Options struct {
	Prefix          string        // Required prefix
	Suffix          string        // Required suffix
	Workers         int           // Number of parallel workers (default: NumCPU)
	Timeout         time.Duration // Max search time (0 = no timeout)
	CaseInsensitive bool          // Case-insensitive matching (default: false, i.e. case-sensitive)
}

// Generate searches for a keypair matching the specified criteria.
//
// Example:
//
//	result, err := vanity.Generate(ctx, vanity.Options{Suffix: "pump"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Found: %s (attempts: %d, time: %s)\n",
//	    result.PublicKey, result.Attempts, result.Duration)
func Generate(ctx context.Context, opts Options) (*Result, error) {
	if opts.Prefix == "" && opts.Suffix == "" {
		return nil, fmt.Errorf("prefix or suffix is required")
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	// Normalize for case-insensitive matching (if enabled)
	prefix := opts.Prefix
	suffix := opts.Suffix
	if opts.CaseInsensitive {
		prefix = strings.ToLower(prefix)
		suffix = strings.ToLower(suffix)
	}

	// Create context with timeout if specified
	searchCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		searchCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	var (
		found    atomic.Bool
		attempts atomic.Uint64
		result   *Result
		resultMu sync.Mutex
		wg       sync.WaitGroup
	)

	startTime := time.Now()

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for !found.Load() {
				select {
				case <-searchCtx.Done():
					return
				default:
				}

				key, err := solana.NewRandomPrivateKey()
				if err != nil {
					continue
				}

				attempts.Add(1)
				addr := key.PublicKey().String()

				// Check match
				checkAddr := addr
				if opts.CaseInsensitive {
					checkAddr = strings.ToLower(addr)
				}

				matchPrefix := prefix == "" || strings.HasPrefix(checkAddr, prefix)
				matchSuffix := suffix == "" || strings.HasSuffix(checkAddr, suffix)

				if matchPrefix && matchSuffix {
					if found.CompareAndSwap(false, true) {
						resultMu.Lock()
						result = &Result{
							PrivateKey: key,
							PublicKey:  key.PublicKey(),
							Attempts:   attempts.Load(),
							Duration:   time.Since(startTime),
						}
						resultMu.Unlock()
					}
					return
				}
			}
		}()
	}

	wg.Wait()

	if result != nil {
		return result, nil
	}

	if searchCtx.Err() != nil {
		return nil, fmt.Errorf("search cancelled after %d attempts: %w", attempts.Load(), searchCtx.Err())
	}

	return nil, fmt.Errorf("search failed after %d attempts", attempts.Load())
}

// GenerateWithSuffix is a convenience function to generate an address with specific suffix.
func GenerateWithSuffix(ctx context.Context, suffix string) (*Result, error) {
	return Generate(ctx, Options{Suffix: suffix})
}

// GenerateWithPrefix is a convenience function to generate an address with specific prefix.
func GenerateWithPrefix(ctx context.Context, prefix string) (*Result, error) {
	return Generate(ctx, Options{Prefix: prefix})
}

// EstimateDifficulty estimates the average attempts needed for a given pattern.
// Base58 has 58 possible characters.
func EstimateDifficulty(prefixLen, suffixLen int) uint64 {
	// Each character has 1/58 probability
	// Combined probability = (1/58)^(prefixLen + suffixLen)
	// Average attempts = 58^(prefixLen + suffixLen)
	total := prefixLen + suffixLen
	if total == 0 {
		return 1
	}
	result := uint64(1)
	for i := 0; i < total; i++ {
		result *= 58
	}
	return result
}
