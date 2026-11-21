# RelayX Agent

RelayX 节点守护 Agent（**开源**）。

## 职责

- 节点注册与控制通道（gRPC；mTLS + 签名 / 时间戳 / nonce）。
- 探测执行（TCP / HTTP）与状态 / 网卡上报。
- 作为本机 `core-node` 的 gRPC 客户端（Unix socket）。

## 边界

- 不生成 Xray JSON、不管理 Xray 内核。
- 不内嵌闭源 SDK；只依赖开放合同仓库 [`relayx-tech/contracts`](https://github.com/relayx-tech/contracts)（`agent/v1`）。
- 独立发版，与 Panel 按合同版本兼容对齐。

## 技术栈

Go + go-zero（zrpc server；本机 core-node 的 zrpc client）。
