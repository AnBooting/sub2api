package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const stickySessionPrefix = "sticky_session:"
const openAIResponsesSessionWindowPrefix = "openai_responses_session_window:"
const liveCallPrefix = "live:call:"

type gatewayCache struct {
	rdb *redis.Client
}

func NewGatewayCache(rdb *redis.Client) service.GatewayCache {
	return &gatewayCache{rdb: rdb}
}

// buildSessionKey 构建 session key，包含 groupID 实现分组隔离
// 格式: sticky_session:{groupID}:{sessionHash}
func buildSessionKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", stickySessionPrefix, groupID, sessionHash)
}

func buildOpenAIResponsesSessionWindowKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", openAIResponsesSessionWindowPrefix, groupID, sessionHash)
}

func (c *gatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	key := buildSessionKey(groupID, sessionHash)
	accountID, err := c.rdb.Get(ctx, key).Int64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, service.ErrStickySessionNotFound
		}
		return 0, err
	}
	return accountID, nil
}

func (c *gatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Set(ctx, key, accountID, ttl).Err()
}

func (c *gatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// DeleteSessionAccountID 删除粘性会话与账号的绑定关系。
// 当检测到绑定的账号不可用（如状态错误、禁用、不可调度等）时调用，
// 以便下次请求能够重新选择可用账号。
//
// DeleteSessionAccountID removes the sticky session binding for the given session.
// Called when the bound account becomes unavailable (e.g., error status, disabled,
// or unschedulable), allowing subsequent requests to select a new available account.
func (c *gatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Del(ctx, key).Err()
}

var claimOpenAIResponsesSessionWindowScript = redis.NewScript(`
local previous = redis.call('GET', KEYS[1])
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return previous
`)

var compareAndRefreshOpenAIResponsesSessionWindowScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == false or current ~= ARGV[1] then
  return 0
end
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`)

var compareAndDeleteOpenAIResponsesSessionWindowScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == false or current ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

func (c *gatewayCache) ClaimOpenAIResponsesSessionWindow(ctx context.Context, groupID int64, sessionHash string, owner []byte, ttl time.Duration) ([]byte, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	if len(owner) == 0 || strings.TrimSpace(sessionHash) == "" || ttl <= 0 {
		return nil, errors.New("invalid OpenAI Responses session-window claim")
	}
	result, err := claimOpenAIResponsesSessionWindowScript.Run(
		ctx,
		c.rdb,
		[]string{buildOpenAIResponsesSessionWindowKey(groupID, sessionHash)},
		owner,
		ttl.Milliseconds(),
	).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch value := result.(type) {
	case nil:
		return nil, nil
	case string:
		return []byte(value), nil
	case []byte:
		return append([]byte(nil), value...), nil
	default:
		return nil, fmt.Errorf("unexpected OpenAI Responses session-window claim result %T", result)
	}
}

func (c *gatewayCache) CompareAndRefreshOpenAIResponsesSessionWindow(ctx context.Context, groupID int64, sessionHash string, expected []byte, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	if len(expected) == 0 || strings.TrimSpace(sessionHash) == "" || ttl <= 0 {
		return false, errors.New("invalid OpenAI Responses session-window refresh")
	}
	n, err := compareAndRefreshOpenAIResponsesSessionWindowScript.Run(
		ctx,
		c.rdb,
		[]string{buildOpenAIResponsesSessionWindowKey(groupID, sessionHash)},
		expected,
		ttl.Milliseconds(),
	).Int()
	return n == 1, err
}

func (c *gatewayCache) CompareAndDeleteOpenAIResponsesSessionWindow(ctx context.Context, groupID int64, sessionHash string, expected []byte) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	if len(expected) == 0 || strings.TrimSpace(sessionHash) == "" {
		return false, errors.New("invalid OpenAI Responses session-window delete")
	}
	n, err := compareAndDeleteOpenAIResponsesSessionWindowScript.Run(
		ctx,
		c.rdb,
		[]string{buildOpenAIResponsesSessionWindowKey(groupID, sessionHash)},
		expected,
	).Int()
	return n == 1, err
}

var _ service.OpenAIWSSessionPreemptionCache = (*gatewayCache)(nil)

const (
	grokVideoPendingBillingPrefix = "grok_video_pending:"
	grokVideoBilledPrefix         = "grok_video_billed:"
)

func (c *gatewayCache) SetGrokVideoPendingBilling(ctx context.Context, key string, payload []byte, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" || len(payload) == 0 {
		return errors.New("invalid grok video pending billing payload")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return c.rdb.Set(ctx, grokVideoPendingBillingPrefix+key, payload, ttl).Err()
}

func (c *gatewayCache) GetGrokVideoPendingBilling(ctx context.Context, key string) ([]byte, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("invalid grok video pending billing key")
	}
	val, err := c.rdb.Get(ctx, grokVideoPendingBillingPrefix+key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

func (c *gatewayCache) ClaimGrokVideoBilled(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false, errors.New("invalid grok video billed key")
	}
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	return c.rdb.SetNX(ctx, grokVideoBilledPrefix+key, "1", ttl).Result()
}

func (c *gatewayCache) ReleaseGrokVideoBilled(ctx context.Context, key string) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("invalid grok video billed key")
	}
	return c.rdb.Del(ctx, grokVideoBilledPrefix+key).Err()
}

// Compile-time assertion: gatewayCache must implement CyberSessionBlockStore.
var _ service.CyberSessionBlockStore = (*gatewayCache)(nil)
var _ service.LiveCallStore = (*gatewayCache)(nil)

const reasoningContentPrefix = "reasoning_content:"

// reasoningContentDefaultTTL 是 reasoning 缓存的默认过期时间。Codex 会话可能
// 跨多天恢复，取 7 天；调用方传入非正 TTL 时兜底。
const reasoningContentDefaultTTL = 7 * 24 * time.Hour

// SetReasoningContent 按 reasoning item id 缓存 reasoning 全文。
// itemID 或 content 为空时直接返回 nil（无可缓存内容，属正常情况而非错误）。
func (c *gatewayCache) SetReasoningContent(ctx context.Context, itemID string, content string, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || content == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = reasoningContentDefaultTTL
	}
	return c.rdb.Set(ctx, reasoningContentPrefix+itemID, content, ttl).Err()
}

// GetReasoningContent 返回缓存的 reasoning 全文；未命中返回
// service.ErrReasoningContentNotFound。
func (c *gatewayCache) GetReasoningContent(ctx context.Context, itemID string) (string, error) {
	if c == nil || c.rdb == nil {
		return "", errors.New("gateway cache unavailable")
	}
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", service.ErrReasoningContentNotFound
	}
	val, err := c.rdb.Get(ctx, reasoningContentPrefix+itemID).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", service.ErrReasoningContentNotFound
		}
		return "", err
	}
	return val, nil
}

const (
	cyberSessionBlockPrefix         = "cyber_session_block:"
	cyberSessionScopePrefix         = "cyber_session_scope:"
	cyberSessionRedisCommandMaxKeys = 128
)

// SetCyberSessionBlocked writes exact blocks in bounded transactions. The
// coarse scope is activated only after all exact blocks have been stored.
func (c *gatewayCache) SetCyberSessionBlocked(ctx context.Context, scopeKey string, keys []string, ttl time.Duration) error {
	if len(keys) == 0 {
		return nil
	}
	exactKeys := make([]string, 0, cyberSessionRedisCommandMaxKeys)
	flush := func() error {
		if len(exactKeys) == 0 {
			return nil
		}
		pipe := c.rdb.TxPipeline()
		for _, key := range exactKeys {
			pipe.Set(ctx, cyberSessionBlockPrefix+key, "1", ttl)
		}
		_, err := pipe.Exec(ctx)
		exactKeys = exactKeys[:0]
		return err
	}
	for _, key := range keys {
		if key != "" {
			exactKeys = append(exactKeys, key)
			if len(exactKeys) == cyberSessionRedisCommandMaxKeys {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if scopeKey != "" {
		return c.rdb.Set(ctx, cyberSessionScopePrefix+scopeKey, "1", ttl).Err()
	}
	return nil
}

func (c *gatewayCache) IsCyberSessionScopeActive(ctx context.Context, scopeKey string) (bool, error) {
	n, err := c.rdb.Exists(ctx, cyberSessionScopePrefix+scopeKey).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// FindCyberSessionBlocked checks bounded batches in caller order and stops at
// the first blocked key, preserving the original earliest-match behavior.
func (c *gatewayCache) FindCyberSessionBlocked(ctx context.Context, keys []string) (string, error) {
	if len(keys) == 0 {
		return "", nil
	}
	for start := 0; start < len(keys); start += cyberSessionRedisCommandMaxKeys {
		end := start + cyberSessionRedisCommandMaxKeys
		if end > len(keys) {
			end = len(keys)
		}
		redisKeys := make([]string, end-start)
		for i, key := range keys[start:end] {
			redisKeys[i] = cyberSessionBlockPrefix + key
		}
		values, err := c.rdb.MGet(ctx, redisKeys...).Result()
		if err != nil {
			return "", err
		}
		for i, value := range values {
			if value != nil {
				return keys[start+i], nil
			}
		}
	}
	return "", nil
}

var claimLiveControllerScript = redis.NewScript(`
	local key = KEYS[1]
	local target = ARGV[1]
	local owner = ARGV[2]
	if redis.call('HGET', key, 'handoff_v2_protocol') == '2' then
		if not ARGV[3] then return -4 end
		local identity = redis.call('HGET', key, 'handoff_v2_identity')
		local op = redis.call('HGET', key, 'handoff_v2_operation')
		if not identity or not op or redis.call('HGET', key, 'handoff_v2_generation') ~= ARGV[3] or
			redis.call('GET', KEYS[2]) ~= op .. ':' .. identity .. ':' .. ARGV[3] then return -2 end
		if redis.call('PTTL', KEYS[2]) ~= -1 then return -3 end
		if redis.call('HGET', key, 'handoff_v2_phase') ~= 'active' or redis.call('PTTL', key) <= 0 then return -3 end
	elseif ARGV[3] then
		return -4
	end
	local current = redis.call('HGET', key, 'controller')
	if current == false or current == 'closed' then
		return 0
	end
	if target == 'observer' and current ~= 'pending' then
		return 0
	end
	if target == 'proxy' and current ~= 'pending' and current ~= 'observer' and
		(current ~= 'proxy' or redis.call('HGET', key, 'controller_owner') ~= owner) then
		return 0
	end
	redis.call('HSET', key, 'controller', target, 'controller_owner', owner)
	return 1
`)

var markLiveCallClosedScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('EXISTS', key) == 0 then
		return 0
	end
	if redis.call('HGET', key, 'handoff_v2_protocol') == '2' then return -4 end
	if redis.call('HGET', key, 'controller') == 'closed' then
		return 0
	end
	redis.call('HSET', key, 'controller', 'closed', 'controller_owner', '')
	redis.call('EXPIRE', key, ARGV[1])
	return 1
`)

