package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	SeedanceEndpointCreate           GrokMediaEndpoint        = "seedance_create"
	SeedanceEndpointStatus           GrokMediaEndpoint        = "seedance_status"
	SeedanceEndpointDelete           GrokMediaEndpoint        = "seedance_delete"
	OpenAIEndpointCapabilitySeedance OpenAIEndpointCapability = "seedance"
)

func (e GrokMediaEndpoint) IsSeedance() bool {
	return e == SeedanceEndpointCreate || e == SeedanceEndpointStatus || e == SeedanceEndpointDelete
}

// SeedanceTaskKey isolates ownership and billing keys from other video providers.
func SeedanceTaskKey(id string) string { return "seedance:" + strings.TrimSpace(id) }

// seedanceFramesPerSecond is Ark's official frame rate: the query response reports
// either duration in seconds or the raw frame count, and the documented conversion
// is duration = total frames / 24, floored.
const seedanceFramesPerSecond = 24

// SeedanceDurationSecondsFromStatusBody extracts the billed duration (seconds)
// from an Ark Seedance task query response.
//
// Official field rules:
//   - top-level "duration" (integer seconds) when present and positive;
//   - otherwise top-level "frames" converted as frames / 24, floored;
//   - neither present (or unusable, e.g. the create-time duration=-1 sentinel) →
//     0, meaning "unknown". Callers must then keep their request-parameter
//     fallback instead of billing a guessed duration.
func SeedanceDurationSecondsFromStatusBody(statusBody []byte) int {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return 0
	}
	if duration := gjson.GetBytes(statusBody, "duration"); duration.Exists() && duration.Type == gjson.Number {
		if seconds := int(duration.Int()); seconds > 0 {
			return seconds
		}
	}
	if frames := gjson.GetBytes(statusBody, "frames"); frames.Exists() && frames.Type == gjson.Number {
		if total := int(frames.Int()); total > 0 {
			return total / seedanceFramesPerSecond
		}
	}
	return 0
}

// SeedanceResolutionFromStatusBody extracts the output resolution tier from an Ark
// Seedance task query response.
//
// Official contract: the query response carries a top-level "resolution" (and the
// create request accepts the same field). Billing is tiered by resolution, so a
// missing tier must not silently fall back to the cheapest one — callers compare
// the returned string against the configured tiers and fail closed when absent.
// Returns "" when the field is absent or unusable.
func SeedanceResolutionFromStatusBody(statusBody []byte) string {
	if len(statusBody) == 0 || !gjson.ValidBytes(statusBody) {
		return ""
	}
	for _, path := range []string{"resolution", "content.resolution", "output.resolution"} {
		value := gjson.GetBytes(statusBody, path)
		if value.Type == gjson.String {
			if normalized, ok := LookupVideoBillingResolution(value.String()); ok {
				return normalized
			}
		}
	}
	return ""
}

// SeedanceResolutionFromRequest extracts the requested output resolution from an
// Ark Seedance create request body. The create response does not echo it, so the
// request is the only create-time source and must be snapshotted for later billing.
func SeedanceResolutionFromRequest(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range []string{"resolution", "content.resolution", "output.resolution"} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String {
			if normalized, ok := LookupVideoBillingResolution(value.String()); ok {
				return normalized
			}
		}
	}
	return ""
}

// SeedanceDurationSecondsFromRequest extracts the requested duration (seconds)
// from an Ark Seedance create request body.
//
// Official contract: the request accepts either "duration" (integer seconds) or
// "frames" (frame count, duration = frames / 24 floored). The create response does
// not echo it, so this is the create-time fallback used only when the query
// response reports neither field. Returns 0 for the documented -1 sentinel and for
// an absent/unusable value, which means "unknown" rather than a guessed duration.
func SeedanceDurationSecondsFromRequest(body []byte) int {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return 0
	}
	if duration := gjson.GetBytes(body, "duration"); duration.Type == gjson.Number {
		if seconds := int(duration.Int()); seconds > 0 {
			return seconds
		}
	}
	if frames := gjson.GetBytes(body, "frames"); frames.Type == gjson.Number {
		if total := int(frames.Int()); total > 0 {
			return total / seedanceFramesPerSecond
		}
	}
	return 0
}

func ParseSeedanceRequest(body []byte) (GrokMediaRequestInfo, error) {
	var info GrokMediaRequestInfo
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return info, fmt.Errorf("request body must be a JSON object")
	}
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String || strings.TrimSpace(model.String()) == "" {
		return info, fmt.Errorf("model is required")
	}
	content := gjson.GetBytes(body, "content")
	if !content.IsArray() || len(content.Array()) == 0 {
		return info, fmt.Errorf("content must be a non-empty array")
	}
	info.Model = strings.TrimSpace(model.String())
	var texts []string
	for _, item := range content.Array() {
		switch item.Get("type").String() {
		case "text":
			texts = append(texts, item.Get("text").String())
		case "image_url":
			info.InputImageURLs = append(info.InputImageURLs, item.Get("image_url.url").String())
		}
	}
	info.Prompt = strings.Join(texts, "\n")
	return info, nil
}

