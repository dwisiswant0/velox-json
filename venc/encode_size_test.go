package venc

import (
	"reflect"
	"testing"
	"unsafe"
)

type sizeLeaf struct {
	Tags []string `json:"tags"`
	Name string   `json:"name,omitempty"`
}

type sizeNode struct {
	*sizeLeaf
	Next any `json:"next,omitempty"`
}

// TestSizeHintPromotedThroughPointer pins the size hint of fields promoted
// across an embedded pointer: their offsets are relative to the pointee, so
// the sizer must walk the hop instead of reading the outer struct's words.
func TestSizeHintPromotedThroughPointer(t *testing.T) {
	ti := EncTypeInfoOf(reflect.TypeFor[sizeNode]())
	cases := []struct {
		name string
		v    *sizeNode
	}{
		{"set", &sizeNode{sizeLeaf: &sizeLeaf{Tags: []string{"t"}, Name: "n"}, Next: &sizeNode{}}},
		{"nil-embed", &sizeNode{Next: "x"}},
	}
	for _, c := range cases {
		out, err := Marshal(c.v)
		if err != nil {
			t.Fatalf("%s: Marshal: %v", c.name, err)
		}
		hint := encodingSizeHint(ti, unsafe.Pointer(c.v))
		if hint > 4*len(out)+256 {
			t.Errorf("%s: size hint %d for a %d-byte output", c.name, hint, len(out))
		}
	}
}
