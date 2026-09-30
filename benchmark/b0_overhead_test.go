package benchmark

import (
	"os"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/bytedance/sonic"
	gojson "github.com/goccy/go-json"
	vjson "github.com/velox-io/json"
)

// =============================================================================
// Framework periphery-cost probes on the tiniest payload (Tiny: 5 flat
// fields). Scan and bind work is nearly free at this size, so results
// isolate the fixed cost around it: option setup, parser checkout, arena
// init, encoder scaffolding.
//
// Probes live outside the Benchmark_ namespace, so bench sweeps and the
// comparison scripts select only the library suite. Drive the report with:
//
//	TINY_PROBE=1 go -C benchmark test -run TestFrameworkOverheadTiny -v
// =============================================================================

func probeTinyUnmarshalVelox(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		var v Tiny
		if err := vjson.Unmarshal(TinyJSON, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func probeTinyUnmarshalJSONv2(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		var v Tiny
		if err := jsonv2.Unmarshal(TinyJSON, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func probeTinyUnmarshalGoJSON(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		var v Tiny
		if err := gojson.UnmarshalOf(TinyJSON, &v, gojson.DecodeNoCopyString()); err != nil {
			b.Fatal(err)
		}
	}
}

func probeTinyUnmarshalSonic(b *testing.B) {
	s := unsafeString(TinyJSON)
	b.ReportAllocs()
	for b.Loop() {
		var v Tiny
		if err := sonic.ConfigFastest.UnmarshalFromString(s, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func probeTinyMarshalVelox(b *testing.B) {
	v := loadTinyProbeValue(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := vjson.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func probeTinyMarshalJSONv2(b *testing.B) {
	v := loadTinyProbeValue(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := jsonv2.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func probeTinyMarshalGoJSON(b *testing.B) {
	v := loadTinyProbeValue(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := gojson.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

func probeTinyMarshalSonic(b *testing.B) {
	v := loadTinyProbeValue(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sonic.Marshal(v); err != nil {
			b.Fatal(err)
		}
	}
}

// loadTinyProbeValue decodes the probe payload with jsonv2, the semantic
// baseline shared by all marshal probes.
func loadTinyProbeValue(b *testing.B) *Tiny {
	var v Tiny
	if err := jsonv2.Unmarshal(TinyJSON, &v); err != nil {
		b.Fatal(err)
	}
	return &v
}

// TestFrameworkOverheadTiny reports per-call cost of the tiny probes
// across all libraries for the same payload.
func TestFrameworkOverheadTiny(t *testing.T) {
	if os.Getenv("TINY_PROBE") == "" {
		t.Skip("research probe: set TINY_PROBE=1 to run")
	}

	probes := []struct {
		name string
		fn   func(*testing.B)
	}{
		{"unmarshal_velox", probeTinyUnmarshalVelox},
		{"unmarshal_jsonv2", probeTinyUnmarshalJSONv2},
		{"unmarshal_gojson", probeTinyUnmarshalGoJSON},
		{"unmarshal_sonic", probeTinyUnmarshalSonic},
		{"marshal_velox", probeTinyMarshalVelox},
		{"marshal_jsonv2", probeTinyMarshalJSONv2},
		{"marshal_gojson", probeTinyMarshalGoJSON},
		{"marshal_sonic", probeTinyMarshalSonic},
	}
	for _, p := range probes {
		r := testing.Benchmark(p.fn)
		t.Logf("%-18s %9.1f ns/op  %5d B/op  %3d allocs/op",
			p.name, float64(r.T.Nanoseconds())/float64(r.N), r.MemBytes/uint64(r.N), r.AllocsPerOp())
	}
}