func buildSeedanceURL(base string, endpoint GrokMediaEndpoint, taskID string) (string, error) {
	base = strings.TrimRight(base, "/")
	// Accept an origin, a proxy prefix, or the full Ark API base.
	if !strings.HasSuffix(base, "/api/v3") && !strings.HasSuffix(base, "/v3") {
		base += "/api/v3"
	}
	base += "/contents/generations/tasks"
	if endpoint != SeedanceEndpointCreate {
		if err := validateUpstreamPathSegment("Seedance task ID", taskID); err != nil || strings.TrimSpace(taskID) == "" {
			return "", fmt.Errorf("invalid Seedance task ID")
		}
		base += "/" + taskID
	}
	return base, nil
}

// ForwardSeedance preserves the Ark protocol, including multimodal content and
// future fields. Only model is rewritten using the account's configured mapping.
func (s *OpenAIGatewayService) ForwardSeedance(ctx context.Context, c *gin.Context, account *Account, endpoint GrokMediaEndpoint, taskID string, body []byte) (*OpenAIForwardResult, error) {
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilitySeedance) || !endpoint.IsSeedance() {
		return nil, fmt.Errorf("seedance requires an OpenAI API key account with a custom base URL")
	}
	base, err := s.validateUpstreamBaseURL(account.GetCredential("base_url"))
	if err != nil {
		return nil, err
	}
	target, err := buildSeedanceURL(base, endpoint, strings.TrimPrefix(taskID, "seedance:"))
	if err != nil {
		return nil, err
	}
	model, upstreamModel := "", ""
	method := http.MethodGet
	switch endpoint {
	case SeedanceEndpointCreate:
		info, parseErr := ParseSeedanceRequest(body)
		if parseErr != nil {
			return nil, parseErr
		}
		model = info.Model
		upstreamModel = account.GetMappedModel(model)
		body, err = sjson.SetBytes(body, "model", upstreamModel)
		if err != nil {
			return nil, err
		}
		method = http.MethodPost
	case SeedanceEndpointDelete:
		method = http.MethodDelete
	}
	token := strings.TrimSpace(account.GetCredential("api_key"))
	if token == "" {
		return nil, fmt.Errorf("seedance account missing api_key")
	}
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)
	proxy := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	// Do not retry ambiguous asynchronous creates: the upstream may already have
	// accepted a billable job. Preserve native error codes and response bodies.
	if resp.StatusCode >= 300 {
		writeGrokMediaResponse(c, resp, responseBody, s.responseHeaderFilter)
		return nil, fmt.Errorf("seedance upstream status %d", resp.StatusCode)
	}
	result := &OpenAIForwardResult{Model: model, BillingModel: model, UpstreamModel: upstreamModel, Duration: time.Since(started), ResponseHeaders: resp.Header.Clone()}
	if endpoint == SeedanceEndpointCreate {
		id := strings.TrimSpace(gjson.GetBytes(responseBody, "id").String())
		if id == "" {
			return nil, fmt.Errorf("seedance create response missing task ID")
		}
		result.ResponseID = SeedanceTaskKey(id)
	}
	if endpoint == SeedanceEndpointStatus {
		result.ResponseID = taskID
		result.UpstreamModel = gjson.GetBytes(responseBody, "model").String()
		if gjson.GetBytes(responseBody, "status").String() == "succeeded" {
			result.VideoTaskSucceeded = true
			// Official Ark reconciliation field: usage.completion_tokens is the
			// billable token usage of the successful generation.
			result.Usage.OutputTokens = max(0, int(gjson.GetBytes(responseBody, "usage.completion_tokens").Int()))
			// The create response carries no duration, and the query response
			// exposes EITHER top-level duration (integer seconds) OR top-level
			// frames — never both. Official contract: duration = actual total
			// frames / 24, floored. Duration is billable for per-second plans, so
			// capture it here; when neither field is usable the value stays zero
			// and the existing request-parameter fallback applies (never invent a
			// duration, which would mis-bill an unbounded video).
			result.VideoDurationSeconds = SeedanceDurationSecondsFromStatusBody(responseBody)
			// C-1: 记录时长来源，供 media.seconds_source 如实标注为 upstream。
			result.VideoDurationFromStatus = result.VideoDurationSeconds > 0
			// Resolution is billed per tier; the query response carries it officially.
			// Empty stays empty so callers fail closed rather than assume 480p.
			result.VideoResolution = SeedanceResolutionFromStatusBody(responseBody)
		}
	}
	writeGrokMediaResponse(c, resp, responseBody, s.responseHeaderFilter)
	return result, nil
}
