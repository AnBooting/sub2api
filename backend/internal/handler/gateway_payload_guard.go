package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// N-03 网关载荷守卫：拒绝空 messages，以及没有输入或上下文的 Responses 请求。
// 覆盖 /v1/chat/completions、/v1/messages、/v1/responses 的全部网关切片
// （GatewayHandler 与 OpenAIGatewayHandler）。错误响应沿用各入口自己的协议
// 格式化器，保证 OpenAI / Anthropic 两种错误形状不被破坏。

// gatewayPayloadErrorWriter 与各 handler 的 errorResponse 系列方法同签名。
type gatewayPayloadErrorWriter = func(c *gin.Context, status int, code, message string)

// rejectEmptyMessages 校验 `messages` 必须是非空数组（Anthropic Messages 与
// OpenAI Chat Completions 两种协议共用）。命中时写 400 并返回 true。
func rejectEmptyMessages(c *gin.Context, body []byte, writeErr gatewayPayloadErrorWriter) bool {
	msgs := gjson.GetBytes(body, "messages")
	if msgs.IsArray() && len(msgs.Array()) > 0 {
		return false
	}
	writeErr(c, http.StatusBadRequest, "invalid_request_error", "messages is required")
	return true
}

// Responses input is optional: instructions, a previous response, a conversation,
// or a saved prompt can provide context instead. Ownership and endpoint capability
// checks still run after this guard; a context reference does not bypass them.
func rejectEmptyResponsesInput(c *gin.Context, body []byte, writeErr gatewayPayloadErrorWriter) bool {
	input := gjson.GetBytes(body, "input")
	switch {
	case input.Type == gjson.String:
		if strings.TrimSpace(input.String()) != "" {
			return false
		}
	case input.IsArray():
		if len(input.Array()) > 0 {
			return false
		}
	case input.Exists() && input.Type != gjson.Null:
		writeErr(c, http.StatusBadRequest, "invalid_request_error", "input must be a string or an array")
		return true
	}
	for _, path := range []string{"instructions", "previous_response_id", "conversation", "conversation.id", "prompt.id"} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return false
		}
	}
	writeErr(c, http.StatusBadRequest, "invalid_request_error", "input is required")
	return true
}