var releaseLiveControllerScript = redis.NewScript(`
	local key = KEYS[1]
	if redis.call('HGET', key, 'handoff_v2_protocol') == '2' then
		if not ARGV[2] then return -4 end
		local identity = redis.call('HGET', key, 'handoff_v2_identity')
		local op = redis.call('HGET', key, 'handoff_v2_operation')
		if not identity or not op or redis.call('HGET', key, 'handoff_v2_generation') ~= ARGV[2] or
			redis.call('GET', KEYS[2]) ~= op .. ':' .. identity .. ':' .. ARGV[2] then return -2 end
		if redis.call('PTTL', KEYS[2]) ~= -1 then return -3 end
		if redis.call('HGET', key, 'handoff_v2_phase') ~= 'active' or redis.call('PTTL', key) <= 0 then return -3 end
	elseif ARGV[2] then
		return -4
	end
	if redis.call('HGET', key, 'controller') ~= 'proxy' or
		redis.call('HGET', key, 'controller_owner') ~= ARGV[1] then
		return 0
	end
	redis.call('HSET', key, 'controller', 'pending', 'controller_owner', '')
	return 1
`)

var claimLiveSettlementAuditScript = redis.NewScript(`
    redis.replicate_commands()
    local key = KEYS[1]
    if redis.call('EXISTS', key) == 0 then return -1 end
    if redis.call('HGET', key, 'handoff_v2_protocol') == '2' then return -4 end
    local raw = redis.call('HGET', key, 'settlement')
    if not raw then return -2 end
    local valid, state = pcall(cjson.decode, raw)
    if not valid or type(state) ~= 'table' then return -2 end
    if state.status == 'settled' then return 0 end
    if state.status ~= 'pending' then return -2 end
    if redis.call('HGET', key, 'audit_written') == '1' then return 0 end
    local clock = redis.call('TIME')
    local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
    local owner = redis.call('HGET', key, 'audit_owner')
    local expires = tonumber(redis.call('HGET', key, 'audit_until') or '0') or 0
    if owner and expires > now then
        if owner == ARGV[1] then return 1 end
        return 0
    end
    redis.call('HSET', key, 'audit_owner', ARGV[1], 'audit_until', now + tonumber(ARGV[2]))
    return 1
`)

