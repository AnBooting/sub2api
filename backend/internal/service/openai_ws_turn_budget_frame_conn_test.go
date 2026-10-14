//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type openAIWSTurnBudgetFrameConnSpy struct {
	ctxs []context.Context
}

func (s *openAIWSTurnBudgetFrameConnSpy) ReadFrame(context.Context) (coderws.MessageType, []byte, error) {
	return coderws.MessageText, nil, errors.New("not used")
}

func (s *openAIWSTurnBudgetFrameConnSpy) WriteFrame(ctx context.Context, _ coderws.MessageType, _ []byte) error {
	s.ctxs = append(s.ctxs, ctx)
	return nil
}

func (s *openAIWSTurnBudgetFrameConnSpy) Close() error { return nil }

func TestOpenAIWSTurnBudgetFrameConnCountsOnlyResponseCreateAndResetsPerTurn(t *testing.T) {
	spy := &openAIWSTurnBudgetFrameConnSpy{}
	turn1Budget := NewRequestAttemptBudget(time.Now(), 1, time.Hour, time.Time{})
	turn2Budget := NewRequestAttemptBudget(time.Now(), 1, time.Hour, time.Time{})
	turnCtx1 := WithRequestAttemptBudget(context.Background(), turn1Budget)
	turnCtx2 := WithRequestAttemptBudget(context.Background(), turn2Budget)
	// 模拟 turn1 内部隐藏重试已经通过真实发送钩子消耗一次：下一个
	// response.create 应被预算拒绝。
	require.NoError(t, allowUpstreamSend(turnCtx1))
	wrapped := &openAIWSTurnBudgetFrameConn{
		inner: spy,
		resolve: func(turn int) context.Context {
			if turn == 1 {
				return turnCtx1
			}
			return turnCtx2
		},
	}

	err := wrapped.WriteFrame(context.Background(), coderws.MessageText, []byte("{\"type\":\"session.update\"}"))
	require.NoError(t, err, "非 response.create 帧不消耗预算")
	require.Len(t, spy.ctxs, 1)

	err = wrapped.WriteFrame(context.Background(), coderws.MessageText, []byte("{\"type\":\"response.create\",\"model\":\"gpt-5.1\"}"))
	var budgetFailover *UpstreamFailoverError
	require.ErrorAs(t, err, &budgetFailover)
	require.Equal(t, RequestAttemptBudgetReason, budgetFailover.Reason)
	require.Len(t, spy.ctxs, 1, "预算拒绝不得真正写上游")

	err = wrapped.WriteFrame(context.Background(), coderws.MessageText, []byte("{\"type\":\"response.create\",\"model\":\"gpt-5.1\"}"))
	require.NoError(t, err, "turn2 使用新预算，turn1 耗尽不影响 turn2")
	require.Len(t, spy.ctxs, 2)
	require.Same(t, turn2Budget, RequestAttemptBudgetFromContext(spy.ctxs[1]))
	require.Equal(t, 1, turn1Budget.Used())
	require.Equal(t, 1, turn2Budget.Used())
}

func TestOpenAIWSTurnBudgetFrameConnRejectsWithFailoverError(t *testing.T) {
	budget := NewRequestAttemptBudget(time.Now(), 1, time.Hour, time.Time{})
	require.NoError(t, budget.AllowSend())
	wrapped := &openAIWSTurnBudgetFrameConn{
		inner: &openAIWSTurnBudgetFrameConnSpy{},
		resolve: func(int) context.Context {
			return WithRequestAttemptBudget(context.Background(), budget)
		},
	}
	err := wrapped.WriteFrame(context.Background(), coderws.MessageText, []byte("{\"type\":\"response.create\"}"))
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, RequestAttemptBudgetReason, failoverErr.Reason)
	require.False(t, failoverErr.ShouldRetryNextAccount())
	require.Equal(t, RequestAttemptBudgetReason, failoverErr.Reason)
}

var _ openaiwsv2.FrameConn = (*openAIWSTurnBudgetFrameConn)(nil)
