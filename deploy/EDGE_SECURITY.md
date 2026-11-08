# Edge and HTTP Ingress Security

Sub2API supports long-lived SSE and WebSocket requests. Protect the request
ingress without imposing a response `WriteTimeout`: a write deadline would
terminate healthy long generations and streams.

## Application defaults

- `server.max_header_bytes: 65536` limits HTTP/1 request headers to 64 KiB;
  Go maps it to the corresponding HTTP/2 header-list limit.
- `server.read_header_timeout: 10` bounds slow-header attacks. It does not
  limit request processing or response streaming.
- `server.max_request_body_size: 268435456` is the absolute 256 MiB safety net.
- `gateway.max_body_size: 268435456` remains available to multimodal, Gemini,
  image, video, and batch-image endpoints.
- `gateway.text_max_body_size: 33554432` limits the known pure-text
  `/embeddings` and `/alpha/search` endpoints to 32 MiB.
- H2C defaults to 50 concurrent streams per connection, a 2 MiB connection
  upload window, and a 512 KiB stream upload window.
- Invalid credential abuse is limited in process by trusted client IP (IPv6
  `/64`): 120 failures per 60 seconds followed by a 60-second block. This is a
  per-instance safety net; multi-instance enforcement still belongs at the
  load balancer, CDN, or WAF.

Do not add a single application-wide request semaphore: an SSE request may
legitimately occupy it for many minutes. Apply connection and unauthenticated
request controls at the edge; authenticated user/API-key concurrency remains
the application's responsibility.

## Trusted client IPs

`security.trust_forwarded_ip_for_api_key_acl` is enabled by default for upgrade
compatibility. While enabled, forwarding headers retain their legacy precedence
for logs and security-sensitive paths, **but only when the direct TCP peer is in
`server.trusted_proxies`**. An untrusted peer is always identified by its own
address, so this switch no longer bypasses the trusted-peer gate. Custom headers
from `security.forwarded_client_ip_headers` are checked in configured order
before the built-in `CF-Connecting-IP`, `X-Real-IP`, and `X-Forwarded-For`
fallback.
Header names are case-insensitive, normalized when loaded, de-duplicated, and
limited to 16 unique valid HTTP field names. Header values must contain IP
literals; comma-separated values are supported, invalid entries are skipped,
and public addresses are preferred over private fallback addresses.

The list can be supplied in YAML or with the comma-separated environment
variable `SECURITY_FORWARDED_CLIENT_IP_HEADERS`; an explicitly empty environment
value clears YAML values. It is also editable from the admin security settings
and updates at runtime without a restart. A request snapshots the switch and
header list together, so one request cannot mix old and new settings. Custom
headers are ignored completely when the switch is disabled. In that mode Gin's
`server.trusted_proxies` chain is authoritative: configure only the exact
CIDR/IP addresses that connect directly to Sub2API. An explicit empty list
trusts no forwarded client IPs.

The former first-upgrade migration that changed a stored legacy `false` value
to `true` is disabled. Existing explicit values are retained even when
`server.trusted_proxies` is not configured; an unsafe or incomplete deployment
produces a warning rather than silently weakening the administrator's choice.
New installations persist the configured custom header list during database
initialization. Existing installations backfill a missing database value from
the YAML configuration. A hidden migration marker prevents later administrator
changes from being overwritten.
If settings cannot be read or the persisted custom-header list is malformed,
the process fails closed to trusted-proxy mode with no custom headers. If a
migration write fails, the computed mode remains active for the current process
and startup records a warning.

Compatibility mode still accepts a broader set of forwarding headers than
secure mode, including configured custom headers, so the trusted proxy list
must contain only the exact direct proxy peers. Protect the origin from direct
access in either mode. A CDN deployment must firewall the origin so only the
CDN or load balancer can reach it, and that proxy must overwrite every trusted
client-IP header rather than append an untrusted client value.

Loopback is appropriate only when the application and proxy share a network
namespace. For the bundled Compose topology, host Caddy reaches the container
through a published port and the application sees the bridge gateway instead.
Both Compose files pin `172.30.18.0/29` with gateway `172.30.18.1`; trust only
that gateway, never the entire Docker network:

```yaml
server:
  trusted_proxies:
    - 172.30.18.1/32
```

If Caddy runs as a container and connects directly to the `sub2api` service
name, the peer is Caddy's container address, not the gateway. Give Caddy a fixed
`ipv4_address` and trust only that `/32`. If the pinned subnet conflicts with
the host, change the Compose subnet, gateway, and application trusted CIDR
together.

## Nginx baseline

Define shared zones in the `http` block. Tune rates to measured legitimate
traffic; the values below are conservative starting points, not universal
capacity targets.

