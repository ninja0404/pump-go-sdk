package types

import (
	"github.com/gagliardetto/solana-go"
)

// ValidateBuyParams validates common buy parameters.
func ValidateBuyParams(amount, maxCost uint64) error {
	if amount == 0 {
		return NewValidationError("amount", "must be greater than 0")
	}
	if maxCost == 0 {
		return NewValidationError("maxCost", "must be greater than 0")
	}
	return nil
}

// ValidateSellParams validates common sell parameters.
func ValidateSellParams(amount, minOutput uint64) error {
	if amount == 0 {
		return NewValidationError("amount", "must be greater than 0")
	}
	if minOutput == 0 {
		return NewValidationError("minOutput", "must be greater than 0")
	}
	return nil
}

// ValidateSlippage validates slippage basis points.
func ValidateSlippage(slippageBps uint64) error {
	if slippageBps > 10000 {
		return NewValidationError("slippageBps", "must be <= 10000 (100%)")
	}
	return nil
}

// ValidatePublicKey validates a public key is not zero.
func ValidatePublicKey(name string, key solana.PublicKey) error {
	if key.IsZero() {
		return NewValidationError(name, "cannot be zero")
	}
	return nil
}

// ValidatePublicKeys validates multiple public keys.
func ValidatePublicKeys(keys map[string]solana.PublicKey) error {
	for name, key := range keys {
		if err := ValidatePublicKey(name, key); err != nil {
			return err
		}
	}
	return nil
}
