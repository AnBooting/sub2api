package middleware

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ArtexGuard is mounted after existing authentication/group/body-limit gates.
// Only these actual LLM JSON POST routes are inspected. No Gemini/media/audit
// classification, permanent ban, URL lookup, header or recursive metadata scan.
func ArtexGuard(cfg *config.Config) gin.HandlerFunc {
	mode := config.ResolveArtexGuardMode(cfg)
	return func(c *gin.Context) {
		if mode == config.ArtexGuardOff || c.Request == nil || c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		protocol, ok := artexRouteProtocol(c.FullPath())
		if !ok {
			c.Next()
			return
		}
		if protocol == service.ArtexResponses {
			// This helper validates path characters, not endpoint semantics.
			// Bare Responses and compact are the proved model-request endpoints;
			// keep all other suffixes (counting/control/unknown) on original dispatch.
			suffix := strings.TrimRight(c.Param("subpath"), "/")
			if !service.IsForwardableOpenAIResponsesRequestPath(c) ||
				(strings.HasSuffix(c.FullPath(), "/*subpath") && suffix != "" && suffix != "/compact") {
				c.Next()
				return
			}
		}
		// Do not introduce content decoding or header mutation at this gate.
		var body []byte
		var err error
		if preread, ok := c.Request.Body.(*httputil.PrereadBody); ok {
			body = preread.Bytes()
		} else if c.Request.Body != nil {
			body, err = io.ReadAll(c.Request.Body)
		}
		if err != nil {
			status := http.StatusBadRequest
			message := "Failed to read request body"
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
				message = "Request body is too large"
			}
			c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
			return
		}
		// Reset ONLY the body cursor. ResetRequestBody also rewrites length/header
		// metadata and must not be introduced by this no-transform gate.
		if c.Request.Body != nil {
			_ = c.Request.Body.Close()
		}
		c.Request.Body = httputil.NewPrereadBody(body)
		if service.EvaluateArtexGuard(mode, protocol, body) {
			failure := gin.H{"type": "permission_error", "code": service.ArtexGuardBlockedCode, "message": "This client execution protocol is not permitted"}
			envelope := gin.H{"error": failure}
			if protocol == service.ArtexAnthropic {
				envelope["type"] = "error"
			}
			c.AbortWithStatusJSON(http.StatusForbidden, envelope)
			return
		}
		c.Next()
	}
}
func artexRouteProtocol(path string) (service.ArtexProtocol, bool) {
	switch path {
	case "/v1/messages", "/antigravity/v1/messages":
		return service.ArtexAnthropic, true
	case "/v1/chat/completions", "/chat/completions":
		return service.ArtexChat, true
	case "/v1/responses", "/v1/responses/*subpath", "/responses", "/responses/*subpath", "/backend-api/codex/responses", "/backend-api/codex/responses/*subpath":
		return service.ArtexResponses, true
	default:
		return "", false
	}
}
