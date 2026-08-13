package quote

import (
	"math"
	"testing"
)

func TestCalculatePumpBuyAmountMatchesOfficialSDKMath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		quoteIn           uint64
		realTokenReserves uint64
		want              uint64
	}{
		{name: "zero input", quoteIn: 0, realTokenReserves: 500_000, want: 0},
		{name: "fees before curve math", quoteIn: 100_000, realTokenReserves: 500_000, want: 89_685},
		{name: "caps output at real reserves", quoteIn: 100_000, realTokenReserves: 80_000, want: 80_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePumpBuyAmount(tt.quoteIn, 1_000_000, 1_000_000, tt.realTokenReserves, 150)
			if got != tt.want {
				t.Fatalf("buy output = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalculatePumpBuyAmountDoesNotOverflow(t *testing.T) {
	t.Parallel()

	got := calculatePumpBuyAmount(math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64, 150)
	if got == 0 {
		t.Fatalf("unexpected buy output: %d", got)
	}
}

func TestCalculatePumpSellAmountMatchesOfficialSDKMath(t *testing.T) {
	t.Parallel()

	got, err := calculatePumpSellAmount(100_000, 1_000_000, 1_000_000, 100, 50)
	if err != nil {
		t.Fatalf("calculate sell: %v", err)
	}
	if got != 89_544 {
		t.Fatalf("sell output = %d, want 89544", got)
	}
}
