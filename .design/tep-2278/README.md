# TEP-2278：部署层终止 QUIC 的端云传输

状态：设计及验证记录；关联 PR #2281。当前 TB 不直接提供 QUIC/UDP 服务端。

## 链路

```text
普通 Agent / SDK
      │ 原有 HTTP
      ▼
本地 TB ── HTTPS / TCP，或 HTTP/3 / QUIC / UDP ──▶ Caddy
  │                                                    │
  │ 可信 HTTPS 发现 + 无副作用探测                       │ HTTP
  │ 不可用时后续请求回到 TCP                             ▼
  └──────────────────────────────────────────────▶ 云端 TB → Provider
```

Caddy 在一个公网端口号的 TCP/UDP 上承载 HTTPS 和 HTTP/3，负责证书及加密终止；云端 TB 继续监听既有 HTTP 端口。Caddy 将 `/.well-known/tingly-transport` 和模型请求转发到 TB。普通客户端到本地 TB 的接口无需改变。部署层终止 QUIC 后，Caddy 到云端 TB 的内部链路是 HTTP；不能把这称为直接 TB-to-TB QUIC。

## 发现、安全与失败语义

- 本地 TB 只接受已验证 HTTPS 的同源同端口 H3 声明，后台 HEAD 探测云端 TB 的能力；探测成功后才切换后续请求。明文 HTTP 不自动升级。
- 客户端需要信任部署层证书。自签名证书应通过受控 CA/信任库配置，禁止以关闭证书验证代替信任配置。当前 TB 没有专门的证书导入 UI；实验用进程级 `SSL_CERT_FILE`。
- 不支持 H3 或 UDP 不可达时继续走 TCP；业务请求结果不明或已返回部分流时不能透明重放，避免重复生成和计费。主动取消向上游传播。
- 云端 TB 的探测端点只回答能力，不处理业务请求；模型入口仍由已有认证与策略保护。
- Caddy 的明文 HTTP 和 HTTPS 共用同一 TCP 端口时，默认 TLS 入口不提供透明明文 API。可用 308 跳转，但客户端必须支持跳转并信任证书。
- 代理到后端使用 HTTP/3 时，Caddy 的 H3-only upstream 不自动回落 HTTP/2；这里推荐的部署是 Caddy → TB 既有 HTTP。
- 原端口明文 HTTP、HTTPS、QUIC 三协议共存曾以 cmux 做独立实验；未接入 `tb start`，不属于本方案的交付。

## 复现验证

使用仓库自带的 `internal/protocoltest` 双进程环境；两个 TB 均运行真实 `server.Start`，云端使用 VModel，不调用付费模型。测试使用 Caddy v2.11.7、临时自签名证书及私有临时配置，不修改系统信任库或已有服务。

```sh
cp .design/tep-2278/caddy_duo_test.go internal/protocoltest/caddy_duo_tmp_test.go
CADDY_BIN=/absolute/path/to/caddy go test ./internal/protocoltest \
  -run '^TestCaddySelfSignedTwoRealTB$' -v -count=1 -timeout=120s
# 测试后删除临时放入 internal/protocoltest 的副本；保留 .design/ 中的原件。
```

测试覆盖初始 HTTPS/TCP、自动发现 H3、OpenAI 与 Anthropic 上游路径的流式/非流式与用量断言，以及关闭 Caddy H3 后的后续请求回落。详细实测结果见 `verification.md`。网络损耗、延迟与吞吐性能尚待基准测试。
