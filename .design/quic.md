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
awk '/^```go$/{p=1;next} /^```$/{if(p)exit} p' .design/quic.md > internal/protocoltest/caddy_duo_tmp_test.go
CADDY_BIN=/absolute/path/to/caddy go test ./internal/protocoltest \
  -run '^TestCaddySelfSignedTwoRealTB$' -v -count=1 -timeout=120s
# 测试后删除临时放入 internal/protocoltest 的副本。
```

测试覆盖初始 HTTPS/TCP、自动发现 H3、OpenAI 与 Anthropic 上游路径的流式/非流式与用量断言，以及关闭 Caddy H3 后的后续请求回落。详细实测结果见下文。网络损耗、延迟与吞吐性能尚待基准测试。

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

本次未测公网部署、代理链、不同操作系统或弱网性能；也未证明 TB 直接监听 QUIC。可复现测试代码见下方附录。

## 附录：可复现的双 TB 测试

```go
package protocoltest

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This test is installed temporarily into internal/protocoltest to reuse its
// production two-process TB harness. CADDY_BIN selects a local Caddy binary.
func TestCaddySelfSignedTwoRealTB(t *testing.T) {
	bin := os.Getenv("CADDY_BIN")
	if bin == "" {
		t.Fatal("set CADDY_BIN")
	}
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	cert := fixture.TLS.Certificates[0]
	fixture.Close()
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", certFile)
	cfg := DuoEnvConfig{BootTimeout: 60 * time.Second}
	cfg.withDefaults()
	env := &DuoEnv{client: &http.Client{Timeout: 20 * time.Second}}
	defer env.Close()
	tb1, err := env.startInstance(duoInstanceSpec{Name: "cloud", Role: duoRoleUpstream, Stream: cfg.Upstream.Stream}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	env.TB1 = tb1
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	backend := strings.TrimPrefix(tb1.BaseURL, "http://")
	makeCaddy := func(protocols []string) func() {
		config := map[string]any{"admin": map[string]any{"disabled": true}, "logging": map[string]any{"logs": map[string]any{"default": map[string]any{"level": "ERROR"}}}, "apps": map[string]any{
			"tls":  map[string]any{"certificates": map[string]any{"load_files": []any{map[string]any{"certificate": certFile, "key": keyFile}}}},
			"http": map[string]any{"servers": map[string]any{"test": map[string]any{"listen": []string{addr}, "protocols": protocols, "tls_connection_policies": []any{map[string]any{}}, "automatic_https": map[string]any{"disable": true}, "routes": []any{map[string]any{"handle": []any{map[string]any{"handler": "reverse_proxy", "upstreams": []any{map[string]any{"dial": backend}}}}}}}}},
		}}
		raw, _ := json.Marshal(config)
		file := filepath.Join(t.TempDir(), "caddy.json")
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "run", "--config", file)
		cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
			if err == nil {
				conn.Close()
				break
			}
			select {
			case err := <-done:
				t.Fatalf("Caddy exited: %v", err)
			default:
			}
			if time.Now().After(deadline) {
				cmd.Process.Kill()
				t.Fatal("Caddy not ready")
			}
			time.Sleep(10 * time.Millisecond)
		}
		stopped := false
		return func() {
			if stopped {
				return
			}
			stopped = true
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		}
	}
	stop := makeCaddy([]string{"h1", "h2", "h3"})
	defer func() { stop() }()
	// The same private self-signed CA must be trusted by the gateway process.
	tb2, err := env.startInstance(duoInstanceSpec{Name: "local", Role: duoRoleGateway, UpstreamURL: "https://" + addr, UpstreamToken: tb1.ModelToken}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	env.TB2 = tb2
	run := func(route DuoRoute) bool {
		checks := env.RunFunctionalChecks(route, 4096)
		for _, check := range checks {
			if !check.Pass {
				t.Logf("%s %s", check.Name, check.Detail)
				return false
			}
		}
		return len(checks) > 0
	}
	seen := func(provider, transport string) bool {
		for _, line := range strings.Split(tb2.OutputTail(), "\n") {
			if strings.Contains(line, "provider="+provider) && strings.Contains(line, "http_protocol="+transport) {
				return true
			}
		}
		return false
	}
	for _, route := range []DuoRoute{{Name: "v1-chat", Target: "chat"}, {Name: "v1-anthropic", Target: "anthropic"}} {
		deadline := time.Now().Add(6 * time.Second)
		for {
			if !run(route) {
				t.Fatal("two-TB streaming/non-streaming checks failed")
			}
			if seen(map[string]string{"chat": "tb1-openai-chat", "anthropic": "tb1-anthropic"}[route.Target], "HTTP/3.0") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("gateway never selected HTTP/3")
			}
			time.Sleep(30 * time.Millisecond)
		}
		t.Logf("%s: streaming, non-streaming, usage and H3 checks passed", route.Name)
	}
	if !strings.Contains(tb2.OutputTail(), "http_protocol=HTTP/2.0") {
		t.Fatal("initial HTTPS/TCP path not observed")
	}
	t.Log("gateway observed initial HTTP/2 followed by HTTP/3 through Caddy")
	beforeFallback := strings.Count(tb2.OutputTail(), "http_protocol=HTTP/2.0")
	stop()
	stop = makeCaddy([]string{"h1", "h2"})
	route := DuoRoute{Name: "v1-chat", Target: "chat"}
	// The first post-interruption request may fail; never demand implicit replay.
	// A later request must recover on the existing HTTPS/TCP path.
	deadline := time.Now().Add(12 * time.Second)
	recovered := false
	for time.Now().Before(deadline) {
		if run(route) {
			recovered = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !recovered || strings.Count(tb2.OutputTail(), "http_protocol=HTTP/2.0") <= beforeFallback {
		t.Fatal("TCP fallback did not recover on HTTP/2")
	}
	t.Log("after Caddy H3 was disabled, a subsequent request recovered via TCP")
}
```
