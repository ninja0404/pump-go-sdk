package quote

import (
	"fmt"
	"math/big"
)

var pumpFeeDenominator = big.NewInt(10_000)

func calculatePumpBuyAmount(quoteIn, virtualTokenReserves, virtualQuoteReserves, realTokenReserves, totalFeeBps uint64) uint64 {
	if quoteIn == 0 || virtualTokenReserves == 0 {
		return 0
	}

	effectiveQuote := new(big.Int).SetUint64(quoteIn - 1)
	effectiveQuote.Mul(effectiveQuote, pumpFeeDenominator)
	effectiveQuote.Div(effectiveQuote, new(big.Int).Add(
		new(big.Int).SetUint64(totalFeeBps),
		pumpFeeDenominator,
	))
	if effectiveQuote.Sign() == 0 {
		return 0
	}

	numerator := new(big.Int).Mul(effectiveQuote, new(big.Int).SetUint64(virtualTokenReserves))
	denominator := new(big.Int).Add(new(big.Int).SetUint64(virtualQuoteReserves), effectiveQuote)
	if denominator.Sign() == 0 {
		return 0
	}

	output := numerator.Div(numerator, denominator)
	realReserves := new(big.Int).SetUint64(realTokenReserves)
	if output.Cmp(realReserves) > 0 {
		output = realReserves
	}
	return output.Uint64()
}

func calculatePumpSellAmount(baseIn, virtualTokenReserves, virtualQuoteReserves, protocolFeeBps, creatorFeeBps uint64) (uint64, error) {
	if baseIn == 0 || virtualQuoteReserves == 0 {
		return 0, nil
	}

	numerator := new(big.Int).Mul(
		new(big.Int).SetUint64(baseIn),
		new(big.Int).SetUint64(virtualQuoteReserves),
	)
	denominator := new(big.Int).Add(
		new(big.Int).SetUint64(virtualTokenReserves),
		new(big.Int).SetUint64(baseIn),
	)
	if denominator.Sign() == 0 {
		return 0, fmt.Errorf("pump sell denominator is zero")
	}
	rawOutput := numerator.Div(numerator, denominator)

	protocolFee := ceilPumpFee(rawOutput, protocolFeeBps)
	creatorFee := ceilPumpFee(rawOutput, creatorFeeBps)
	totalFee := new(big.Int).Add(protocolFee, creatorFee)
	if totalFee.Cmp(rawOutput) > 0 {
		return 0, fmt.Errorf("pump fees exceed sell output")
	}
	return new(big.Int).Sub(rawOutput, totalFee).Uint64(), nil
}

func ceilPumpFee(amount *big.Int, basisPoints uint64) *big.Int {
	if amount.Sign() == 0 || basisPoints == 0 {
		return new(big.Int)
	}
	fee := new(big.Int).Mul(new(big.Int).Set(amount), new(big.Int).SetUint64(basisPoints))
	fee.Add(fee, new(big.Int).Sub(new(big.Int).Set(pumpFeeDenominator), big.NewInt(1)))
	return fee.Div(fee, pumpFeeDenominator)
}
