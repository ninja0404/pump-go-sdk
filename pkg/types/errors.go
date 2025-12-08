package types

import (
	"errors"
	"fmt"

	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
)

// Common SDK errors
var (
	// Parameter validation errors
	ErrNilRPC           = errors.New("rpc client is nil")
	ErrNilSigner        = errors.New("signer is nil")
	ErrNilFeePayer      = errors.New("fee payer is nil")
	ErrZeroAmount       = errors.New("amount must be greater than 0")
	ErrZeroMaxCost      = errors.New("max cost must be greater than 0")
	ErrZeroMinOutput    = errors.New("min output must be greater than 0")
	ErrInvalidSlippage  = errors.New("slippage bps must be <= 10000")
	ErrInvalidPublicKey = errors.New("invalid public key")
	ErrNoInstructions   = errors.New("requires at least one instruction")

	// Account errors
	ErrAccountNotFound       = errors.New("account not found")
	ErrAccountNotInitialized = errors.New("account not initialized")
	ErrMintNotFound          = errors.New("mint account not found")
	ErrPoolNotFound          = errors.New("pool account not found")
	ErrBondingCurveNotFound  = errors.New("bonding curve not found")
	ErrATANotFound           = errors.New("associated token account not found")
	ErrGlobalConfigNotFound  = errors.New("global config not found")
	ErrFeeConfigNotFound     = errors.New("fee config not found")
	ErrFeeRecipientNotFound  = errors.New("fee recipient not found")

	// Transaction errors
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrInsufficientLiquidity = errors.New("insufficient liquidity")
	ErrSlippageExceeded      = errors.New("slippage exceeded")
	ErrTransactionFailed     = errors.New("transaction failed")
	ErrSimulationFailed      = errors.New("simulation failed")
	ErrConfirmationTimeout   = errors.New("confirmation timeout")

	// Program errors
	ErrNotEnoughTokensToSell = errors.New("not enough tokens to sell")
	ErrZeroBaseAmount        = errors.New("zero base amount")
	ErrZeroQuoteAmount       = errors.New("zero quote amount")
)

// RPCError wraps RPC failures with operation context.
type RPCError struct {
	Op  string
	Err error
}

func (e RPCError) Error() string {
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

func (e RPCError) Unwrap() error {
	return e.Err
}

// ValidationError represents input validation failures.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s - %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error.
func NewValidationError(field, message string) ValidationError {
	return ValidationError{Field: field, Message: message}
}

// ProgramError represents on-chain program execution errors.
type ProgramError struct {
	Program string
	Code    int
	Message string
	Logs    []string
}

func (e ProgramError) Error() string {
	return fmt.Sprintf("program %s error [%d]: %s", e.Program, e.Code, e.Message)
}

// SimulationError contains simulation failure details.
type SimulationError struct {
	Err  interface{}
	Logs []string
}

func (e SimulationError) Error() string {
	return fmt.Sprintf("simulation failed: %v", e.Err)
}

// ParsePumpError converts pump program error code to friendly error.
func ParsePumpError(code int) error {
	if err, ok := pump.ErrorFromCode(uint32(code)); ok {
		msg := err.Msg
		if msg == "" {
			msg = err.Name
		}
		return &ProgramError{
			Program: "pump",
			Code:    code,
			Message: msg,
		}
	}
	return fmt.Errorf("pump error code %d", code)
}

// ParsePumpAmmError converts pump_amm program error code to friendly error.
func ParsePumpAmmError(code int) error {
	if err, ok := pumpamm.ErrorFromCode(uint32(code)); ok {
		msg := err.Msg
		if msg == "" {
			msg = err.Name
		}
		return &ProgramError{
			Program: "pump_amm",
			Code:    code,
			Message: msg,
		}
	}
	return fmt.Errorf("pump_amm error code %d", code)
}

// ParseSimulationError extracts error details from simulation result.
func ParseSimulationError(errVal interface{}, logs []string) error {
	if errVal == nil {
		return nil
	}

	// Try to extract instruction error
	if errMap, ok := errVal.(map[string]interface{}); ok {
		if instErr, exists := errMap["InstructionError"]; exists {
			if errSlice, ok := instErr.([]interface{}); ok && len(errSlice) >= 2 {
				if customErr, ok := errSlice[1].(map[string]interface{}); ok {
					if code, exists := customErr["Custom"]; exists {
						if codeNum, ok := code.(float64); ok {
							codeInt := int(codeNum)
							// Extract account name from logs
							account := extractAccountFromLogs(logs)
							// Parse error based on code
							msg := parseErrorCode(codeInt, account)
							return &ProgramError{
								Code:    codeInt,
								Message: msg,
								Logs:    logs,
							}
						}
					}
				}
			}
		}
	}

	return &SimulationError{Err: errVal, Logs: logs}
}

// extractAccountFromLogs extracts the account name from Anchor error logs.
func extractAccountFromLogs(logs []string) string {
	for _, log := range logs {
		// Look for "AnchorError caused by account: xxx"
		if idx := indexOf(log, "caused by account: "); idx >= 0 {
			rest := log[idx+len("caused by account: "):]
			if end := indexOf(rest, "."); end >= 0 {
				return rest[:end]
			}
			return rest
		}
	}
	return ""
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// parseErrorCode converts error code to human-readable message.
func parseErrorCode(code int, account string) string {
	// Anchor system errors (0-3000 range)
	switch code {
	case 3012:
		if account != "" {
			return fmt.Sprintf("account '%s' not initialized (create the account first)", account)
		}
		return "account not initialized"
	case 2023:
		return "token program constraint violated (wrong token program for mint)"
	case 3008:
		return "program ID was not as expected (wrong program)"
	}

	// Try pump_amm errors first (more common in AMM operations)
	if err, ok := pumpamm.ErrorFromCode(uint32(code)); ok {
		msg := err.Msg
		if msg == "" {
			msg = toReadableError(err.Name)
		}
		if account != "" && needsAccountContext(code) {
			return fmt.Sprintf("%s (account: %s)", msg, account)
		}
		return msg
	}

	// Try pump errors
	if err, ok := pump.ErrorFromCode(uint32(code)); ok {
		msg := err.Msg
		if msg == "" {
			msg = toReadableError(err.Name)
		}
		if account != "" && needsAccountContext(code) {
			return fmt.Sprintf("%s (account: %s)", msg, account)
		}
		return msg
	}

	return fmt.Sprintf("error code %d", code)
}

// needsAccountContext returns true if the error message should include account context.
func needsAccountContext(code int) bool {
	// Errors that benefit from knowing which account caused them
	switch code {
	case 6023: // NotEnoughTokensToSell
		return true
	}
	return false
}

// toReadableError converts CamelCase error name to readable format.
func toReadableError(name string) string {
	if name == "" {
		return "unknown error"
	}
	// Simple conversion: insert space before capitals
	var result []byte
	for i, c := range name {
		if i > 0 && c >= 'A' && c <= 'Z' {
			result = append(result, ' ')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// IsRetryableError checks if an error is retryable.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Network/transient errors are retryable
	if errors.Is(err, ErrSimulationFailed) {
		return true
	}
	// Program errors are not retryable
	var progErr *ProgramError
	if errors.As(err, &progErr) {
		return false
	}
	return true
}
