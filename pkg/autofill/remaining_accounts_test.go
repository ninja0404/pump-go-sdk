package autofill

import (
	"testing"

	"github.com/gagliardetto/solana-go"

	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
)

func TestPumpLegacyRemainingAccounts(t *testing.T) {
	t.Parallel()

	mint := solana.NewWallet().PublicKey()
	user := solana.NewWallet().PublicKey()
	buyback := solana.NewWallet().PublicKey()
	bondingCurveV2 := mustPDA(t, pump.ProgramKey, []byte(constants.SeedBondingCurveV2), mint[:])
	userVolumeAccumulator := mustPDA(t, pump.ProgramKey, []byte(constants.SeedUserVolumeAccumulator), user[:])

	t.Run("buy or non-cashback sell", func(t *testing.T) {
		metas, err := pumpLegacyRemainingAccounts(mint, user, buyback, false)
		if err != nil {
			t.Fatalf("derive remaining accounts: %v", err)
		}
		assertAccountMetas(t, metas, []expectedAccountMeta{
			{key: bondingCurveV2},
			{key: buyback, writable: true},
		})
	})

	t.Run("cashback sell", func(t *testing.T) {
		metas, err := pumpLegacyRemainingAccounts(mint, user, buyback, true)
		if err != nil {
			t.Fatalf("derive remaining accounts: %v", err)
		}
		assertAccountMetas(t, metas, []expectedAccountMeta{
			{key: userVolumeAccumulator, writable: true},
			{key: bondingCurveV2},
			{key: buyback, writable: true},
		})
	})
}

func TestPumpAmmRemainingAccounts(t *testing.T) {
	t.Parallel()

	baseMint := solana.NewWallet().PublicKey()
	quoteMint := constants.WSOLMint
	user := solana.NewWallet().PublicKey()
	coinCreator := solana.NewWallet().PublicKey()
	buyback := solana.NewWallet().PublicKey()
	userVolumeAccumulator := mustPDA(t, pumpamm.ProgramKey, []byte(constants.SeedUserVolumeAccumulator), user[:])
	userVolumeAccumulatorATA, _, err := findATAWithProgram(userVolumeAccumulator, quoteMint, constants.TokenProgramID, constants.AssociatedTokenProgramID)
	if err != nil {
		t.Fatalf("derive cashback ATA: %v", err)
	}
	poolV2 := mustPDA(t, pumpamm.ProgramKey, []byte(constants.SeedPoolV2), baseMint[:])
	buybackATA, _, err := findATAWithProgram(buyback, quoteMint, constants.TokenProgramID, constants.AssociatedTokenProgramID)
	if err != nil {
		t.Fatalf("derive buyback ATA: %v", err)
	}

	t.Run("cashback buy", func(t *testing.T) {
		metas, err := pumpAmmRemainingAccounts(baseMint, quoteMint, user, constants.TokenProgramID, coinCreator, buyback, true, false)
		if err != nil {
			t.Fatalf("derive remaining accounts: %v", err)
		}
		assertAccountMetas(t, metas, []expectedAccountMeta{
			{key: userVolumeAccumulatorATA, writable: true},
			{key: poolV2},
			{key: buyback},
			{key: buybackATA, writable: true},
		})
	})

	t.Run("cashback sell", func(t *testing.T) {
		metas, err := pumpAmmRemainingAccounts(baseMint, quoteMint, user, constants.TokenProgramID, coinCreator, buyback, true, true)
		if err != nil {
			t.Fatalf("derive remaining accounts: %v", err)
		}
		assertAccountMetas(t, metas, []expectedAccountMeta{
			{key: userVolumeAccumulatorATA, writable: true},
			{key: userVolumeAccumulator, writable: true},
			{key: poolV2},
			{key: buyback},
			{key: buybackATA, writable: true},
		})
	})

	t.Run("plain non-Pump pool", func(t *testing.T) {
		metas, err := pumpAmmRemainingAccounts(baseMint, quoteMint, user, constants.TokenProgramID, solana.PublicKey{}, buyback, false, false)
		if err != nil {
			t.Fatalf("derive remaining accounts: %v", err)
		}
		assertAccountMetas(t, metas, []expectedAccountMeta{
			{key: buyback},
			{key: buybackATA, writable: true},
		})
	})
}

func TestRandomFeeRecipientUsesOnlyConfiguredAccounts(t *testing.T) {
	t.Parallel()

	first := solana.NewWallet().PublicKey()
	second := solana.NewWallet().PublicKey()
	for i := 0; i < 32; i++ {
		got, err := randomFeeRecipient("test fee recipient", []solana.PublicKey{{}, first, {}, second})
		if err != nil {
			t.Fatalf("select fee recipient: %v", err)
		}
		if got != first && got != second {
			t.Fatalf("selected unconfigured fee recipient %s", got)
		}
	}

	if _, err := randomFeeRecipient("test fee recipient", make([]solana.PublicKey, 8)); err == nil {
		t.Fatal("expected an error when no fee recipient is configured")
	}
}

type expectedAccountMeta struct {
	key      solana.PublicKey
	writable bool
}

func assertAccountMetas(t *testing.T, got []*solana.AccountMeta, want []expectedAccountMeta) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("account count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].PublicKey != want[i].key || got[i].IsWritable != want[i].writable || got[i].IsSigner {
			t.Errorf("account[%d] = (%s, writable=%t, signer=%t), want (%s, writable=%t, signer=false)",
				i, got[i].PublicKey, got[i].IsWritable, got[i].IsSigner, want[i].key, want[i].writable)
		}
	}
}

func mustPDA(t *testing.T, program solana.PublicKey, seeds ...[]byte) solana.PublicKey {
	t.Helper()
	key, _, err := solana.FindProgramAddress(seeds, program)
	if err != nil {
		t.Fatalf("derive PDA: %v", err)
	}
	return key
}