var finishLiveSettlementAuditScript = redis.NewScript(`
    local key = KEYS[1]
    if redis.call('EXISTS', key) == 0 then return -1 end
    if redis.call('HGET', key, 'handoff_v2_protocol') == '2' then return -4 end
    if redis.call('HGET', key, 'audit_owner') ~= ARGV[1] then return 0 end
    if ARGV[2] == '1' then redis.call('HSET', key, 'audit_written', '1') end
    redis.call('HDEL', key, 'audit_owner', 'audit_until')
    return 1
`)

func liveCallKey(callHash string) string {
	return liveCallPrefix + callHash
}

func HashLiveCallID(callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return hex.EncodeToString(sum[:])
}

var saveLegacyLiveCallScript = redis.NewScript(`
	local key, guard = KEYS[1], KEYS[2]
	if redis.call('EXISTS', guard) ~= 0 or redis.call('HGET', key, 'handoff_v2_protocol') == '2' then return -4 end
	redis.call('HSET', key, unpack(ARGV, 2))
	redis.call('EXPIRE', key, ARGV[1])
	return 1
`)

func (c *gatewayCache) SaveLiveCall(ctx context.Context, record *service.LiveCallRecord, ttl time.Duration) error {
	if record == nil || record.CallHash == "" || record.CallID == "" {
		return fmt.Errorf("invalid live call record")
	}
	values := map[string]any{
		"call_id":          record.CallID,
		"account_id":       record.AccountID,
		"api_key_id":       record.APIKeyID,
		"user_id":          record.UserID,
		"group_id":         record.GroupID,
		"subscription_id":  record.SubscriptionID,
		"lease_id":         record.LeaseID,
		"model":            record.Model,
		"created_at":       record.CreatedAt.UnixMilli(),
		"expires_at":       record.ExpiresAt.UnixMilli(),
		"controller":       record.Controller,
		"controller_owner": record.ControllerOwner,
		"user_agent":       record.UserAgent,
		"ip_address":       record.IPAddress,
		"inbound_endpoint": record.InboundEndpoint,
		"attestation":      record.AttestationCiphertext,
	}
	// 冻结单价与计费策略必须随记录持久化：observer 路径会从 Redis 回读记录再结算，
	// 回读丢失单价会让已配置价格的会话按缺价落账；结算重试也需要同一份策略。
	if record.RealtimePricePerMin != nil {
		values["realtime_price_per_min"] = strconv.FormatFloat(*record.RealtimePricePerMin, 'f', -1, 64)
	}
	if record.BillingPolicy != nil {
		if encoded, err := json.Marshal(record.BillingPolicy); err == nil {
			values["billing_policy"] = string(encoded)
		}
	}
	if record.Settlement != nil {
		if encoded, err := json.Marshal(record.Settlement); err == nil {
			values["settlement"] = string(encoded)
		}
	}
	seconds := int64(ttl / time.Second)
	if ttl > 0 && ttl < time.Second {
		seconds = 1
	}
	args := []any{seconds}
	for field, value := range values {
		args = append(args, field, value)
	}
	result, err := saveLegacyLiveCallScript.Run(ctx, c.rdb, []string{liveCallKey(record.CallHash), liveHandoffGuardKey(record.CallHash)}, args...).Int()
	if err != nil {
		return err
	}
	if result == -4 {
		return service.ErrLiveHandoffProtocol
	}
	return nil
}

