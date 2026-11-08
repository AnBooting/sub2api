// Package ip 提供客户端 IP 地址提取工具。
package ip

import (
	"net"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

const forwardedIPSettingsKey = "sub2api.forwarded_ip_settings"

type forwardedIPSettings struct {
	trustForwarded bool
	headers        []string
}

// defaultTrustedProxyCIDRs 是未显式配置 server.trusted_proxies 时唯一默认信任的
// 直连对端网段：同机反向代理。任何其它对端都必须由运维显式声明，否则一律视为
// 不可信 —— 转发头只能由「受信的代理」写入，不能靠「头里的值像公网地址」来推断。
var defaultTrustedProxyCIDRs = []string{"127.0.0.0/8", "::1/128"}

// trustedProxyNetworks 保存允许写入转发头的直连对端网段快照。
// nil 表示尚未配置，按 defaultTrustedProxyCIDRs 处理。
var trustedProxyNetworks atomic.Pointer[[]*net.IPNet]

// SetTrustedProxyPolicy 配置「允许其写入转发头的直连对端」网段（来源 server.trusted_proxies）。
// explicitlyConfigured=false 表示 server.trusted_proxies 未配置，此时只信任
// 同机反代（loopback）；显式配置为空列表表示不信任任何对端的转发头。
// 非法条目返回错误且不改变现有配置。
func SetTrustedProxyPolicy(proxies []string, explicitlyConfigured bool) error {
	sources := proxies
	if !explicitlyConfigured {
		sources = defaultTrustedProxyCIDRs
	}
	networks := make([]*net.IPNet, 0, len(sources))
	for _, entry := range sources {
		normalized := strings.TrimSpace(entry)
		if normalized == "" {
			continue
		}
		if !strings.Contains(normalized, "/") {
			parsed := net.ParseIP(normalized)
			if parsed == nil {
				return &net.ParseError{Type: "IP address or CIDR", Text: entry}
			}
			bits := 8 * net.IPv6len
			if parsed.To4() != nil {
				parsed = parsed.To4()
				bits = 8 * net.IPv4len
			}
			normalized = (&net.IPNet{IP: parsed, Mask: net.CIDRMask(bits, bits)}).String()
		}
		_, network, err := net.ParseCIDR(normalized)
		if err != nil {
			return err
		}
		networks = append(networks, network)
	}
	trustedProxyNetworks.Store(&networks)
	return nil
}

// TrustedProxyPolicy 返回当前生效的受信直连对端网段（未配置时为 loopback 默认值）。
func TrustedProxyPolicy() []*net.IPNet {
	if stored := trustedProxyNetworks.Load(); stored != nil {
		return *stored
	}
	networks, err := parseTrustedProxyNetworks(defaultTrustedProxyCIDRs)
	if err != nil {
		return nil
	}
	return networks
}

func parseTrustedProxyNetworks(entries []string) ([]*net.IPNet, error) {
	networks := make([]*net.IPNet, 0, len(entries))
	for _, entry := range entries {
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, err
		}
		networks = append(networks, network)
	}
	return networks, nil
}

// IsTrustedDirectPeer 判断直连对端地址是否落在受信网段内。
// 只有受信对端写入的转发头才可能被采信。导出以便启动期接线测试断言策略生效。
func IsTrustedDirectPeer(peerIP string) bool {
	parsed := net.ParseIP(peerIP)
	if parsed == nil {
		return false
	}
	for _, network := range TrustedProxyPolicy() {
		if network.Contains(parsed) {
			return true
		}
	}
	return false
}

// directPeerIP 返回 TCP 直连对端地址（去除端口）。无法解析时返回空串。
func directPeerIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	remoteAddr := strings.TrimSpace(c.Request.RemoteAddr)
	if remoteAddr == "" {
		return ""
	}
	peer := normalizeIP(remoteAddr)
	if net.ParseIP(peer) == nil {
		return ""
	}
	return peer
}

// SetForwardedIPSettings snapshots the forwarded-IP mode and custom header list
// for this request.
func SetForwardedIPSettings(c *gin.Context, enabled bool, headers []string) {
	if c == nil {
		return
	}
	c.Set(forwardedIPSettingsKey, forwardedIPSettings{
		trustForwarded: enabled,
		headers:        append([]string(nil), headers...),
	})
}

