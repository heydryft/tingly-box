# Optional TB-to-TB HTTP/3 transport

An updated local TB can automatically use HTTP/3 for a remote TB's existing
HTTPS API. Agents and ordinary OpenAI/Anthropic clients need no changes.
The original TCP/HTTP listener remains available.

## Cloud endpoint

Provide a certificate trusted by the local TB and expose the **same TCP and UDP
port** (including firewall, container publishing and load-balancer rules):

```sh
tb start --http3-addr 0.0.0.0:12581 \
  --http3-cert /etc/tingly/tls/fullchain.pem \
  --http3-key /etc/tingly/tls/privkey.pem
```

The normal `--port` listener is unchanged. The additional port serves both HTTPS
and HTTP/3 through the same routes, authentication, policies and usage recording.
Certificate paths and listen flags are process options, not persisted settings:
repeat them on `tb restart`. They are preserved when `start` daemonizes.
Certificate renewal requires a restart in this first implementation.

Configure the local TB's upstream provider base URL to the cloud's HTTPS API,
for example `https://cloud.example.com:12581/tingly/openai/v1` or the existing
Anthropic scenario endpoint. Use the existing cloud model/team credential.
Do not use a plaintext `http://` URL or disable certificate verification.

## Automatic selection

The first request uses the original TCP path. A verified HTTPS response's
same-origin, same-port `Alt-Svc: h3=":12581"; ma=300` advertisement starts one
background, uncredentialed HEAD probe to `/.well-known/tingly-transport`.
Only a successful HTTP/3 probe returning the TB capability marker enables H3
for subsequent requests. Discovery is capped at five minutes. Unsupported or
unreachable H3 is cached for one minute; probe timeout is two seconds and does
not delay ordinary TCP requests. The probe never invokes a model.

HTTP/SOCKS provider proxies retain their original transport; environment proxy
selection is also respected. API-key-style OpenAI and Anthropic TB peer paths
are covered; provider-specific OAuth transports and WebSockets keep their
existing behavior. A generic third-party H3 server without TB's probe endpoint
is not automatically selected. Reverse proxies must route the probe and the
same API resources, provide trusted TLS and UDP, and avoid buffering SSE.

To disable client-side discovery for new provider clients, start the local TB
with `TINGLY_HTTP3=off`. No cloud H3 listener starts unless the flags above are
supplied. Upstream call logs include `http_protocol` (`HTTP/1.1`, `HTTP/2.0`,
`HTTP/3.0`) so actual selection can be checked.

## Failure semantics and limitations

A failed H3 business request is **not replayed over TCP** by the transport
selector; its outcome could already include model work and billing. Later
requests return to the original TCP path during cooldown. Streaming output
cannot resume after a disconnect. Existing routing/failover policy remains a
separate application decision. No model POST is raced over two transports and
no 0-RTT model requests are enabled.

A deadline failure also starts cooldown: an unreachable UDP path can surface
only when the request times out. Explicit caller cancellation does not mark
the peer unavailable.

The optimization changes transport only. JSON/SSE semantics and model billing
are preserved, and existing protocol stages still execute. It does not compress
large prompts or guarantee lower inference time. Lossy-network performance
benchmarks remain necessary before claiming speed or bandwidth improvements.

## Same-port experiment

An isolated local experiment using cmux v0.1.5 for TCP connection dispatch
successfully served plaintext HTTP and TLS HTTPS on one TCP listener, alongside
HTTP/3 on the same numbered UDP port. It reused this package and the real Gin
streaming loop. Verified behavior includes HTTPS HTTP/2 discovery followed by
HTTP/3, unchanged legacy HTTP, 20 concurrent mixed HTTP/QUIC streams with exactly
20 business executions, and subsequent-request fallback to same-port HTTP/2
after UDP was stopped. The experiment and peer transport regression tests passed
with Go's race detector; these results are compatibility evidence, not a speed
benchmark.

