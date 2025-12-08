package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/gagliardetto/solana-go"
)

// parsePubkey converts base58 string to PublicKey.
func parsePubkey(label, v string) (solana.PublicKey, error) {
	if v == "" {
		return solana.PublicKey{}, fmt.Errorf("%s is required", label)
	}
	pk, err := solana.PublicKeyFromBase58(v)
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("%s invalid pubkey: %w", label, err)
	}
	return pk, nil
}

func defaultSystemProgram() solana.PublicKey {
	return solana.MustPublicKeyFromBase58("11111111111111111111111111111111")
}

func defaultTokenProgram() solana.PublicKey {
	return solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
}

func defaultAssociatedTokenProgram() solana.PublicKey {
	return solana.MustPublicKeyFromBase58("ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL")
}

func isZeroPK(pk solana.PublicKey) bool {
	return pk == (solana.PublicKey{})
}

func fetchAccountData(ctx context.Context, deps *runtimeDeps, pk solana.PublicKey) ([]byte, error) {
	if deps == nil || deps.rpc == nil {
		return nil, fmt.Errorf("rpc client not ready")
	}
	info, err := deps.rpc.Raw().GetAccountInfo(ctx, pk)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	if info == nil || info.Value == nil || info.Value.Data == nil {
		return nil, fmt.Errorf("account empty")
	}
	return info.Value.Data.GetBinary(), nil
}

// loadPubkeyMap reads a JSON map[string]string of base58 pubkeys.
func loadPubkeyMap(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read accounts json: %w", err)
	}
	var m map[string]string
	if err := json.Unmarshal(content, &m); err != nil {
		return nil, fmt.Errorf("parse accounts json: %w", err)
	}
	return m, nil
}

// applyPubkeyOverrides sets exported fields on target struct if present in map.
func applyPubkeyOverrides(target interface{}, m map[string]string) error {
	if len(m) == 0 {
		return nil
	}
	val := reflectValue(target)
	if !val.IsValid() || val.Kind() != reflect.Struct {
		return fmt.Errorf("target must be struct")
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
		pk, err := parsePubkey(field.Name, m[key])
		if err != nil {
			return err
		}
		val.Field(i).Set(reflect.ValueOf(pk))
	}
	return nil
}

// loadAccountsJSON fills a struct T from a JSON object of base58 pubkeys keyed by field name variants.
func loadAccountsJSON[T any](path string) (T, error) {
	var zero T
	content, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read accounts json: %w", err)
	}
	var m map[string]string
	if err := json.Unmarshal(content, &m); err != nil {
		return zero, fmt.Errorf("parse accounts json: %w", err)
	}

	var out T
	val := reflectValue(&out)
	if !val.IsValid() {
		return zero, fmt.Errorf("invalid accounts target")
	}
	t := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		key := pickKey(field.Name, m)
		if key == "" {
			return zero, fmt.Errorf("missing account field %s", field.Name)
		}
		pk, err := parsePubkey(field.Name, m[key])
		if err != nil {
			return zero, err
		}
		val.Field(i).Set(reflectValue(&pk))
	}
	return out, nil
}

func pickKey(name string, m map[string]string) string {
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

// reflectValue is a tiny helper to avoid importing reflect twice.
func reflectValue(v interface{}) reflect.Value {
	return reflect.Indirect(reflect.ValueOf(v))
}
