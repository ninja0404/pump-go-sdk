package autofill

import (
	"context"
	"reflect"
	"strings"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
)

// applyPubkeyOverrides sets exported fields from a map (key: field name or snake_case).
func applyPubkeyOverrides(target interface{}, m map[string]solana.PublicKey) {
	if len(m) == 0 {
		return
	}
	val := reflect.ValueOf(target)
	if val.Kind() != reflect.Ptr {
		panic("target must be pointer to struct")
	}
	val = reflect.Indirect(val)
	if val.Kind() != reflect.Struct {
		panic("target must be struct")
	}
	t := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		key := pickKey(field.Name, m)
		if key == "" {
			continue
		}
		if pk, ok := m[key]; ok {
			val.Field(i).Set(reflect.ValueOf(pk))
		}
	}
}

func pickKey(name string, m map[string]solana.PublicKey) string {
	candidates := []string{name, lowerCamel(name), snake(name)}
	for _, k := range candidates {
		if _, ok := m[k]; ok {
			return k
		}
	}
	return ""
}

func lowerCamel(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToLower(name[:1]) + name[1:]
}

func snake(name string) string {
	var parts []string
	cur := ""
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			parts = append(parts, strings.ToLower(cur))
			cur = string(r)
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		parts = append(parts, strings.ToLower(cur))
	}
	return strings.Join(parts, "_")
}

func isZeroPK(pk solana.PublicKey) bool {
	return pk == (solana.PublicKey{})
}

func firstNonZeroPK(list []solana.PublicKey) solana.PublicKey {
	for _, pk := range list {
		if !isZeroPK(pk) {
			return pk
		}
	}
	return solana.PublicKey{}
}

// ataRequest holds parameters for a single ATA ensure check.
type ataRequest struct {
	Payer        solana.PublicKey
	Wallet       solana.PublicKey
	Mint         solana.PublicKey
	TokenProgram solana.PublicKey
	ATAProgram   solana.PublicKey
	ATAAddr      solana.PublicKey // derived
}

// ensureATABatchResult holds both instructions and balances from ATA batch check.
type ensureATABatchResult struct {
	Instructions []solana.Instruction
	Balances     map[string]uint64 // key: ATA address string
}

// ensureATABatch checks multiple ATAs in one batch RPC call and returns create instructions for missing ones.
func ensureATABatch(ctx context.Context, rpc *sdkrpc.Client, requests []ataRequest) ([]solana.Instruction, error) {
	result, err := ensureATABatchWithBalances(ctx, rpc, requests)
	if err != nil {
		return nil, err
	}
	return result.Instructions, nil
}

// ensureATABatchWithBalances checks multiple ATAs and also returns their balances (0 for non-existent accounts).
// This avoids needing a separate fetchTokenAmount call after ensureATABatch.
func ensureATABatchWithBalances(ctx context.Context, rpc *sdkrpc.Client, requests []ataRequest) (ensureATABatchResult, error) {
	result := ensureATABatchResult{
		Balances: make(map[string]uint64),
	}
	if len(requests) == 0 {
		return result, nil
	}

	// derive ATA addresses
	addrs := make([]solana.PublicKey, len(requests))
	for i := range requests {
		ata, _, err := findATAWithProgram(requests[i].Wallet, requests[i].Mint, requests[i].TokenProgram, requests[i].ATAProgram)
		if err != nil {
			return result, err
		}
		requests[i].ATAAddr = ata
		addrs[i] = ata
	}

	// batch fetch
	amap, err := fetchAccountsBatch(ctx, rpc, addrs...)
	if err != nil {
		return result, err
	}

	// build create instructions for missing ATAs and extract balances
	for _, req := range requests {
		acc := amap[req.ATAAddr.String()]
		if acc != nil && acc.Owner.Equals(req.TokenProgram) {
			// exists - decode balance
			if acc.Data != nil {
				data := acc.Data.GetBinary()
				if len(data) > 0 {
					dec := bin.NewBinDecoder(data)
					var tokAcc token.Account
					if err := dec.Decode(&tokAcc); err == nil {
						result.Balances[req.ATAAddr.String()] = tokAcc.Amount
					}
				}
			}
			continue
		}
		// doesn't exist - create instruction, balance = 0
		result.Balances[req.ATAAddr.String()] = 0
		metas := []*solana.AccountMeta{
			solana.NewAccountMeta(req.Payer, true, true),
			solana.NewAccountMeta(req.ATAAddr, true, false),
			solana.NewAccountMeta(req.Wallet, false, false),
			solana.NewAccountMeta(req.Mint, false, false),
			solana.NewAccountMeta(constants.SystemProgramID, false, false),
			solana.NewAccountMeta(req.TokenProgram, false, false),
		}
		result.Instructions = append(result.Instructions, solana.NewInstruction(req.ATAProgram, metas, nil))
	}
	return result, nil
}