// SetLegacyForwardedIPTrust records whether raw forwarding headers override
// Gin's server.trusted_proxies chain for this request.
func SetLegacyForwardedIPTrust(c *gin.Context, enabled bool) {
	SetForwardedIPSettings(c, enabled, nil)
}

func requestForwardedIPSettings(c *gin.Context) (forwardedIPSettings, bool) {
	if c == nil {
		return forwardedIPSettings{}, false
	}
	value, ok := c.Get(forwardedIPSettingsKey)
	if !ok {
		return forwardedIPSettings{}, false
	}
	settings, ok := value.(forwardedIPSettings)
	return settings, ok
}

func requestUsesLegacyForwardedIPTrust(c *gin.Context) bool {
	settings, ok := requestForwardedIPSettings(c)
	return !ok || settings.trustForwarded
}

// GetClientIP resolves the client address using the legacy forwarding-header
// precedence used before the trusted-proxy hardening. It remains the
// compatibility path for request metadata and usage/error logs; security-
// sensitive callers must use GetTrustedClientIP or GetSecurityClientIP.
func GetClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if !requestUsesLegacyForwardedIPTrust(c) {
		return GetTrustedClientIP(c)
	}

	// 信任方向由「谁写的头」决定：只有受信代理的直连连接才可能承载真实的转发头。
	// 其它对端（含线上被用来轮换 X-Real-IP 的直连客户端）一律忽略所有转发头，
	// 直接返回对端地址。
	peer := directPeerIP(c)
	if peer == "" {
		// 拿不到可解析的 TCP 对端（异常 RemoteAddr）时不做「忽略全部转发头」的
		// 强判定，回落到 gin 的解析结果，避免所有此类请求聚到空身份上。
		peer = normalizeIP(c.ClientIP())
	}
	if peer == "" {
		// 连 gin 都解析不出：使用仓库既有的「未知客户端」口径（见
		// server/middleware/api_key_auth.go），绝不返回空身份。
		return unknownClientIP
	}
	if !trustsClientIPHeaders(peer) {
		return peer
	}

	settings, _ := requestForwardedIPSettings(c)
	customIP, customFallback := resolveCustomForwardedClientIP(c, settings.headers)
	if customIP != "" {
		return customIP
	}

	// Preserve the historical precedence used by existing reverse-proxy
	// deployments, while skipping an internal proxy address when a public XFF
	// value is available. This covers Docker/Nginx setups that accidentally
	// write the bridge address into X-Real-IP.
	legacyIP, legacyFallback := resolveLegacyForwardedHeaderIP(c)
	if legacyIP != "" {
		return legacyIP
	}
	if customFallback != "" {
		return customFallback
	}
	if legacyFallback != "" {
		return legacyFallback
	}
	return normalizeIP(c.ClientIP())
}

func resolveCustomForwardedClientIP(c *gin.Context, headers []string) (string, string) {
	if c == nil {
		return "", ""
	}
	var fallback string
	for _, header := range headers {
		for _, value := range c.Request.Header.Values(header) {
			for _, candidate := range strings.Split(value, ",") {
				parsed := net.ParseIP(strings.TrimSpace(candidate))
				if parsed == nil {
					continue
				}
				normalized := parsed.String()
				if isPrivateIP(normalized) {
					if fallback == "" {
						fallback = normalized
					}
					continue
				}
				return normalized, fallback
			}
		}
	}
	return "", fallback
}

