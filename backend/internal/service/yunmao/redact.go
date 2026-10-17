package yunmao

import "strings"

// RedactText 抹掉文本里可能出现的凭据值。
//
// 用途：任何可能写入日志的诊断文本（错误、上游响应摘要）都要先过一遍这里，
// 保证「凭据不进日志」（PRD-03 §2.7 / BE-06）。空值不参与替换，避免把整段文本
// 变成占位符。
func (c Config) RedactText(text string) string {
	if text == "" {
		return text
	}
	out := text
	for _, secret := range c.secretValues() {
		out = strings.ReplaceAll(out, secret, "[redacted]")
	}
	return out
}

// secretValues 返回所有必须抹除的凭据值（不含短于 4 字符的值，避免误伤正常文本）。
func (c Config) secretValues() []string {
	candidates := []string{c.Credentials.AppSecret}
	for _, value := range c.Credentials.Extra {
		candidates = append(candidates, value)
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if len(candidate) >= 4 {
			out = append(out, candidate)
		}
	}
	return out
}

// RedactedURL 把 URL 裁剪成「只有 scheme://host/path」，去掉 query 与 fragment。
//
// query 里常带签名与凭据，绝不能整条写日志。
func RedactedURL(raw string) string {
	if raw == "" {
		return ""
	}
	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		return raw[:idx]
	}
	return raw
}
