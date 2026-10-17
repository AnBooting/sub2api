package yunmao

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// 环境变量槽位名。
//
// 设计取舍（PRD-03 BE-01 / 共享契约 C-8「凭据类只能走环境变量或加密存储」）：
// 这里刻意**只**从环境变量读取，不新增配置文件字段、不写入 settings 表、不落库。
// 好处是凭据不可能被管理面 API 回读、不可能进入前端 bundle、不可能被审计日志打印。
// 代价是运维改地址/凭据需要重启进程——对「等待上游文档填充」的预留阶段是可接受的。
const (
	EnvEnabled          = "YUNMAO_ENABLED"
	EnvBaseURL          = "YUNMAO_BASE_URL"
	EnvAppID            = "YUNMAO_APP_ID"
	EnvAppSecret        = "YUNMAO_APP_SECRET"
	EnvListProductsPath = "YUNMAO_PRODUCTS_PATH"
	EnvCreateOrderPath  = "YUNMAO_CREATE_ORDER_PATH"
	EnvQueryOrderPath   = "YUNMAO_QUERY_ORDER_PATH"
	EnvFetchCardsPath   = "YUNMAO_FETCH_CARDS_PATH"
	EnvTimeoutSeconds   = "YUNMAO_TIMEOUT_SECONDS"
	EnvRedirectHosts    = "YUNMAO_REDIRECT_HOST_ALLOWLIST"
	// EnvCredentialPrefix 是追加凭据槽位的前缀：YUNMAO_CREDENTIAL_<KEY>=<value>。
	// 例如接口文档要求「店铺号」时，填 YUNMAO_CREDENTIAL_SHOP_ID=xxx 即可，
	// 无需改代码——这正是「预留槽位、后续填充」要解决的问题。
	EnvCredentialPrefix = "YUNMAO_CREDENTIAL_"
)

// DefaultTimeout 是未显式配置时的上游调用超时。
const DefaultTimeout = 10 * time.Second

// LoadConfigFromEnv 从环境变量读取配置。
//
// 只读取、不校验业务合法性：校验交给 ValidateConfig，便于把「配置读取」和
// 「是否可服务」两件事分开测试（未配置时 Adapter.Configured() 必须为 false）。
func LoadConfigFromEnv() Config {
	timeout := DefaultTimeout
	if raw := strings.TrimSpace(os.Getenv(EnvTimeoutSeconds)); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}

	return Config{
		Enabled: strings.EqualFold(strings.TrimSpace(os.Getenv(EnvEnabled)), "true"),
		BaseURL: strings.TrimSpace(os.Getenv(EnvBaseURL)),
		Endpoints: Endpoints{
			ListProducts: strings.TrimSpace(os.Getenv(EnvListProductsPath)),
			CreateOrder:  strings.TrimSpace(os.Getenv(EnvCreateOrderPath)),
			QueryOrder:   strings.TrimSpace(os.Getenv(EnvQueryOrderPath)),
			FetchCards:   strings.TrimSpace(os.Getenv(EnvFetchCardsPath)),
		},
		Credentials: Credentials{
			AppID:     strings.TrimSpace(os.Getenv(EnvAppID)),
			AppSecret: strings.TrimSpace(os.Getenv(EnvAppSecret)),
			Extra:     loadExtraCredentialsFromEnv(),
		},
		Timeout:               timeout,
		RedirectHostAllowlist: splitAndTrim(os.Getenv(EnvRedirectHosts)),
	}
}

func loadExtraCredentialsFromEnv() map[string]string {
	extra := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(name, EnvCredentialPrefix) {
			continue
		}
		key := strings.ToLower(strings.TrimPrefix(name, EnvCredentialPrefix))
		if key == "" {
			continue
		}
		extra[key] = strings.TrimSpace(value)
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func splitAndTrim(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, strings.ToLower(trimmed))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