This TCP dispatch experiment is not integrated into `tb start`; the current
CLI still requires the separate endpoint described above. cmux has not been
added to the application's dependencies. Plaintext `http://` clients remain on
HTTP; an enhanced TB must use the trusted `https://` URL on the shared port for
automatic H3 discovery.

## Self-signed TLS and Caddy experiments

Local tests using Caddy v2.11.7 (verified against the release SHA-512 checksum)
passed with the Go race detector for these paths:

- Direct self-signed TB HTTPS: an empty trust pool rejects the certificate;
  explicitly trusting its certificate enables HTTPS followed by HTTP/3.
- Self-signed Caddy HTTPS/HTTP3 frontend to a plaintext HTTP backend: initial
  HTTP/2 switches to HTTP/3 after the proxied capability probe. Twenty concurrent
  streams produce exactly twenty backend executions. Default SSE handling
  delivers the first event before backend completion and propagates client
  cancellation. Disabling H3 preserves subsequent-request HTTP/2 fallback
  without replaying the failed request.
- Self-signed Caddy frontend to a self-signed HTTPS backend: without backend CA
  trust Caddy returns 502 and does not invoke the backend handler. Configuring
  the upstream transport's file CA pool enables complete streams through H3.

Neither segment disables certificate verification. The experiment injects
client RootCAs explicitly; it does not add a TB certificate-import UI or CLI.
For TLS upstreams the proxy removes the backend's Alt-Svc before forwarding
the response, retaining Caddy's public-port advertisement. The well-known probe
must be routed to TB along with the API routes. No explicit flush_interval=-1
was needed for SSE.

Caddy terminates HTTPS and H3 on the same public numeric port; the backend has
its own port. The three-protocol plaintext/TLS/QUIC dispatch on the TB side
uses cmux, not Caddy's standard listener configuration.

## Both components on their own unified ports

A combined local experiment bound Caddy to one TCP/UDP port and the Gin/TB
experimental backend to another single TCP/UDP port. The backend accepted
plaintext HTTP/1.1, HTTPS HTTP/2, and HTTP/3 on its one numbered port. Client H3
streams through Caddy succeeded with each of HTTP/1.1, HTTPS HTTP/2 and HTTP/3
selected as Caddy's upstream protocol. Each variant completed twenty concurrent
streams with exactly twenty backend executions and propagated client
cancellation. Go race detection passed.

Caddy's standard TLS listener returned HTTP 400 for plaintext HTTP on the same
port. Adding the built-in http_redirect listener wrapper before the tls wrapper
returned HTTP 308 pointing to HTTPS on that same port. A trusted client following
redirects preserved the POST body and authorization and executed the backend
once. A redirect is not transparent plaintext API support: clients must follow
it and trust the HTTPS certificate. No Caddy plugin was added.

The optional Caddy HTTP/3 upstream transport is explicitly configured with
versions=["3"]. Caddy v2.11.7 requires that version to be exclusive; it does not
automatically choose HTTP/2 if the upstream's UDP path fails. The experiment
confirmed failure after stopping backend UDP while the same-port backend
HTTP/HTTPS paths remained available. This differs from the TB client's own
opportunistic transport selector. These are isolated experiments, and cmux
dispatch remains outside the production tb start path.

## 中文概要

本地 TB 需要升级以具备自动发现和 HTTP/3 传输能力；普通 Agent / SDK 客户端无需
改动。云端通过额外 HTTPS + QUIC 端口提供服务，保留原 HTTP 端口。首次请求走
TCP，后台探测成功后后续请求走 H3；不支持或 UDP 不可达时继续 TCP，失败探测
进入冷却。已提交的生成请求不会因 H3 失败自动在 TCP 重发。首期仅支持同源同
端口声明及通用 OpenAI / Anthropic peer 路径；代理、OAuth 特殊传输和 WebSocket
保留现有行为。启动和重启需提供证书、私钥、监听参数，并开放对应 TCP 与 UDP。