// fetchTokenAmountBatch fetches token amounts for multiple accounts in one batch RPC call.
func fetchTokenAmountBatch(ctx context.Context, rpc *sdkrpc.Client, accounts []solana.PublicKey) (map[string]uint64, error) {
	if len(accounts) == 0 {
		return map[string]uint64{}, nil
	}
	amap, err := fetchAccountsBatch(ctx, rpc, accounts...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]uint64, len(accounts))
	for _, addr := range accounts {
		key := addr.String()
		acc := amap[key]
		if acc == nil || acc.Data == nil {
			result[key] = 0
			continue
		}
		data := acc.Data.GetBinary()
		if len(data) == 0 {
			result[key] = 0
			continue
		}
		dec := bin.NewBinDecoder(data)
		var tokAcc token.Account
		if err := dec.Decode(&tokAcc); err != nil {
			result[key] = 0
			continue
		}
		result[key] = tokAcc.Amount
	}
	return result, nil
}

func findATAWithProgram(wallet, mint, tokenProgram, ataProgram solana.PublicKey) (solana.PublicKey, uint8, error) {
	return solana.FindProgramAddress([][]byte{
		wallet[:],
		tokenProgram[:],
		mint[:],
	}, ataProgram)
}

// fetchAccountsBatch pulls multiple accounts in one RPC call.
func fetchAccountsBatch(ctx context.Context, rpc *sdkrpc.Client, addrs ...solana.PublicKey) (map[string]*solanarpc.Account, error) {
	if len(addrs) == 0 {
		return map[string]*solanarpc.Account{}, nil
	}
	keys := make([]string, 0, len(addrs))
	for _, a := range addrs {
		keys = append(keys, a.String())
	}
	res, err := rpc.Raw().GetMultipleAccountsWithOpts(ctx, addrs, &solanarpc.GetMultipleAccountsOpts{
		Commitment: solanarpc.CommitmentConfirmed,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]*solanarpc.Account, len(addrs))
	for i, v := range res.Value {
		if v == nil {
			continue
		}
		out[keys[i]] = v
	}
	return out, nil
}

// buildWrapWSOL constructs transfer lamports -> ATA + sync_native.
func buildWrapWSOL(payer solana.PublicKey, wsolATA solana.PublicKey, lamports uint64) []solana.Instruction {
	if lamports == 0 {
		return nil
	}
	return []solana.Instruction{
		system.NewTransferInstruction(
			lamports,
			payer,
			wsolATA,
		).Build(),
		token.NewSyncNativeInstruction(wsolATA).Build(),
	}
}

// buildCloseAccount constructs a CloseAccount instruction for any Token Program (SPL or Token-2022).
func buildCloseAccount(account, destination, owner, tokenProgram solana.PublicKey) solana.Instruction {
	// CloseAccount instruction discriminator = 9
	data := []byte{9}
	metas := []*solana.AccountMeta{
		solana.NewAccountMeta(account, true, false),     // account to close (writable)
		solana.NewAccountMeta(destination, true, false), // destination for rent (writable)
		solana.NewAccountMeta(owner, false, true),       // owner (signer)
	}
	return solana.NewInstruction(tokenProgram, metas, data)
}
