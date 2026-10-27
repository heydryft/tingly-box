# 自签名 Caddy 与双 TB 验证

验证时间：2026-10-27。代码基线：PR #2281，`21d21b51`。Caddy：v2.11.7。两台 TB 为独立进程，通过生产 `server.Start` 启动；云端 TB 使用 VModel，未调用真实模型或发生模型费用。

```text
普通 Anthropic 客户端 → 本地 TB → Caddy（自签名 HTTPS/HTTP/3）→ 云端 TB（HTTP）→ VModel
```

客户端与 Caddy 之间的 HTTPS 和 HTTP/3 使用同一公网端口号；Caddy 到云端 TB 使用其现有 HTTP 端口。证书仅在测试进程及子进程通过 `SSL_CERT_FILE` 信任，不更改系统证书库。Caddy 在 loopback 临时端口运行，测试结束后停止。

| 检查 | 结果 |
| --- | --- |
| 首次请求 | 本地 TB 日志观察到 HTTP/2 |
| OpenAI Chat 上游 | 观察到该 provider 的 HTTP/3；流式、非流式、用量检查通过 |
| Anthropic 上游 | 观察到该 provider 的 HTTP/3；流式、非流式、用量检查通过 |
| Caddy 关闭 H3 | 首个受影响流式请求返回 502；后续请求恢复成功，日志新增 HTTP/2 |
| 结果 | PASS，约 9.4 秒 |

关闭 H3 后出现的 502 是已发出请求的失败，不应把该请求描述成无损或透明重放。上述记录证实后续请求能够回落；它不证明所有失败都无重复执行。前期使用独立 Caddy 测试夹具还验证过不信任自签名证书时拒绝连接、受控信任后成功、取消传播以及 20 路并发。当前双 TB 测试没有重复这些独立夹具的全部检查。

本次未测公网部署、代理链、不同操作系统或弱网性能；也未证明 TB 直接监听 QUIC。测试代码及运行方法见同目录 `caddy_duo_test.go` 与 `README.md`。
