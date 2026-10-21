//go:build unit

package service

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// geminiCompletedFrameChunk 是带 finishReason 与 usageMetadata 的结束分片：
// 上游据此判定应答完整并对该次请求计费。
const geminiCompletedFrameChunk = `data: {"candidates":[{"content":{"parts":[{"text":" world"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}` + "\n\n"

// TestGeminiForwardNative_CompletedResponseReadErrorKeepsBillableUsage native 路径：
// 结束分片（finishReason=STOP）与 usage 已经到达客户端之后，上游又抛出读错误
// （io.ErrUnexpectedEOF）。此时完整响应**已经发给客户端**、上游**也已经计费**，
// 所以 ForwardNative 必须同时返回错误与带用量的结果，调用方才能照常记账。
//
// 回归背景：合并时给 native 路径误加了"尚未收到结束分片"条件，使这条路径返回
// (nil, err)，ForwardNative 随即丢弃结果 → 已发给客户端的完整响应不记账。
func TestGeminiForwardNative_CompletedResponseReadErrorKeepsBillableUsage(t *testing.T) {
	reqBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	svc, c, rec := newGeminiAbortingStreamService(t, "/v1beta/models/gemini-3-flash-preview:streamGenerateContent",
		reqBody, geminiAbortTextChunk+geminiCompletedFrameChunk, false)

	result, err := svc.ForwardNative(context.Background(), c, geminiVertexAPIKeyTestAccount(""),
		"gemini-3-flash-preview", "streamGenerateContent", true, reqBody)

	require.ErrorIs(t, err, io.ErrUnexpectedEOF, "上游读错误仍然要上报")
	require.NotNil(t, result, "完整响应已发给客户端，必须保留结果让调用方记账")
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Contains(t, rec.Body.String(), "finishReason", "结束分片已经写给客户端")
}

// TestGeminiCompatForward_CompletedResponseReadErrorKeepsBillableUsage Claude 兼容路径：
// 同 native 的口径——结束分片与 usage 已经到达客户端之后的上游读错误不得吃掉用量。
//
// 回归背景：合并时给兼容路径误加了"尚未收到结束分片"（finishReason == ""）条件，
// 使这条路径返回 (nil, err) → 已发给客户端的完整响应不记账。
func TestGeminiCompatForward_CompletedResponseReadErrorKeepsBillableUsage(t *testing.T) {
	reqBody := []byte(`{"model":"gemini-3-flash-preview","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	svc, c, rec := newGeminiAbortingStreamService(t, "/v1/messages",
		reqBody, geminiAbortTextChunk+geminiCompletedFrameChunk, false)

	result, err := svc.Forward(context.Background(), c, geminiVertexAPIKeyTestAccount(""), reqBody)

	require.ErrorIs(t, err, io.ErrUnexpectedEOF, "上游读错误仍然要上报")
	require.NotNil(t, result, "完整响应已发给客户端，必须保留结果让调用方记账")
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Contains(t, rec.Body.String(), "content_block_delta", "内容分片已经写给客户端")
}
