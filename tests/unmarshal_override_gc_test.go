//go:build vj_test_overideptr

package tests

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	vjson "github.com/velox-io/json"
)

// oldValue is a pointee a target field holds before the decode overwrites it.
type oldValue struct{ N [8]int }

// overwriteTarget is what Unmarshal decodes into.
type overwriteTarget struct {
	S string               `json:"s"`
	P *oldValue            `json:"p"`
	M map[string]*oldValue `json:"m"`
}

// chainNode links a target behind a long chain, so mark reaches it last.
type chainNode struct {
	next   *chainNode
	target *overwriteTarget
}

func newChainToTarget(target *overwriteTarget, n int) *chainNode {
	head := &chainNode{target: target}
	for range n - 1 {
		head = &chainNode{next: head}
	}
	return head
}

func chainTail(head *chainNode) *overwriteTarget {
	for head.next != nil {
		head = head.next
	}
	return head.target
}

type overwriteCase struct {
	name      string
	doc       string                                // overwrites the field holding the old value
	newTarget func() *overwriteTarget               // target whose field holds a fresh old value
	oldOf     func(*overwriteTarget) unsafe.Pointer // reads the old value out of the field
}

// TestUnmarshalOverrideNonNilGC: a caller copies a field's old value to its
// stack, then Unmarshal overwrites the field. When the copy lands after mark
// scanned that stack, the field is the only path to the old value that mark
// still follows, so the overwrite must shade the old value, as a Go store's
// write barrier does. std is the control: its stores go through the barrier.
func TestUnmarshalOverrideNonNilGC(t *testing.T) {
	cases := []overwriteCase{
		{
			name:      "string",
			doc:       `{"s":"new"}`,
			newTarget: func() *overwriteTarget { return &overwriteTarget{S: strings.Repeat("o", 256)} },
			oldOf:     func(v *overwriteTarget) unsafe.Pointer { return unsafe.Pointer(unsafe.StringData(v.S)) },
		},
		{
			name:      "pointer",
			doc:       `{"p":null}`,
			newTarget: func() *overwriteTarget { return &overwriteTarget{P: &oldValue{}} },
			oldOf:     func(v *overwriteTarget) unsafe.Pointer { return unsafe.Pointer(v.P) },
		},
		{
			name:      "map value",
			doc:       `{"m":{"k":{"N":[1]}}}`,
			newTarget: func() *overwriteTarget { return &overwriteTarget{M: map[string]*oldValue{"k": {}}} },
			oldOf:     func(v *overwriteTarget) unsafe.Pointer { return unsafe.Pointer(v.M["k"]) },
		},
	}
	decoders := []struct {
		name string
		fn   func(data []byte, v *overwriteTarget) error
	}{
		{"std", func(d []byte, v *overwriteTarget) error { return json.Unmarshal(d, v) }},
		{"vjson", func(d []byte, v *overwriteTarget) error { return vjson.Unmarshal(d, v) }},
	}
	for _, dec := range decoders {
		for _, c := range cases {
			t.Run(dec.name+"/"+c.name, func(t *testing.T) {
				for range 30 {
					// The old value is reachable only through the target's
					// field, and the target only through the chain tail.
					chain := newChainToTarget(c.newTarget(), 1_000_000)

					// Start a cycle. Mark scans root stacks before heap
					// objects, so this parked stack is scanned now: it holds
					// the chain head, with the target at the far end.
					done := make(chan struct{})
					go func() { runtime.GC(); close(done) }()
					time.Sleep(time.Millisecond)

					// Mark is still walking the chain. A plain load loop
					// outruns it to the target, and the old value lands on
					// the scanned stack. The target's field stays the only
					// path to it that mark will follow.
					target := chainTail(chain)
					old := c.oldOf(target)

					// The overwrite cuts that path. A barriered store shades
					// the old value; a raw store leaves it white, and the
					// cycle frees it.
					if err := dec.fn([]byte(c.doc), target); err != nil {
						t.Fatal(err)
					}
					<-done

					// The next cycle reaches the freed old value through old
					// and throws "found pointer to free object".
					runtime.GC()
					runtime.KeepAlive(old)
				}
			})
		}
	}
}