func (c *gatewayCache) GetLiveCall(ctx context.Context, callHash string) (*service.LiveCallRecord, error) {
	values, err := c.rdb.HGetAll(ctx, liveCallKey(callHash)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, service.ErrLiveCallNotFound
	}
	parseInt := func(field string) int64 {
		value, _ := strconv.ParseInt(values[field], 10, 64)
		return value
	}
	createdAt := time.UnixMilli(parseInt("created_at"))
	expiresAt := time.UnixMilli(parseInt("expires_at"))
	price, priceErr := strconv.ParseFloat(values["realtime_price_per_min"], 64)
	var realtimePrice *float64
	if priceErr == nil && strings.TrimSpace(values["realtime_price_per_min"]) != "" {
		realtimePrice = &price
	}
	var billingPolicy *service.LiveBillingPolicy
	if encoded := values["billing_policy"]; encoded != "" {
		var policy service.LiveBillingPolicy
		if err := json.Unmarshal([]byte(encoded), &policy); err == nil {
			billingPolicy = &policy
		}
	}
	var settlement *service.LiveSettlement
	if encoded := values["settlement"]; encoded != "" {
		var state service.LiveSettlement
		if err := json.Unmarshal([]byte(encoded), &state); err == nil {
			settlement = &state
		}
	}
	return &service.LiveCallRecord{
		CallID:                values["call_id"],
		CallHash:              callHash,
		AccountID:             parseInt("account_id"),
		APIKeyID:              parseInt("api_key_id"),
		UserID:                parseInt("user_id"),
		GroupID:               parseInt("group_id"),
		SubscriptionID:        parseInt("subscription_id"),
		LeaseID:               values["lease_id"],
		Model:                 values["model"],
		CreatedAt:             createdAt,
		ExpiresAt:             expiresAt,
		Controller:            values["controller"],
		ControllerOwner:       values["controller_owner"],
		UserAgent:             values["user_agent"],
		IPAddress:             values["ip_address"],
		InboundEndpoint:       values["inbound_endpoint"],
		AttestationCiphertext: values["attestation"],
		RealtimePricePerMin:   realtimePrice,
		BillingPolicy:         billingPolicy,
		Settlement:            settlement,
	}, nil
}

