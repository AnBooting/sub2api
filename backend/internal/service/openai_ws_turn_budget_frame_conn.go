package service

import (
	"context"
	"strings"
	"sync"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

// openAIWSTurnBudgetFrameConn 给 passthrough 上游写帧挂 TS-02 预算：只在
// response.create 上计一次真实推理发送，并优先使用 TurnContext 返回的本 turn
// ctx（含新预算）。非 response.create 帧（session.update/response.cancel 等）
// 不消耗预算。预算拒绝直接返回终止型 failover，使 handler 复用耗尽出口。
type openAIWSTurnBudgetFrameConn struct {
	inner   openaiwsv2.FrameConn
	resolve func(turn int) context.Context

	mu   sync.Mutex
	turn int
}

var _ openaiwsv2.FrameConn = (*openAIWSTurnBudgetFrameConn)(nil)

func (c *openAIWSTurnBudgetFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if c == nil || c.inner == nil {
		return coderws.MessageText, nil, errOpenAIWSConnClosed
	}
	return c.inner.ReadFrame(ctx)
}

func (c *openAIWSTurnBudgetFrameConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	if c == nil || c.inner == nil {
		return errOpenAIWSConnClosed
	}
	if msgType != coderws.MessageText || strings.TrimSpace(gjson.GetBytes(payload, "type").String()) != "response.create" {
		return c.inner.WriteFrame(ctx, msgType, payload)
	}
	c.mu.Lock()
	c.turn++
	turn := c.turn
	c.mu.Unlock()
	writeCtx := ctx
	if c.resolve != nil {
		if turnCtx := c.resolve(turn); turnCtx != nil {
			writeCtx = turnCtx
		}
	}
	if err := allowUpstreamSend(writeCtx); err != nil {
		return NewRequestAttemptBudgetFailoverError()
	}
	return c.inner.WriteFrame(writeCtx, msgType, payload)
}

func (c *openAIWSTurnBudgetFrameConn) Close() error {
	if c == nil || c.inner == nil {
		return errOpenAIWSConnClosed
	}
	return c.inner.Close()
}
