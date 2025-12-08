package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"

	"github.com/ninja0404/pump-go-sdk/pkg/autofill"
	sdkconfig "github.com/ninja0404/pump-go-sdk/pkg/config"
	"github.com/ninja0404/pump-go-sdk/pkg/constants"
	"github.com/ninja0404/pump-go-sdk/pkg/program/pump"
	sdkrpc "github.com/ninja0404/pump-go-sdk/pkg/rpc"
	"github.com/ninja0404/pump-go-sdk/pkg/txbuilder"
	"github.com/ninja0404/pump-go-sdk/pkg/wallet"
)

// 示例：pump 卖出，支持传入滑点 bps 自动计算最小可接受 SOL 输出。
func main() {
	ctx := context.Background()

	const (
		mintStr     = "8GT663BCnPZ1nLFkFynZzquy3WGS9gMkugFtNKcrpump" // 替换为实际 mint
		slippageBps = uint64(200)                                    // 2% 滑点，设为 0 则直接使用 minSolOut 常量
		rpcURL      = solanarpc.MainNetBeta_RPC
	)

	// 从环境变量读取私钥
	privateKeyB58 := os.Getenv("PUMP_PRIVATE_KEY")
	if privateKeyB58 == "" {
		log.Fatal("PUMP_PRIVATE_KEY environment variable is required")
	}

	// 如果你想手动指定最小输出，可将 minSolOut >0 并将 slippageBps 设为 0。
	minSolOutManual := uint64(0)

	mint := solana.MustPublicKeyFromBase58(mintStr)

	cfg := sdkconfig.DefaultRPCConfig()
	cfg.RPCURL = rpcURL
	cfg.Timeout = 20 * time.Second
	client := sdkrpc.NewClient(cfg)
	builder := txbuilder.NewBuilder(client, solanarpc.CommitmentConfirmed)

	signer, err := wallet.NewLocalFromBase58(privateKeyB58)
	if err != nil {
		log.Fatalf("load signer from base58: %v", err)
	}
	user := signer.PublicKey()

	// 获取 mint 的 token program（支持 Token-2022）
	mintInfo, err := client.Raw().GetAccountInfo(ctx, mint)
	if err != nil || mintInfo == nil || mintInfo.Value == nil {
		log.Fatalf("fetch mint info: %v", err)
	}
	tokenProgram := mintInfo.Value.Owner

	// 读取用户持有的 base 余额，全部卖出
	userBaseATA, _, err := solana.FindProgramAddress(
		[][]byte{user[:], tokenProgram[:], mint[:]},
		constants.AssociatedTokenProgramID,
	)
	if err != nil {
		log.Fatalf("derive user ATA: %v", err)
	}
	baseIn, err := fetchTokenBalance(ctx, client, userBaseATA)
	if err != nil {
		log.Fatalf("fetch token balance: %v", err)
	}
	if baseIn == 0 {
		log.Fatalf("user has zero balance in %s", userBaseATA)
	}

	// 高阶封装：若指定手动 minSolOut 则直接用，否则自动模拟+滑点
	var (
		accounts pump.SellAccounts
		args     pump.SellArgs
		ixSend   solana.Instruction
	)
	if minSolOutManual > 0 || slippageBps == 0 {
		accts, argsBase, ixBase, err := autofill.PumpSell(ctx, client, user, mint, baseIn, minSolOutManual)
		if err != nil {
			log.Fatalf("autofill/build ix: %v", err)
		}
		accounts = accts
		args = argsBase
		ixSend = ixBase
	} else {
		accts, argsCalced, instrs, err := autofill.PumpSellWithSlippage(ctx, client, signer.PublicKey(), mint, baseIn, slippageBps)
		if err != nil {
			log.Fatalf("sell with slippage: %v", err)
		}
		accounts = accts
		args = argsCalced
		// 一键发送（包含 ATA ensure/可选 close）
		sig, err := builder.BuildSignSend(ctx, signer, nil, instrs...)
		if err != nil {
			log.Fatalf("send: %v", err)
		}
		fmt.Printf("min_sol_out=%d tx=%s\n", args.MinSolOutput, sig)
		_ = accounts
		_ = args
		return
	}

	sig, err := builder.BuildSignSend(ctx, signer, nil, ixSend)
	if err != nil {
		log.Fatalf("send: %v", err)
	}

	fmt.Printf("min_sol_out=%d tx=%s\n", args.MinSolOutput, sig)
	_ = accounts
	_ = args
}

// fetchTokenBalance 使用 RPC 获取 token 账户原始数量（Amount 字段，单位最小粒度）
func fetchTokenBalance(ctx context.Context, rpc *sdkrpc.Client, ata solana.PublicKey) (uint64, error) {
	res, err := rpc.Raw().GetTokenAccountBalance(ctx, ata, solanarpc.CommitmentConfirmed)
	if err != nil {
		return 0, err
	}
	if res == nil || res.Value == nil {
		return 0, fmt.Errorf("balance empty for %s", ata)
	}
	return strconv.ParseUint(res.Value.Amount, 10, 64)
}
