package service

import (
	"encoding/json"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
)

// CheckArtexWSRequest inspects the actual model frame before selection/write.
// Native ingress and HTTP bridge do not implement session.update inheritance.
func CheckArtexWSRequest(cfg *config.Config, payload []byte) error {
	if EvaluateArtexGuard(config.ResolveArtexGuardMode(cfg), ArtexResponses, payload) {
		return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, ArtexGuardBlockedCode, nil)
	}
	return nil
}

// This state is connection-local protocol context, not a cyber risk profile.
// Only an actual upstream session.updated echo can establish known-effective
// fields. Local/pending/rejected updates NEVER commit; omitted response fields
// can reuse confirmed values, but response overrides are NEVER persisted.
// Unknown remote previous_response_id history is deliberately not synthesized.
type artexWSSessionGuard struct {
	mode      string
	mu        sync.Mutex
	confirmed map[string]json.RawMessage
}

func newArtexWSSessionGuard(mode string) *artexWSSessionGuard {
	return &artexWSSessionGuard{mode: mode, confirmed: map[string]json.RawMessage{}}
}
func (g *artexWSSessionGuard) ObserveUpstream(payload []byte) {
	if g == nil || g.mode == config.ArtexGuardOff {
		return
	}
	var event map[string]json.RawMessage
	if json.Unmarshal(payload, &event) != nil || artexString(event["type"]) != "session.updated" {
		return
	}
	var session map[string]json.RawMessage
	if json.Unmarshal(event["session"], &session) != nil || session == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// An echo is evidence only for fields actually present in this echo.
	// Missing keys become unknown; retaining an old proof could hard-block a
	// normal request after the provider omitted/cleared/replaced session state.
	g.confirmed = map[string]json.RawMessage{}
	for _, field := range []string{"instructions", "tools"} {
		if value, provided := session[field]; provided {
			// An echoed clear, including null/empty text/[], replaces the old value.
			// No field inference from local requests, error events or missing echo keys.
			g.confirmed[field] = append(json.RawMessage(nil), value...)
		}
	}
}
func (g *artexWSSessionGuard) CheckBeforeSend(payload []byte) error {
	if g == nil || g.mode == config.ArtexGuardOff {
		return nil
	}
	var event map[string]json.RawMessage
	if json.Unmarshal(payload, &event) != nil || event == nil {
		return nil
	}
	kind := artexString(event["type"])
	effective := event
	switch kind {
	case "session.update":
		effective = nil
		if json.Unmarshal(event["session"], &effective) != nil || effective == nil {
			return nil
		}
	case "response.create", "":
	default:
		return nil // user/tool/other conversation events are not instructions
	}
	g.mu.Lock()
	for _, field := range []string{"instructions", "tools"} {
		if _, provided := effective[field]; !provided {
			if known, ok := g.confirmed[field]; ok {
				effective[field] = append(json.RawMessage(nil), known...)
			}
		}
	}
	g.mu.Unlock()
	body, err := json.Marshal(effective)
	if err != nil {
		return nil
	}
	if EvaluateArtexGuard(g.mode, ArtexResponses, body) {
		return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, ArtexGuardBlockedCode, nil)
	}
	return nil
}
