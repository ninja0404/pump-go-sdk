# Pump IDL 来源

本目录的三份 IDL 以 Pump 官方公开仓库为唯一协议基线，不以第三方 watcher 或手写账户列表为准。

- 官方仓库：<https://github.com/pump-fun/pump-public-docs>
- 固定 commit：`9c82f61cb711b044a17f770ab8ce9f9bdf78f333`
- 同步日期：2026-08-13
- 同步时官方 npm 版本：`@pump-fun/pump-sdk@1.36.0`、`@pump-fun/pump-swap-sdk@1.19.0`

文件 SHA-256：

```text
b90bc471327f671449271d5d1d42354d1fae6f5a06502f5834459a3108138e49  pump.json
6b5c7ec4e5ef9742fa99dc57b0d75b1031b379bba02a7e1b3c5a4cad68d77e56  pump_amm.json
ea2fb5dae0252375513ff8072a111affe83a0fd4db7a2e8840b3b39bace5ec83  pump_fees.json
```

更新时应先审阅官方变更，再复制三份 IDL 并重新生成：

```bash
cp /path/to/pump-public-docs/idl/pump.json idl/pump.json
cp /path/to/pump-public-docs/idl/pump_amm.json idl/pump_amm.json
cp /path/to/pump-public-docs/idl/pump_fees.json idl/pump_fees.json
make gen
go test ./...
go test -race ./...
go vet ./...
```

生成器输出必须可复现；连续执行 `make gen` 不应产生新的 diff。

IDL 中的 Anchor 可选账户以零值 `solana.PublicKey{}` 表示 `None`；生成器会将其编码为当前程序的只读、非 signer 占位账户。要传 `Some`，必须显式设置对应公钥，不会自动回退到 IDL 中的固定地址。