func (c *gatewayCache) ClaimLiveController(ctx context.Context, callHash, controller, owner string) (bool, error) {
	result, err := claimLiveControllerScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, controller, owner).Int()
	return liveHandoffControllerResult(result, err)
}

func (c *gatewayCache) GetLiveController(ctx context.Context, callHash string) (string, error) {
	value, err := c.rdb.HGet(ctx, liveCallKey(callHash), "controller").Result()
	if err == redis.Nil {
		return "", service.ErrLiveCallNotFound
	}
	return value, err
}

func (c *gatewayCache) ReleaseLiveController(ctx context.Context, callHash, owner string) (bool, error) {
	result, err := releaseLiveControllerScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, owner).Int()
	return liveHandoffControllerResult(result, err)
}

func (c *gatewayCache) MarkLiveCallClosed(ctx context.Context, callHash string, ttl time.Duration) (bool, error) {
	result, err := markLiveCallClosedScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, int64(ttl.Seconds())).Int()
	return liveHandoffControllerResult(result, err)
}

// BeginLiveSettlement atomically closes an existing call and establishes the
// first frozen amount. The returned token is the exact stored JSON for CAS.
var beginLiveSettlementScript = redis.NewScript(`
    local key = KEYS[1]
    if redis.call('EXISTS', key) == 0 then return {-1, ''} end
    if redis.call('HGET', key, 'handoff_v2_protocol') == '2' then return {-4, ''} end
    if ARGV[3] == '1' then
        for i = 4, #ARGV, 3 do
            local value = redis.call('HGET', key, ARGV[i])
            local presence = ARGV[i + 1]
            if presence == '0' and value then return {-2, ''} end
            if presence == '1' and (not value or value ~= ARGV[i + 2]) then return {-2, ''} end
            if presence == '2' and (value or '0') ~= ARGV[i + 2] then return {-2, ''} end
        end
    end
    local current = redis.call('HGET', key, 'settlement')
    if not current then
        current = ARGV[1]
        redis.call('HSET', key, 'settlement', current)
    end
    local first = 0
    if redis.call('HGET', key, 'controller') ~= 'closed' then first = 1 end
    redis.call('HSET', key, 'controller', 'closed', 'controller_owner', '')
    redis.call('EXPIRE', key, ARGV[2])
    return {first, current}
`)

