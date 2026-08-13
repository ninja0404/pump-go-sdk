package autofill

import (
	"context"
	"testing"

	"github.com/gagliardetto/solana-go"

	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
)

func TestBuildPumpV2AccountSetUsesCurrentProtocolDerivations(t *testing.T) {
	t.Parallel()

	user := solana.NewWallet().PublicKey()
	mint := solana.NewWallet().PublicKey()
	creator := solana.NewWallet().PublicKey()
	feeRecipient := solana.NewWallet().PublicKey()
	reservedFeeRecipient := solana.NewWallet().PublicKey()
	buybackFeeRecipient := solana.NewWallet().PublicKey()
	global := pump.Global{
		FeeRecipient:         feeRecipient,
		ReservedFeeRecipient: reservedFeeRecipient,
		BuybackFeeRecipients: [8]solana.PublicKey{buybackFeeRecipient},
	}
	bondingCurve := pump.BondingCurve{Creator: creator, IsMayhemMode: true}

	accounts, err := buildPumpV2AccountSet(
		user,
		mint,
		constants.Token2022ProgramID,
		constants.WSOLMint,
		constants.TokenProgramID,
		global,
		bondingCurve,
	)
	if err != nil {
		t.Fatalf("build account set: %v", err)
	}

	wantBondingCurve := mustPDA(t, pump.ProgramKey, []byte(constants.SeedBondingCurve), mint[:])
	wantCreatorVault := mustPDA(t, pump.ProgramKey, []byte(constants.SeedCreatorVault), creator[:])
	wantSharingConfig := mustPDA(t, constants.PumpFeeProgramID, []byte(constants.SeedSharingConfig), mint[:])
	if accounts.FeeRecipient != reservedFeeRecipient {
		t.Errorf("fee recipient = %s, want reserved recipient %s", accounts.FeeRecipient, reservedFeeRecipient)
	}
	if accounts.BuybackFeeRecipient != buybackFeeRecipient {
		t.Errorf("buyback fee recipient = %s, want %s", accounts.BuybackFeeRecipient, buybackFeeRecipient)
	}
	if accounts.BondingCurve != wantBondingCurve {
		t.Errorf("bonding curve = %s, want %s", accounts.BondingCurve, wantBondingCurve)
	}
	if accounts.CreatorVault != wantCreatorVault {
		t.Errorf("creator vault = %s, want creator-derived %s", accounts.CreatorVault, wantCreatorVault)
	}
	if accounts.SharingConfig != wantSharingConfig {
		t.Errorf("sharing config = %s, want %s", accounts.SharingConfig, wantSharingConfig)
	}

	wantBaseUser, _, err := findATAWithProgram(user, mint, constants.Token2022ProgramID, constants.AssociatedTokenProgramID)
	if err != nil {
		t.Fatalf("derive base user ATA: %v", err)
	}
	wantQuoteUser, _, err := findATAWithProgram(user, constants.WSOLMint, constants.TokenProgramID, constants.AssociatedTokenProgramID)
	if err != nil {
		t.Fatalf("derive quote user ATA: %v", err)
	}
	if accounts.AssociatedBaseUser != wantBaseUser {
		t.Errorf("base user ATA = %s, want %s", accounts.AssociatedBaseUser, wantBaseUser)
	}
	if accounts.AssociatedQuoteUser != wantQuoteUser {
		t.Errorf("quote user ATA = %s, want %s", accounts.AssociatedQuoteUser, wantQuoteUser)
	}
}

func TestPumpV2QuoteMintNormalizesLegacySOLPair(t *testing.T) {
	t.Parallel()

	if got := pumpV2QuoteMint(solana.PublicKey{}); got != constants.WSOLMint {
		t.Fatalf("default quote mint = %s, want WSOL", got)
	}
	quoteMint := solana.NewWallet().PublicKey()
	if got := pumpV2QuoteMint(quoteMint); got != quoteMint {
		t.Fatalf("non-native quote mint = %s, want %s", got, quoteMint)
	}
}

func TestPumpAutofillCreateV2UsesMayhemSolVaultATA(t *testing.T) {
	t.Parallel()

	user := solana.NewWallet().PublicKey()
	mint := solana.NewWallet().PublicKey()
	accounts, err := pumpAutofillCreateV2(context.Background(), nil, user, mint, solana.PublicKey{})
	if err != nil {
		t.Fatalf("autofill create_v2: %v", err)
	}
	wantVault, _, err := findATAWithProgram(
		accounts.SolVault,
		mint,
		constants.Token2022ProgramID,
		constants.AssociatedTokenProgramID,
	)
	if err != nil {
		t.Fatalf("derive expected mayhem vault: %v", err)
	}
	if accounts.MayhemTokenVault != wantVault {
		t.Fatalf("mayhem token vault = %s, want %s", accounts.MayhemTokenVault, wantVault)
	}
	if len(accounts.RemainingAccounts) != 0 {
		t.Fatalf("native quote create_v2 has %d remaining accounts, want 0", len(accounts.RemainingAccounts))
	}
}