```nginx
limit_conn_zone $binary_remote_addr zone=sub2api_conn:20m;
limit_req_zone  $binary_remote_addr zone=sub2api_auth:20m rate=5r/s;
limit_req_zone  $binary_remote_addr zone=sub2api_api:40m rate=30r/s;
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl http2;
    server_name api.example.com;

    client_header_timeout 10s;
    client_max_body_size 256m;
    large_client_header_buffers 4 16k;
    limit_conn sub2api_conn 40;

    location ~ ^/(auth|api/auth)/ {
        limit_req zone=sub2api_auth burst=10 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }

    location ~ ^/(v1/)?(embeddings|alpha/search)$ {
        client_max_body_size 32m;
        limit_req zone=sub2api_api burst=60 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }

    location / {
        limit_req zone=sub2api_api burst=60 nodelay;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 1800s;
        proxy_send_timeout 1800s;
        proxy_pass http://127.0.0.1:8080;
    }
}
```

If Nginx gzip is enabled in the `http` block, keep `text/event-stream` out of
`gzip_types` and do not use `gzip_types *` for Sub2API. The
`proxy_buffering off` setting above prevents proxy buffering, but it does not
disable the gzip response filter. Use an explicit list for ordinary responses:

```nginx
gzip on;
gzip_types text/plain text/css application/json application/javascript application/xml image/svg+xml;
```

If a shared global configuration cannot exclude SSE by content type, set
`gzip off;` in the locations serving streaming API routes. This leaves gzip
available for the web UI and static assets.

Do not use an incoming `$http_x_forwarded_for` value unless Nginx real-IP
processing is restricted to explicit trusted proxy CIDRs.

## Caddy and CDN

The bundled `deploy/Caddyfile` sets a 64 KiB header limit, a 10-second header
timeout, a 256 MiB absolute body limit, and overwrites forwarded addresses from
the TCP peer. It is therefore a direct-to-Caddy baseline. Do not use its
`{remote_host}` forwarding lines unchanged behind a CDN: all clients would be
attributed to a CDN egress address, collapsing rejection aggregation and the
invalid-auth limiter onto unrelated users.

The bundled Caddy configuration leaves `flush_interval` unset so Caddy can
automatically flush `text/event-stream` responses while still propagating
client cancellation upstream. Do not set it globally: positive values can add
streaming latency, while Caddy 2.6.2's special `-1` mode also causes
reverse-proxied requests to continue after clients disconnect. The
configuration uses an explicit response content-type list for compression. Do
not replace that list with `text/*` or the shorthand `encode gzip zstd`: both
match `text/event-stream` and can buffer SSE until the response ends. Keep
streaming responses uncompressed while retaining compression for the web UI,
JSON, and static assets.

For a CDN deployment, first firewall the origin so only current CDN egress
CIDRs can connect. Then configure those exact ranges as Caddy trusted proxies
and derive upstream headers from Caddy's parsed `{client_ip}`. For example:

```caddyfile
{
	servers {
		trusted_proxies static 192.0.2.0/24 2001:db8:1234::/48
		trusted_proxies_strict
		client_ip_headers CF-Connecting-IP X-Forwarded-For
	}
}

api.example.com {
	reverse_proxy 127.0.0.1:8080 {
		header_up X-Real-IP {client_ip}
		header_up X-Forwarded-For {client_ip}
	}
}
```

Replace the documentation ranges with the CDN's published, automatically
maintained egress ranges. `CF-Connecting-IP` is safe here only because direct
origin access is blocked and Caddy trusts only those TCP peers. Configure
Sub2API `server.trusted_proxies` with the Caddy address/private subnet so the
application accepts only Caddy's rewritten headers.

### Bundled template: `deploy/Caddyfile.edgeone` (Tencent Cloud EdgeOne)

`deploy/Caddyfile` is the direct-to-Caddy baseline: it derives `X-Real-IP` from
`{remote_host}`, which behind a CDN collapses every client onto the CDN's origin
egress address. `deploy/Caddyfile.edgeone` is the CDN variant for the
EdgeOne -> Caddy -> Sub2API topology. **The two files are not interchangeable**;
do not copy the `trusted_proxies` block into `deploy/Caddyfile`, and never put
`{remote_host}` into the EdgeOne template.

Three layers must hold at once — drop any one and the hardening is cosmetic:

1. **Origin firewall** allows 80/443 only from EdgeOne origin ranges, everything
   else dropped.
2. **Caddy** trusts only those same ranges (`trusted_proxies static ...`), so
   `{client_ip}` is derived from headers only when the TCP peer really is EdgeOne.
3. **Sub2API** trusts only Caddy (`server.trusted_proxies`), and the compatibility
   header takeover is off
   (`SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=false`).

Header choice, and why it differs from the generic example above:

- The template puts `X-Forwarded-For` first and `EO-Connecting-IP` second.
  EdgeOne always sends `X-Forwarded-For` toward the origin (when the client sent
  none, EdgeOne adds it with the address of the proxy that connected to the edge
  node, which equals `EO-Connecting-IP`). With `trusted_proxies_strict` the
  right-to-left scan lands on the address EdgeOne itself observed, which the
  client cannot forge.
