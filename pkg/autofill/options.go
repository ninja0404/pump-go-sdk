package autofill

import (
	"encoding/json"
	"io"
	"time"

	"github.com/gagliardetto/solana-go"

	"github.com/ninja0404/pump-go-sdk/pkg/jito"
)

// Options configures autofill helpers.
type Options struct {
	Overrides        map[string]solana.PublicKey
	Preview          io.Writer
	TrackVolume      bool
	VanitySuffix     string             // Vanity address suffix (e.g., "pump")
	VanityPrefix     string             // Vanity address prefix
	VanityTimeout    time.Duration      // Vanity search timeout (default: 5 minutes)
	KnownATAs        []solana.PublicKey // Skip ATA existence check for these addresses
	ExpectedQuoteOut uint64             // Skip simulation and use this as expected quote output (for sell)
	CloseBaseATA     bool               // Close base token ATA after sell (default: false)
	CloseQuoteATA    bool               // Close quote token ATA after sell for WSOL unwrap (default: false)
	JitoTipLamports  uint64             // Jito tip amount in lamports (0 = no tip)
	JitoTipAccount   solana.PublicKey   // Jito tip account (if zero, uses random from predefined list)
}

// Option functional option.
type Option func(*Options)

func WithOverrides(m map[string]solana.PublicKey) Option {
	return func(o *Options) { o.Overrides = m }
}

func WithPreview(w io.Writer) Option {
	return func(o *Options) { o.Preview = w }
}

func WithTrackVolume(v bool) Option {
	return func(o *Options) { o.TrackVolume = v }
}

// WithVanitySuffix generates a mint address ending with the specified suffix.
// Example: WithVanitySuffix("pump") generates addresses like "...pump"
func WithVanitySuffix(suffix string) Option {
	return func(o *Options) { o.VanitySuffix = suffix }
}

// WithVanityPrefix generates a mint address starting with the specified prefix.
func WithVanityPrefix(prefix string) Option {
	return func(o *Options) { o.VanityPrefix = prefix }
}

// WithVanityTimeout sets the timeout for vanity address generation.
// Default is 5 minutes if not specified.
func WithVanityTimeout(d time.Duration) Option {
	return func(o *Options) { o.VanityTimeout = d }
}

// WithKnownATAs skips ATA existence check for the specified addresses.
// Use this when you know the ATA exists (e.g., from a previous buy transaction)
// to avoid RPC state propagation delays.
//
// Example:
//
//	// After buy, use the known ATA for sell
//	autofill.PumpAmmSellWithSlippage(ctx, rpc, signer, pool, baseIn, slippage,
//	    autofill.WithKnownATAs(buyAccts.UserBaseTokenAccount),
//	)
func WithKnownATAs(atas ...solana.PublicKey) Option {
	return func(o *Options) { o.KnownATAs = append(o.KnownATAs, atas...) }
}

// WithExpectedQuoteOut skips simulation and uses the provided value as expected quote output.
// Use this when you want to sell immediately after buy without waiting for RPC state propagation.
// The expected output can be estimated from the buy simulation.
//
// Example:
//
//	// Buy returns simulated output, use it to estimate sell output
//	buyAccts, buyArgs, buyInstrs, simBaseOut, _ := autofill.PumpAmmBuyWithSol(...)
//	// After buy tx confirmed, sell immediately using estimated quote
//	// estimatedQuote ≈ quoteLamportsSpent (for small trades)
//	autofill.PumpAmmSellWithSlippage(ctx, rpc, signer, pool, tokensReceived, slippage,
//	    autofill.WithKnownATAs(buyAccts.UserBaseTokenAccount, buyAccts.UserQuoteTokenAccount),
//	    autofill.WithExpectedQuoteOut(estimatedQuote),
//	)
func WithExpectedQuoteOut(quoteOut uint64) Option {
	return func(o *Options) { o.ExpectedQuoteOut = quoteOut }
}

// WithCloseBaseATA closes the base token ATA after sell.
// Only use when you are selling ALL tokens in the account.
// The account must have zero balance after the sell for close to succeed.
func WithCloseBaseATA() Option {
	return func(o *Options) { o.CloseBaseATA = true }
}

// WithCloseQuoteATA closes the quote token ATA after sell (for WSOL unwrap).
// Use this when quote is WSOL and you want to unwrap it to native SOL.
func WithCloseQuoteATA() Option {
	return func(o *Options) { o.CloseQuoteATA = true }
}

// WithJitoTip adds a Jito tip transfer instruction at the end of the transaction.
// This is used to incentivize Jito validators to include your transaction.
// tipLamports: amount to tip in lamports (e.g., 1_000_000 = 0.001 SOL)
// Uses a random tip account from the predefined list.
//
// Example:
//
//	autofill.PumpAmmBuyWithSol(ctx, rpc, user, pool, amountSol, slippageBps,
//	    autofill.WithJitoTip(1_000_000), // 0.001 SOL tip
//	)
func WithJitoTip(tipLamports uint64) Option {
	return func(o *Options) {
		o.JitoTipLamports = tipLamports
		if o.JitoTipAccount.IsZero() {
			o.JitoTipAccount = jito.GetRandomTipAccountLocal()
		}
	}
}

// WithJitoTipAccount specifies a custom Jito tip account.
// Use this with WithJitoTip to use a specific tip account instead of a random one.
//
// Example:
//
//	autofill.PumpAmmBuyWithSol(ctx, rpc, user, pool, amountSol, slippageBps,
//	    autofill.WithJitoTip(1_000_000),
//	    autofill.WithJitoTipAccount(myTipAccount),
//	)
func WithJitoTipAccount(account solana.PublicKey) Option {
	return func(o *Options) { o.JitoTipAccount = account }
}

// MergeOverridesFromJSON merges base58 pubkeys from JSON blob into map.
func MergeOverridesFromJSON(dst map[string]solana.PublicKey, jsonBytes []byte) (map[string]solana.PublicKey, error) {
	if dst == nil {
		dst = make(map[string]solana.PublicKey)
	}
	var m map[string]string
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return nil, err
	}
	for k, v := range m {
		pk, err := solana.PublicKeyFromBase58(v)
		if err != nil {
			return nil, err
		}
		dst[k] = pk
	}
	return dst, nil
}
