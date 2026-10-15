package yunmao

import (
	"net/url"
	"strings"
)

// ValidateConfig 判定配置是否**完整到可以发起上游调用**。
//
// 这里不做任何「尽力而为」的推断：只要有一项缺失就返回 CodeNotConfigured，
// 调用方必须把它当作 fail closed 处理（PRD-03 §2.8 / AC-B6）。
func ValidateConfig(cfg Config) error {
	if !cfg.Enabled {
		return notConfiguredError("上游服务未启用")
	}
	if err := validateBaseURL(cfg.BaseURL); err != nil {
		return err
	}
	if !cfg.Endpoints.Complete() {
		return notConfiguredError("接口路径槽位未填写完整（等待上游接口文档）")
	}
	if strings.TrimSpace(cfg.Credentials.AppID) == "" || strings.TrimSpace(cfg.Credentials.AppSecret) == "" {
		return notConfiguredError("凭据槽位未配置")
	}
	return nil
}

func validateBaseURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return notConfiguredError("接口地址未配置")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return notConfiguredError("接口地址非法")
	}
	if parsed.Host == "" {
		return notConfiguredError("接口地址非法")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return notConfiguredError("接口地址协议非法")
	}
	return nil
}

// resolveEndpoint 把 BaseURL 与相对路径拼成绝对地址。
//
// 只允许相对路径：绝对地址必须由运维显式写进 BaseURL，避免端点配置绕过白标校验。
func (c Config) resolveEndpoint(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", notConfiguredError("接口路径槽位为空")
	}
	if strings.Contains(trimmed, "://") {
		return "", notConfiguredError("接口路径必须是相对路径")
	}
	base, err := url.Parse(strings.TrimSpace(c.BaseURL))
	if err != nil || base.Host == "" {
		return "", notConfiguredError("接口地址非法")
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + trimmed
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}
