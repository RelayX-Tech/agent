# agent 交付物

开源 Agent 以独立二进制交付，经本机 Unix socket 调用 core-node，并作为 Panel 控制通道的 gRPC 服务端。

## 目录布局

```text
/opt/relayx/agent/agent
/etc/relayx/agent.yaml
/var/lib/relayx/agent/       # 0700 relayx:relayx
/run/relayx/core-node.sock   # 与 core-node 共用
```

## 构建

```bash
VERSION=1.0.0 ./scripts/build.sh   # 产出 dist/agent
```

- `syft` / `cosign` 存在时生成 SBOM 与签名。
- Agent 只依赖 `github.com/relayx-tech/contracts`，不 import 闭源 core module。

## systemd

`packaging/systemd/relayx-agent.service`：`After/Requires=relayx-core-node.service`，`LimitNOFILE=65535`。
