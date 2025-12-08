package constants

import "github.com/gagliardetto/solana-go"

// Well-known program IDs
var (
	// SPL Programs
	SystemProgramID          = solana.SystemProgramID
	TokenProgramID           = solana.TokenProgramID
	Token2022ProgramID       = solana.MustPublicKeyFromBase58("TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb")
	AssociatedTokenProgramID = solana.SPLAssociatedTokenAccountProgramID
	SysvarRentProgramID      = solana.SysVarRentPubkey
	MetadataProgramID        = solana.MustPublicKeyFromBase58("metaqbxxUerdq28cj1RbAWkYQm3ybzjb6a8bt518x1s")

	// Pump.fun Program
	PumpProgramID    = solana.MustPublicKeyFromBase58("6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P")
	PumpFeeProgramID = solana.MustPublicKeyFromBase58("pfeeUxB6jkeY1Hxd7CsFCAjcbHA9rWtchMGdZ6VojVZ")

	// Pump AMM Program
	PumpAmmProgramID    = solana.MustPublicKeyFromBase58("pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA")
	PumpAmmFeeProgramID = PumpFeeProgramID // same as pump fee program
)

// Mainnet well-known accounts
var (
	// WSOL (Native Mint)
	WSOLMint = solana.WrappedSol
)

// PDA seeds
const (
	SeedGlobal                  = "global"
	SeedBondingCurve            = "bonding-curve"
	SeedCreatorVault            = "creator-vault"
	SeedMintAuthority           = "mint-authority"
	SeedEventAuthority          = "__event_authority"
	SeedGlobalVolumeAccumulator = "global_volume_accumulator"
	SeedUserVolumeAccumulator   = "user_volume_accumulator"
	SeedGlobalConfig            = "global_config"
	SeedCreatorVaultAmm         = "creator_vault"
)