- `EO-Connecting-IP` is kept only as a fallback. Tencent's documentation states
  what the header *records* but does not promise that a client-supplied value is
  overwritten, so trusting it first would reintroduce exactly the spoofing path
  this hardening exists to close.
- `header_up X-Real-IP {client_ip}` / `header_up X-Forwarded-For {client_ip}`
  **replace** the incoming values (`header_up` sets, it does not append), and
  `EO-Connecting-IP`, `EO-LOG-UUID`, `EO-Bot-Tag`, `CDN-Loop` are stripped so
  the application never records CDN metadata as fact.

Origin ranges: copy the current list from the EdgeOne console under
**站点详情 -> 安全防护 -> 源站防护**. The template ships RFC 5737 / RFC 3849
placeholders (`198.51.100.0/24`, `2001:db8:beef::/48`) which **must** be
replaced. Leaving them in place is silent: Caddy trusts no peer, `{client_ip}`
degrades to the EdgeOne egress address, and the symptom is "all users share one
IP" with no error anywhere. Tencent rotates these ranges roughly every 3–6
months with 14/7/3/1-day notices and a 14-day update window; an unnoticed
rotation breaks origin access.

### Two-stage rollout and rollback

For an existing installation, stage 1 keeps
`SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=true` while deploying the fixed
network and observing request logs. Stage 2 changes both the environment value
and the persisted admin setting to `false` only after Caddy and the application
agree on real client IPs. Changing only the environment does not override a
persisted database value. A plain `docker compose restart` also reuses the old
container environment; after changing `.env`, apply it with
`docker compose up -d --force-recreate sub2api`.

To roll back stage 2, first restore the admin setting to `true`, then restore
the environment value and recreate the container if needed. Keep the origin
firewall and trusted Caddy gateway `/32` in place during rollback. Do not treat
`git revert` alone as a runtime rollback.

### Verification steps (EdgeOne topology)

Run after every change to the Caddyfile, the firewall, or the application's
trust settings. All of these are read-only.

1. **Caddy parsed the config the way you think.**
   `caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile`
   Then confirm the running config really carries the ranges:
   `curl -s localhost:2019/config/ | jq '.apps.http.servers | map(.trusted_proxies)'`
   An empty or missing `trusted_proxies` means the block did not take effect.

2. **The client IP Caddy derives is the real client, not EdgeOne.**
   Hit the site from a known public address and compare Caddy's JSON log fields
   `client_ip` and `remote_ip`:
   ```bash
   jq -r 'select(.request.uri != null) | [.request.remote_ip, .client_ip, .request.host, .request.uri] | @tsv' \
     /var/log/caddy/sub2api.log | tail -20
   ```
   `remote_ip` must fall inside the EdgeOne origin ranges.
   `client_ip` must be your real public address and must differ from
   `remote_ip`. If the two are always equal and both look like EdgeOne
   addresses, `trusted_proxies` is not active.

3. **The application agrees with Caddy.**
   ```bash
   curl -s https://<your-domain>/api/v1/settings/public | jq '.data.version'
   ```
   and check the admin security settings show
   `trust_forwarded_ip_for_api_key_acl = false`. In either mode, only trusted
   direct peers may supply forwarding headers; `false` additionally disables
   custom-header takeover and uses Gin's trusted-proxy chain. Remember the
   database value wins over the environment:
   `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=false` alone does not flip an
   instance that already persisted `true`.

4. **Forged headers do not move the rate limiter.**
   Rotating `X-Real-IP` on a rate-limited endpoint must still hit the limit; the
   five concrete commands are in `project-docs/08-runbook.md`,
   "限流绕过自检". A run where 25 requests all return something other than 429 is
   a failure, not a success.

5. **Origin is not directly reachable.**
   From an address outside the EdgeOne ranges, connecting to the origin IP on
   80/443 must time out or be refused. If it answers, layer 1 is missing and
   every other layer is bypassable by talking to the origin directly.

Caddy core does not provide a general request-rate limiter; use a trusted
CDN/WAF, a supported rate-limit module, or host firewall controls.

At a CDN/WAF, configure connection limits, header/body limits, bot challenges,
and per-IP/ASN rates before traffic reaches the origin. Allow origin ingress
only from CDN egress CIDRs or a private load balancer. Keep the application port
off the public Internet.

## DDoS boundary

Application checks reduce amplification after a connection reaches Go. They
cannot absorb volumetric attacks, TLS floods, bandwidth saturation, or a large
distributed source set. Those require upstream network capacity, CDN/WAF
filtering, provider firewall rules, and origin isolation. Avoid high-cardinality
metrics or per-request database security logs during rejection storms.