var compareAndSwapLiveSettlementScript = redis.NewScript(`
    local key = KEYS[1]
    if redis.call('EXISTS', key) == 0 then return -1 end
    if redis.call('HGET', key, 'handoff_v2_protocol') == '2' then return -4 end
    if redis.call('HGET', key, 'settlement') ~= ARGV[1] then return 0 end
    redis.call('HSET', key, 'settlement', ARGV[2])
    redis.call('EXPIRE', key, ARGV[3])
    return 1
`)

func (c *gatewayCache) BeginLiveSettlement(ctx context.Context, callHash string, candidate *service.LiveSettlement, ttl time.Duration) (*service.LiveSettlement, string, bool, error) {
	return c.beginLiveSettlement(ctx, callHash, candidate, nil, ttl)
}

func (c *gatewayCache) BeginLiveSettlementVerified(ctx context.Context, record *service.LiveCallRecord, candidate *service.LiveSettlement, ttl time.Duration) (*service.LiveSettlement, string, bool, error) {
	if record == nil || candidate == nil || record.CallID == "" || HashLiveCallID(record.CallID) != record.CallHash || record.UserID <= 0 || record.AccountID <= 0 || record.APIKeyID <= 0 || record.LeaseID == "" || record.Model == "" || record.CreatedAt.UnixMilli() <= 0 || record.ExpiresAt.UnixMilli() <= 0 {
		return nil, "", false, service.ErrLiveIdentityMismatch
	}
	if record.RealtimePricePerMin == nil {
		if candidate.ActualCost != 0 || candidate.TotalCost != 0 {
			return nil, "", false, service.ErrLiveIdentityMismatch
		}
	} else if *record.RealtimePricePerMin <= 0 || math.IsNaN(*record.RealtimePricePerMin) || math.IsInf(*record.RealtimePricePerMin, 0) {
		return nil, "", false, service.ErrLiveIdentityMismatch
	}
	return c.beginLiveSettlement(ctx, record.CallHash, candidate, record, ttl)
}

func (c *gatewayCache) beginLiveSettlement(ctx context.Context, callHash string, candidate *service.LiveSettlement, expected *service.LiveCallRecord, ttl time.Duration) (*service.LiveSettlement, string, bool, error) {
	if callHash == "" || candidate == nil || ttl < time.Second {
		return nil, "", false, fmt.Errorf("invalid live settlement")
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return nil, "", false, err
	}
	args := []any{string(encoded), int64(ttl.Seconds()), "0"}
	if expected != nil {
		price := ""
		if expected.RealtimePricePerMin != nil {
			price = strconv.FormatFloat(*expected.RealtimePricePerMin, 'f', -1, 64)
		}
		policy := ""
		if expected.BillingPolicy != nil {
			value, err := json.Marshal(expected.BillingPolicy)
			if err != nil {
				return nil, "", false, err
			}
			policy = string(value)
		}
		args[2] = "1"
		required := func(field, value string) { args = append(args, field, "1", value) }
		optional := func(field, value string, present bool) {
			flag := "0"
			if present {
				flag = "1"
			}
			args = append(args, field, flag, value)
		}
		required("call_id", expected.CallID)
		required("user_id", strconv.FormatInt(expected.UserID, 10))
		required("account_id", strconv.FormatInt(expected.AccountID, 10))
		required("api_key_id", strconv.FormatInt(expected.APIKeyID, 10))
		required("group_id", strconv.FormatInt(expected.GroupID, 10))
		required("subscription_id", strconv.FormatInt(expected.SubscriptionID, 10))
		required("lease_id", expected.LeaseID)
		required("model", expected.Model)
		required("created_at", strconv.FormatInt(expected.CreatedAt.UnixMilli(), 10))
		required("expires_at", strconv.FormatInt(expected.ExpiresAt.UnixMilli(), 10))
		optional("realtime_price_per_min", price, expected.RealtimePricePerMin != nil)
		optional("billing_policy", policy, expected.BillingPolicy != nil)
		required("user_agent", expected.UserAgent)
		required("ip_address", expected.IPAddress)
		required("inbound_endpoint", expected.InboundEndpoint)
	}
	result, err := beginLiveSettlementScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, args...).Slice()
	if err != nil {
		return nil, "", false, err
	}
	if len(result) != 2 {
		return nil, "", false, fmt.Errorf("invalid live settlement response")
	}
	first, ok := result[0].(int64)
	if !ok {
		return nil, "", false, fmt.Errorf("invalid live settlement close result")
	}
	if first == -1 {
		return nil, "", false, service.ErrLiveCallNotFound
	}
	if first == -2 {
		return nil, "", false, service.ErrLiveIdentityMismatch
	}
	if first == -4 {
		return nil, "", false, service.ErrLiveHandoffProtocol
	}
	token, ok := result[1].(string)
	if !ok || token == "" {
		return nil, "", false, fmt.Errorf("invalid live settlement checkpoint")
	}
	var state service.LiveSettlement
	if err := json.Unmarshal([]byte(token), &state); err != nil {
		return nil, "", false, fmt.Errorf("invalid live settlement checkpoint: %w", err)
	}
	return &state, token, first == 1, nil
}

