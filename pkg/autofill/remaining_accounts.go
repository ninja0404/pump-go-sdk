package autofill

import (
	cryptorand "crypto/rand"
	"fmt"
	"math/big"

	"github.com/gagliardetto/solana-go"

	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pumpamm"
)

func pumpLegacyRemainingAccounts(mint, user, buybackFeeRecipient solana.PublicKey, includeCashback bool) ([]*solana.AccountMeta, error) {
	if buybackFeeRecipient.IsZero() {
		return nil, fmt.Errorf("buyback fee recipient is required")
	}

	bondingCurveV2, _, err := solana.FindProgramAddress(
		[][]byte{[]byte(constants.SeedBondingCurveV2), mint[:]},
		pump.ProgramKey,
	)
	if err != nil {
		return nil, fmt.Errorf("derive bonding_curve_v2: %w", err)
	}

	metas := make([]*solana.AccountMeta, 0, 3)
	if includeCashback {
		userVolumeAccumulator, _, err := solana.FindProgramAddress(
			[][]byte{[]byte(constants.SeedUserVolumeAccumulator), user[:]},
			pump.ProgramKey,
		)
		if err != nil {
			return nil, fmt.Errorf("derive user_volume_accumulator: %w", err)
		}
		metas = append(metas, solana.NewAccountMeta(userVolumeAccumulator, true, false))
	}
	metas = append(metas,
		solana.NewAccountMeta(bondingCurveV2, false, false),
		solana.NewAccountMeta(buybackFeeRecipient, true, false),
	)
	return metas, nil
}

func pumpFeeRecipient(global pump.Global, mayhemMode bool) (solana.PublicKey, error) {
	if mayhemMode {
		return randomFeeRecipient("reserved fee recipient", append([]solana.PublicKey{global.ReservedFeeRecipient}, global.ReservedFeeRecipients[:]...))
	}
	return randomFeeRecipient("fee recipient", append([]solana.PublicKey{global.FeeRecipient}, global.FeeRecipients[:]...))
}

func pumpAmmFeeRecipient(global pumpamm.GlobalConfig, mayhemMode bool) (solana.PublicKey, error) {
	if mayhemMode {
		return randomFeeRecipient("reserved fee recipient", append([]solana.PublicKey{global.ReservedFeeRecipient}, global.ReservedFeeRecipients[:]...))
	}
	return randomFeeRecipient("protocol fee recipient", global.ProtocolFeeRecipients[:])
}

func randomFeeRecipient(name string, candidates []solana.PublicKey) (solana.PublicKey, error) {
	valid := make([]solana.PublicKey, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.IsZero() {
			valid = append(valid, candidate)
		}
	}
	if len(valid) == 0 {
		return solana.PublicKey{}, fmt.Errorf("%s not found in global config", name)
	}

	index, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(valid))))
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("select %s: %w", name, err)
	}
	return valid[index.Int64()], nil
}

func pumpAmmRemainingAccounts(
	baseMint, quoteMint, user, quoteTokenProgram, coinCreator, buybackFeeRecipient solana.PublicKey,
	includeCashback, isSell bool,
) ([]*solana.AccountMeta, error) {
	if buybackFeeRecipient.IsZero() {
		return nil, fmt.Errorf("buyback fee recipient is required")
	}

	metas := make([]*solana.AccountMeta, 0, 5)
	if includeCashback {
		userVolumeAccumulator, _, err := solana.FindProgramAddress(
			[][]byte{[]byte(constants.SeedUserVolumeAccumulator), user[:]},
			pumpamm.ProgramKey,
		)
		if err != nil {
			return nil, fmt.Errorf("derive user_volume_accumulator: %w", err)
		}
		cashbackATA, _, err := findATAWithProgram(
			userVolumeAccumulator,
			quoteMint,
			quoteTokenProgram,
			constants.AssociatedTokenProgramID,
		)
		if err != nil {
			return nil, fmt.Errorf("derive cashback quote ATA: %w", err)
		}
		metas = append(metas, solana.NewAccountMeta(cashbackATA, true, false))
		if isSell {
			metas = append(metas, solana.NewAccountMeta(userVolumeAccumulator, true, false))
		}
	}

	if !coinCreator.IsZero() {
		poolV2, _, err := solana.FindProgramAddress(
			[][]byte{[]byte(constants.SeedPoolV2), baseMint[:]},
			pumpamm.ProgramKey,
		)
		if err != nil {
			return nil, fmt.Errorf("derive pool_v2: %w", err)
		}
		metas = append(metas, solana.NewAccountMeta(poolV2, false, false))
	}

	buybackATA, _, err := findATAWithProgram(
		buybackFeeRecipient,
		quoteMint,
		quoteTokenProgram,
		constants.AssociatedTokenProgramID,
	)
	if err != nil {
		return nil, fmt.Errorf("derive buyback quote ATA: %w", err)
	}
	metas = append(metas,
		solana.NewAccountMeta(buybackFeeRecipient, false, false),
		solana.NewAccountMeta(buybackATA, true, false),
	)
	return metas, nil
}
