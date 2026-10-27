# TB transport experiments

This isolated Go module makes the same-port, self-signed TLS and Caddy experiments
reproducible. It imports the repository's actual peer transport and Gin streaming
loop. Responses are synthetic: no model providers or credentials are required.
All sockets bind loopback ephemeral ports. Certificates are test fixtures trusted
explicitly for the experiment, without disabling certificate verification.

From the repository root, run the production transport regression tests:

```sh
go test -race ./internal/peertransport
```

Then run the isolated experiments:

```sh
cd experiments/transport
GOWORK=off go test -race -v -count=1 ./...
```

Use `GOWORK=off` because this optional module is intentionally outside the
application workspace. Caddy tests skip unless `CADDY_BIN` names an installed executable. To include them:

```sh
CADDY_BIN=/absolute/path/to/caddy GOWORK=off go test -race -v -count=1 ./...
```

Validated with Caddy v2.11.7; its release binary was checked against the published
SHA-512 checksum. No Caddy binary or certificate private key is committed. The
experiment launches Caddy with a temporary configuration and disabled admin API,
then stops every process and listener. It does not change host trust or services.

## Coverage and limits

- A cmux experimental TB listener accepts HTTP, HTTPS and QUIC on one numbered
  TCP/UDP port, preserving legacy plaintext clients.
- Caddy's public HTTPS/H3 port proxies to that unified backend using HTTP/1.1,
  HTTPS HTTP/2, or HTTP/3. Each variant checks twenty concurrent complete SSE
  streams, backend execution counts and cancellation propagation.
- Caddy's standard TLS listener rejects same-port plaintext HTTP with 400. Its
  `http_redirect` wrapper instead returns 308 to HTTPS on the same port; trusted
  redirect-following clients preserve POST and authorization. A redirect is not
  transparent plaintext API compatibility.
- Untrusted self-signed frontend certificates fail verification. Untrusted TLS
  backends produce Caddy 502; explicit CA trust enables the complete stream.
- Stopping the frontend's H3 service tests the TB selector's subsequent-request
  TCP fallback without replaying an uncertain business request.
- Caddy's explicit HTTP/3-only upstream has no automatic TCP fallback; the
  backend UDP-stop test verifies failure while its HTTP/HTTPS remain available.

cmux is confined to this module. Port dispatch is not wired into `tb start`.
The tests establish protocol compatibility and failure behavior, not throughput,
bandwidth savings, or performance under real network loss.
