package benchmark

import (
	"testing"
)

// =============================================================================
// LLM API: a request and a response of the Chat Completions API
//
// Three payloads, measured in both directions:
//
//	LLMTools    - the definitions of five tools only: a map[string]JSONSchema
//	              per tool, whose values are 144 bytes and so are kept out of
//	              the buckets of the map. It isolates the maps from the strings.
//	LLMRequest  - one turn of a session: the tools, five tool calls and their
//	              results, one of which is a source file of 15 KB. Escape-heavy.
//	LLMResponse - the answer of the model. A client encodes a request which is
//	              much larger than the response it decodes, so the response is
//	              its own payload instead of a scaled-down request.
//
// The JSON is the one of the standard library, so every library decodes the
// same bytes. See llm_payload.go for how the payloads are built.
// =============================================================================

// ---------------------------------------------------------------- LLMTools

func Benchmark_Marshal_LLMTools_Sonic(b *testing.B)  { benchMarshalSonic(b, loadLLMToolsValue()) }
func Benchmark_Marshal_LLMTools_GoJSON(b *testing.B) { benchMarshalGoJSON(b, loadLLMToolsValue()) }
func Benchmark_Marshal_LLMTools_JSONv2(b *testing.B) { benchMarshalJSONv2(b, loadLLMToolsValue()) }
func Benchmark_Marshal_LLMTools_Velox(b *testing.B)  { benchMarshalVelox(b, loadLLMToolsValue()) }

func Benchmark_Unmarshal_LLMTools_Sonic(b *testing.B) {
	benchUnmarshalSonic[ChatCompletionRequest](b, LoadLLMToolsJSON())
}
func Benchmark_Unmarshal_LLMTools_GoJSON(b *testing.B) {
	benchUnmarshalGoJSON[ChatCompletionRequest](b, LoadLLMToolsJSON())
}
func Benchmark_Unmarshal_LLMTools_JSONv2(b *testing.B) {
	benchUnmarshalJSONv2[ChatCompletionRequest](b, LoadLLMToolsJSON())
}
func Benchmark_Unmarshal_LLMTools_Velox(b *testing.B) {
	benchUnmarshalVelox[ChatCompletionRequest](b, LoadLLMToolsJSON())
}

// ---------------------------------------------------------------- LLMRequest

func Benchmark_Marshal_LLMRequest_Sonic(b *testing.B)  { benchMarshalSonic(b, loadLLMRequestValue()) }
func Benchmark_Marshal_LLMRequest_GoJSON(b *testing.B) { benchMarshalGoJSON(b, loadLLMRequestValue()) }
func Benchmark_Marshal_LLMRequest_JSONv2(b *testing.B) { benchMarshalJSONv2(b, loadLLMRequestValue()) }
func Benchmark_Marshal_LLMRequest_Velox(b *testing.B)  { benchMarshalVelox(b, loadLLMRequestValue()) }

func Benchmark_Unmarshal_LLMRequest_Sonic(b *testing.B) {
	benchUnmarshalSonic[ChatCompletionRequest](b, LoadLLMRequestJSON())
}
func Benchmark_Unmarshal_LLMRequest_GoJSON(b *testing.B) {
	benchUnmarshalGoJSON[ChatCompletionRequest](b, LoadLLMRequestJSON())
}
func Benchmark_Unmarshal_LLMRequest_JSONv2(b *testing.B) {
	benchUnmarshalJSONv2[ChatCompletionRequest](b, LoadLLMRequestJSON())
}
func Benchmark_Unmarshal_LLMRequest_Velox(b *testing.B) {
	benchUnmarshalVelox[ChatCompletionRequest](b, LoadLLMRequestJSON())
}

// ---------------------------------------------------------------- LLMResponse

func Benchmark_Marshal_LLMResponse_Sonic(b *testing.B) { benchMarshalSonic(b, loadLLMResponseValue()) }
func Benchmark_Marshal_LLMResponse_GoJSON(b *testing.B) {
	benchMarshalGoJSON(b, loadLLMResponseValue())
}
func Benchmark_Marshal_LLMResponse_JSONv2(b *testing.B) {
	benchMarshalJSONv2(b, loadLLMResponseValue())
}
func Benchmark_Marshal_LLMResponse_Velox(b *testing.B) { benchMarshalVelox(b, loadLLMResponseValue()) }

func Benchmark_Unmarshal_LLMResponse_Sonic(b *testing.B) {
	benchUnmarshalSonic[ChatCompletionResponse](b, LoadLLMResponseJSON())
}
func Benchmark_Unmarshal_LLMResponse_GoJSON(b *testing.B) {
	benchUnmarshalGoJSON[ChatCompletionResponse](b, LoadLLMResponseJSON())
}
func Benchmark_Unmarshal_LLMResponse_JSONv2(b *testing.B) {
	benchUnmarshalJSONv2[ChatCompletionResponse](b, LoadLLMResponseJSON())
}
func Benchmark_Unmarshal_LLMResponse_Velox(b *testing.B) {
	benchUnmarshalVelox[ChatCompletionResponse](b, LoadLLMResponseJSON())
}
