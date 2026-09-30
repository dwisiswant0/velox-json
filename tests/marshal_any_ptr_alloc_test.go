package tests

import (
	"testing"

	vjson "github.com/velox-io/json"
)

type anyPtrPayload struct {
	ID   int      `json:"id"`
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// A *S passed through `any` must take the pointer path: no boxing of the
// interface and no reflect copy of the pointee.
func TestMarshalAnyHoldingPointerNoExtraAlloc(t *testing.T) {
	p := &anyPtrPayload{ID: 1, Name: "x", Tags: []string{"a", "b"}}
	var v any = p
	want, err := vjson.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := vjson.Marshal(v)
	if err != nil || string(got) != string(want) {
		t.Fatalf("Marshal(any) = %s, %v; want %s", got, err, want)
	}
	typed := testing.AllocsPerRun(1000, func() { _, _ = vjson.Marshal(p) })
	boxed := testing.AllocsPerRun(1000, func() { _, _ = vjson.Marshal(v) })
	if boxed > typed {
		t.Fatalf("allocs: any=%v typed=%v", boxed, typed)
	}

	var nilPtr *anyPtrPayload
	if out, _ := vjson.Marshal(any(nilPtr)); string(out) != "null" {
		t.Fatalf("typed nil in any = %s", out)
	}
}