// resolveLegacyForwardedHeaderIP 在「直连对端已受信」的前提下选择客户端地址。
//
// 注意信任边界：受信只能说明**代理**可信，不能说明请求里每一个头都由代理写入。
// 典型反代基线（deploy/Caddyfile）只覆盖 X-Real-IP 与 X-Forwarded-For，
// 客户端自带的其它头会原样穿过。因此这里必须：
//   - 只读代理基线会覆盖的头：X-Real-IP、X-Forwarded-For；
//   - CF-Connecting-IP 不再默认读取（Caddy/Nginx 基线都不覆盖它），只有运维在
//     security.forwarded_client_ip_headers 里显式声明时才读，这正是该配置项的语义；
//   - X-Forwarded-For 从右往左遍历（与 gin 的 validateHeader 一致）：最右侧是代理
//     追加的真实对端，左侧全部是客户端可伪造的内容。
func resolveLegacyForwardedHeaderIP(c *gin.Context) (string, string) {
	var fallback string
	if realIP := normalizeValidIP(c.GetHeader("X-Real-IP")); realIP != "" {
		fallback = realIP
		if !isPrivateIP(realIP) {
			return realIP, fallback
		}
	}
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		candidates := strings.Split(xff, ",")
		// 从右往左：跳过受信代理自身的地址，取第一个非受信地址。
		for i := len(candidates) - 1; i >= 0; i-- {
			candidate := normalizeValidIP(candidates[i])
			if candidate == "" {
				// 与 gin 一致：遇到无法解析的元素即停止，避免把更左侧的伪造值当结果。
				break
			}
			if IsTrustedDirectPeer(candidate) {
				if fallback == "" {
					fallback = candidate
				}
				continue
			}
			if !isPrivateIP(candidate) {
				return candidate, fallback
			}
			if fallback == "" {
				fallback = candidate
			}
		}
	}
	return "", fallback
}

// unknownClientIP 是客户端地址完全无法解析时使用的稳定占位值，
// 与 server/middleware/api_key_auth.go 的既有口径保持一致。
const unknownClientIP = "unknown"

// trustsClientIPHeaders 判断本次请求的原始转发头是否可信。
//
// 唯一依据是「直连对端是否在受信代理网段内」：该策略在启动期由
// server.trusted_proxies 灌入（见 server.configureClientIPTrust）。
//
// 这里刻意**不**复用 gin 的 ClientIP 判定作为第二个信任来源。gin.New() 的默认
// 值是信任全部对端（0.0.0.0/0、::/0），只要某个入口漏调 SetTrustedProxies，
// 「gin 已改写客户端地址」就会恒成立，从而把 F-01 的绕过面重新打开。
func trustsClientIPHeaders(peer string) bool {
	return IsTrustedDirectPeer(peer)
}

// GetTrustedClientIP 从 Gin 的可信代理解析链提取客户端 IP。
// 该方法依赖 gin.Engine.SetTrustedProxies 配置，不会优先直接信任原始转发头值。
// 适用于 ACL / 风控等安全敏感场景。
func GetTrustedClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if resolved := normalizeIP(c.ClientIP()); resolved != "" {
		return resolved
	}
	// RemoteAddr 不可解析时 gin 的 ClientIP 返回空串。安全敏感调用方（限流键、
	// 审计、会话绑定）会因此退化成空身份、把所有此类请求聚到同一个键上，
	// 故回落仓库既有的「未知客户端」口径。
	return unknownClientIP
}

// GetSecurityClientIP returns the address used by security-sensitive paths.
// When legacy forwarded-IP trust is enabled, raw forwarding headers take over
// client-IP resolution. When disabled, Gin's server.trusted_proxies chain is
// authoritative.
func GetSecurityClientIP(c *gin.Context, trustForwarded bool) string {
	if requestSettings, ok := requestForwardedIPSettings(c); ok {
		trustForwarded = requestSettings.trustForwarded
	}
	if trustForwarded {
		return GetClientIP(c)
	}
	return GetTrustedClientIP(c)
}

// normalizeIP 规范化 IP 地址，去除端口号和空格。
func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	// 移除端口号（如 "192.168.1.1:8080" -> "192.168.1.1"）
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}

// normalizeValidIP 规范化并验证代理头中的候选值，避免把 unknown、主机名等非法值传给安全服务。
func normalizeValidIP(value string) string {
	normalized := normalizeIP(value)
	parsed := net.ParseIP(normalized)
	if parsed == nil {
		return ""
	}
	return parsed.String()
}

// privateNets contains the private/loopback ranges skipped while selecting a
// public address from a legacy X-Forwarded-For chain.
var privateNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("invalid CIDR: " + cidr)
		}
		privateNets = append(privateNets, block)
	}
}

// CompiledIPRules 表示预编译的 IP 匹配规则。
// PatternCount 记录原始规则数量，用于保留“规则存在但全无效”时的行为语义。
type CompiledIPRules struct {
	CIDRs        []*net.IPNet
	IPs          []net.IP
	PatternCount int
}