func (c *gatewayCache) ClaimLiveSettlementAudit(ctx context.Context, callHash, owner string, ttl time.Duration) (bool, error) {
	if callHash == "" || owner == "" || ttl < time.Second {
		return false, fmt.Errorf("invalid live audit claim")
	}
	result, err := claimLiveSettlementAuditScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	if result == -1 {
		return false, service.ErrLiveCallNotFound
	}
	if result == -2 {
		return false, service.ErrLiveSettlementInvalid
	}
	if result == -4 {
		return false, service.ErrLiveHandoffProtocol
	}
	return result == 1, nil
}

func (c *gatewayCache) FinishLiveSettlementAudit(ctx context.Context, callHash, owner string, written bool) (bool, error) {
	if callHash == "" || owner == "" {
		return false, fmt.Errorf("invalid live audit completion")
	}
	success := "0"
	if written {
		success = "1"
	}
	result, err := finishLiveSettlementAuditScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, owner, success).Int64()
	if err != nil {
		return false, err
	}
	if result == -1 {
		return false, service.ErrLiveCallNotFound
	}
	if result == -4 {
		return false, service.ErrLiveHandoffProtocol
	}
	return result == 1, nil
}

func (c *gatewayCache) CompareAndSwapLiveSettlement(ctx context.Context, callHash, expected string, next *service.LiveSettlement, ttl time.Duration) (bool, error) {
	if callHash == "" || expected == "" || next == nil || ttl < time.Second {
		return false, fmt.Errorf("invalid live settlement update")
	}
	var previous service.LiveSettlement
	if err := json.Unmarshal([]byte(expected), &previous); err != nil {
		return false, fmt.Errorf("invalid live settlement checkpoint: %w", err)
	}
	if previous.TotalCost != next.TotalCost || previous.ActualCost != next.ActualCost ||
		previous.BillingMode != next.BillingMode || previous.DurationMs != next.DurationMs ||
		!previous.FrozenAt.Equal(next.FrozenAt) ||
		(previous.SettlementMode != "" && previous.SettlementMode != next.SettlementMode) ||
		(previous.Status == service.LiveSettlementSettled && next.Status != service.LiveSettlementSettled) ||
		(next.Status != service.LiveSettlementPending && next.Status != service.LiveSettlementSettled) ||
		(previous.UsageLogWritten && !next.UsageLogWritten) || next.Attempts < previous.Attempts {
		return false, fmt.Errorf("invalid live settlement transition")
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	result, err := compareAndSwapLiveSettlementScript.Run(ctx, c.rdb, []string{liveCallKey(callHash)}, expected, string(encoded), int64(ttl.Seconds())).Int()
	if err != nil {
		return false, err
	}
	if result == -1 {
		return false, service.ErrLiveCallNotFound
	}
	if result == -4 {
		return false, service.ErrLiveHandoffProtocol
	}
	return result == 1, nil
}
