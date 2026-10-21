package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression for three false rejections / false acceptances found in review.
//
//  1. An omitted "strict" was rejected: the converter applies defaultStrictFalse, so
//     the converted side carries &false while the original is nil, and
//     reflect.DeepEqual(&false, nil) is false. Clients rarely send strict, so this
//     misclassified the common case as unsupported.
func TestChatBridgeResponsesToolOmittingStrictIsCompatible(t *testing.T) {
	body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}}]}`)
	require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(body, "gpt-5.5", chatBridgeResponses).Class)

	// An explicit false is the same value and must also pass.
	explicitFalse := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup","strict":false,"parameters":{"type":"object"}}}]}`)
	require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(explicitFalse, "gpt-5.5", chatBridgeResponses).Class)

	// An explicit true is preserved verbatim by the converter (defaultStrictFalse
	// only rewrites nil), so it must also pass: the definition is unchanged.
	explicitTrue := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup","strict":true,"parameters":{"type":"object"}}}]}`)
	require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(explicitTrue, "gpt-5.5", chatBridgeResponses).Class)
}

//  2. A function tool that omits "parameters" was rejected: the converter copies
//     Parameters verbatim, so both sides are nil, but json.Valid(nil) is false and
//     the two absent values were reported as a changed definition.
func TestChatBridgeEmptyParametersCompareEqual(t *testing.T) {
	require.True(t, chatBridgeJSONEqual(nil, nil), "two absent values are equal")
	require.True(t, chatBridgeJSONEqual(nil, []byte("")), "absent and empty are equal")
	require.True(t, chatBridgeJSONEqual([]byte("  "), nil), "blank and absent are equal")

	body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`)
	require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(body, "gpt-5.5", chatBridgeResponses).Class)

	// A genuinely missing key on one side must still be unequal.
	require.False(t, chatBridgeJSONEqual([]byte(`{"type":"object"}`), nil))
}

//  3. The Responses gate converted with the client-visible model while the forward
//     path rewrites the model to the frozen upstream model before converting. The
//     converter drops temperature/top_p for reasoning models, so a cross-family
//     bridge passed the sampling check and then silently lost the client's sampling
//     parameters. The gate must assess the conversion that will actually run.
func TestChatBridgeResponsesGateUsesFrozenModelForConversion(t *testing.T) {
	// Client-visible model is not a reasoning model, the frozen upstream model is.
	body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}],"temperature":0.7,"top_p":0.8}`)

	// With a reasoning frozen model the converter drops sampling, so the gate must
	// report the loss instead of claiming compatibility.
	require.Equal(t, chatBridgeUnsupported, assessChatBridgeCapability(body, "gpt-5.5", chatBridgeResponses).Class)

	// With a non-reasoning frozen model the conversion preserves sampling.
	require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(body, "gpt-4o", chatBridgeResponses).Class)
}
