package benchmark

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
	"unsafe"

	"github.com/bytedance/sonic"
	gojson "github.com/goccy/go-json"
	vjson "github.com/velox-io/json"
)

// Shared per-library decode/encode drivers. Each benchmark function keeps
// its Benchmark_ name and delegates the loop here.

// marshalSize returns the JSON output size for SetBytes throughput reporting.
func marshalSize(v any) int64 {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return int64(len(data))
}

// unsafeString returns a string aliasing data without copying, the same
// conversion sonic's Unmarshal performs on its []byte input.
func unsafeString(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}

func benchUnmarshalSonic[T any](b *testing.B, data []byte) {
	s := unsafeString(data)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var v T
		if err := sonic.ConfigFastest.UnmarshalFromString(s, &v); err != nil {
			b.Fatal(err)
		}
	}
}

// benchUnmarshalGoJSON decodes with go-json in no-copy string mode, so decoded
// strings alias the input buffer like the velox default Unmarshal.
func benchUnmarshalGoJSON[T any](b *testing.B, data []byte) {
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var v T
		if err := gojson.UnmarshalOf(data, &v, gojson.DecodeNoCopyString()); err != nil {
			b.Fatal(err)
		}
	}
}

func benchUnmarshalJSONv2[T any](b *testing.B, data []byte) {
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var v T
		if err := jsonv2.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func benchUnmarshalVelox[T any](b *testing.B, data []byte) {
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		var v T
		if err := vjson.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func benchMarshalSonic(b *testing.B, v any) {
	b.SetBytes(marshalSize(v))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sonic.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func benchMarshalGoJSON(b *testing.B, v any) {
	b.SetBytes(marshalSize(v))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := gojson.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func benchMarshalJSONv2(b *testing.B, v any) {
	b.SetBytes(marshalSize(v))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := jsonv2.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func benchMarshalVelox(b *testing.B, v any) {
	b.SetBytes(marshalSize(v))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := vjson.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}
