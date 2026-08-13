package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateTypesSupportsCurrentPumpIDLShapes(t *testing.T) {
	t.Parallel()

	doc := idl{Types: []idlTypeDef{
		{
			Name: "ConfigStatus",
			Type: json.RawMessage(`{"kind":"enum","variants":[{"name":"Paused"},{"name":"Active"}]}`),
		},
		{
			Name: "Pool",
			Type: json.RawMessage(`{"kind":"struct","fields":[{"name":"virtual_quote_reserves","type":"i128"},{"name":"payload","type":"bytes"}]}`),
		},
	}}

	got := generateTypes("pumpamm", doc)
	for _, want := range []string{
		"type ConfigStatus uint8",
		"ConfigStatusPaused ConfigStatus = iota",
		"ConfigStatusActive",
		"VirtualQuoteReserves bin.Int128",
		"Payload []byte",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated types missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateInstructionsSupportsRemainingAccounts(t *testing.T) {
	t.Parallel()

	doc := idl{Instructions: []idlInstruction{{
		Name:          "buy",
		Discriminator: []int{1, 2, 3, 4, 5, 6, 7, 8},
		Accounts: []idlInstrAccount{
			{Name: "user", Writable: true, Signer: true},
		},
	}}}

	got := generateInstructions("pump", doc)
	for _, want := range []string{
		"RemainingAccounts []*solana.AccountMeta",
		"metas = append(metas, a.RemainingAccounts...)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated instruction missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateAccountsRejectsMissingTypeDefinition(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected missing account type to panic")
		}
	}()
	generateAccounts("pump", idl{Accounts: []idlAccountDef{{Name: "Global"}}})
}

func TestGenerateInstructionsSupportsOptionalAccounts(t *testing.T) {
	t.Parallel()

	doc := idl{Instructions: []idlInstruction{{
		Name:          "create_fee_sharing_config",
		Discriminator: []int{1, 2, 3, 4, 5, 6, 7, 8},
		Accounts: []idlInstrAccount{
			{Name: "pool", Writable: true, Optional: true},
			{Name: "amm_program", Address: "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA", Optional: true},
		},
	}}}

	got := generateInstructions("pumpfees", doc)
	for _, want := range []string{
		"if a.Pool.IsZero()",
		"solana.NewAccountMeta(ProgramKey, false, false)",
		"solana.NewAccountMeta(a.Pool, true, false)",
		"if a.AmmProgram.IsZero()",
		"solana.NewAccountMeta(a.AmmProgram, false, false)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated optional account handling missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "defaultCreateFeeSharingConfigAmmProgram()") {
		t.Fatalf("optional fixed address must not be forced to Some:\n%s", got)
	}
}

func TestGenerateInstructionsUsesIDLIntegerWidthForPDASeeds(t *testing.T) {
	t.Parallel()

	doc := idl{Instructions: []idlInstruction{{
		Name:          "create_pool",
		Discriminator: []int{1, 2, 3, 4, 5, 6, 7, 8},
		Accounts: []idlInstrAccount{{
			Name: "pool",
			PDA: &idlPDA{Seeds: []idlSeed{{
				Kind: "arg",
				Path: "index",
			}}},
		}},
		Args: []idlArg{{
			Name: "index",
			Type: json.RawMessage(`"u16"`),
		}},
	}}}

	got := generateInstructions("pumpamm", doc)
	if !strings.Contains(got, "tmp := make([]byte, 2)") {
		t.Fatalf("u16 PDA seed must use two bytes:\n%s", got)
	}
	if !strings.Contains(got, "binary.LittleEndian.PutUint16(tmp, uint16(args.Index))") {
		t.Fatalf("u16 PDA seed must use PutUint16:\n%s", got)
	}
	if strings.Contains(got, "PutUint64(tmp, uint64(args.Index))") {
		t.Fatalf("u16 PDA seed must not be widened to eight bytes:\n%s", got)
	}
}

func TestGenerateInstructionsRequiresDecodedFieldsForNestedAccountSeeds(t *testing.T) {
	t.Parallel()

	doc := idl{
		Types: []idlTypeDef{{
			Name: "Pool",
			Type: json.RawMessage(`{"kind":"struct","fields":[{"name":"coin_creator","type":"pubkey"},{"name":"index","type":"u16"}]}`),
		}},
		Instructions: []idlInstruction{{
			Name:          "buy",
			Discriminator: []int{1, 2, 3, 4, 5, 6, 7, 8},
			Accounts: []idlInstrAccount{
				{Name: "pool"},
				{
					Name: "creator_vault",
					PDA: &idlPDA{Seeds: []idlSeed{
						{Kind: "account", Path: "pool.coin_creator"},
						{Kind: "account", Path: "pool.index"},
					}},
				},
			},
		}},
	}

	got := generateInstructions("pumpamm", doc)
	for _, want := range []string{
		"poolCoinCreator solana.PublicKey",
		"poolIndex uint16",
		"seeds = append(seeds, poolCoinCreator[:])",
		"binary.LittleEndian.PutUint16(tmp, uint16(poolIndex))",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated nested seed helper missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "seeds = append(seeds, accounts.Pool[:])") {
		t.Fatalf("nested account data seed incorrectly uses the account address:\n%s", got)
	}
}

func TestGeneratedHeaderIsDeterministic(t *testing.T) {
	t.Parallel()

	var got strings.Builder
	header(&got, "pump")
	if strings.Contains(got.String(), "Generated at") {
		t.Fatalf("generated header contains a timestamp: %s", got.String())
	}
}