// CompileIPRules 将 IP/CIDR 字符串规则预编译为可复用结构。
// 非法规则会被忽略，但 PatternCount 会保留原始规则条数。
func CompileIPRules(patterns []string) *CompiledIPRules {
	compiled := &CompiledIPRules{
		CIDRs:        make([]*net.IPNet, 0, len(patterns)),
		IPs:          make([]net.IP, 0, len(patterns)),
		PatternCount: len(patterns),
	}
	for _, pattern := range patterns {
		normalized := strings.TrimSpace(pattern)
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, "/") {
			_, cidr, err := net.ParseCIDR(normalized)
			if err != nil || cidr == nil {
				continue
			}
			compiled.CIDRs = append(compiled.CIDRs, cidr)
			continue
		}
		parsedIP := net.ParseIP(normalized)
		if parsedIP == nil {
			continue
		}
		compiled.IPs = append(compiled.IPs, parsedIP)
	}
	return compiled
}

func matchesCompiledRules(parsedIP net.IP, rules *CompiledIPRules) bool {
	if parsedIP == nil || rules == nil {
		return false
	}
	for _, cidr := range rules.CIDRs {
		if cidr.Contains(parsedIP) {
			return true
		}
	}
	for _, ruleIP := range rules.IPs {
		if parsedIP.Equal(ruleIP) {
			return true
		}
	}
	return false
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, block := range privateNets {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// MatchesPattern 检查 IP 是否匹配指定的模式（支持单个 IP 或 CIDR）。
// pattern 可以是：
// - 单个 IP: "192.168.1.100"
// - CIDR 范围: "192.168.1.0/24"
func MatchesPattern(clientIP, pattern string) bool {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}

	// 尝试解析为 CIDR
	if strings.Contains(pattern, "/") {
		_, cidr, err := net.ParseCIDR(pattern)
		if err != nil {
			return false
		}
		return cidr.Contains(ip)
	}

	// 作为单个 IP 处理
	patternIP := net.ParseIP(pattern)
	if patternIP == nil {
		return false
	}
	return ip.Equal(patternIP)
}

// MatchesAnyPattern 检查 IP 是否匹配任意一个模式。
func MatchesAnyPattern(clientIP string, patterns []string) bool {
	for _, pattern := range patterns {
		if MatchesPattern(clientIP, pattern) {
			return true
		}
	}
	return false
}

// CheckIPRestriction 检查 IP 是否被 API Key 的 IP 限制允许。
// 返回值：(是否允许, 拒绝原因)
// 逻辑：
// 1. 先检查黑名单，如果在黑名单中则直接拒绝
// 2. 如果白名单不为空，IP 必须在白名单中
// 3. 如果白名单为空，允许访问（除非被黑名单拒绝）
func CheckIPRestriction(clientIP string, whitelist, blacklist []string) (bool, string) {
	return CheckIPRestrictionWithCompiledRules(
		clientIP,
		CompileIPRules(whitelist),
		CompileIPRules(blacklist),
	)
}

// CheckIPRestrictionWithCompiledRules 使用预编译规则检查 IP 是否允许访问。
func CheckIPRestrictionWithCompiledRules(clientIP string, whitelist, blacklist *CompiledIPRules) (bool, string) {
	// 规范化 IP
	clientIP = normalizeIP(clientIP)
	if clientIP == "" {
		return false, "access denied"
	}
	parsedIP := net.ParseIP(clientIP)
	if parsedIP == nil {
		return false, "access denied"
	}

	// 1. 检查黑名单
	if blacklist != nil && blacklist.PatternCount > 0 && matchesCompiledRules(parsedIP, blacklist) {
		return false, "access denied"
	}

	// 2. 检查白名单（如果设置了白名单，IP 必须在其中）
	if whitelist != nil && whitelist.PatternCount > 0 && !matchesCompiledRules(parsedIP, whitelist) {
		return false, "access denied"
	}

	return true, ""
}

// ValidateIPPattern 验证 IP 或 CIDR 格式是否有效。
func ValidateIPPattern(pattern string) bool {
	if strings.Contains(pattern, "/") {
		_, _, err := net.ParseCIDR(pattern)
		return err == nil
	}
	return net.ParseIP(pattern) != nil
}

// ValidateIPPatterns 验证多个 IP 或 CIDR 格式。
// 返回无效的模式列表。
func ValidateIPPatterns(patterns []string) []string {
	var invalid []string
	for _, p := range patterns {
		if !ValidateIPPattern(p) {
			invalid = append(invalid, p)
		}
	}
	return invalid
}
