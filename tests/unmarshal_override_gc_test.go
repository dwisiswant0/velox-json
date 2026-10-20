package tests

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	vjson "github.com/velox-io/json"
)

// oldValue is a pointee a target field holds before the decode overwrites it.
type oldValue struct {
	N [8]int `json:"N"`
}

// nestedValue is a value struct whose fields Unmarshal overwrites.
type nestedValue struct {
	S string    `json:"S"`
	P *oldValue `json:"P"`
}

// overwriteTarget is what Unmarshal decodes into.
type overwriteTarget struct {
	S  string               `json:"s"`
	P  *oldValue            `json:"p"`
	PP **oldValue           `json:"pp"`
	M  map[string]*oldValue `json:"m"`
	I  any                  `json:"i"`
	ST nestedValue          `json:"st"`
	AR [2]*oldValue         `json:"ar"`
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

// overwriteDecoder is a decoder a case runs against, instantiated for the
// type of copy that case keeps. pin shades that copy: std's stores go
// through the barrier and shade it themselves, so std passes the copy
// through, while vjson's stores skip the barrier and leave the shading to
// the caller, the contract vjson.Pin documents.
type overwriteDecoder[T any] struct {
	name      string
	unmarshal func(data []byte, v *overwriteTarget) error
	keep      func(T) T
}

// noPin is the shading of a decoder whose own stores already shade.
func noPin[T any](x T) T { return x }

// overwriteDecodersFor builds the decoder set per instantiation: pin is
// generic in the copy's type, so no single value can carry it for every case.
func overwriteDecodersFor[T pinnedRef]() []overwriteDecoder[T] {
	return []overwriteDecoder[T]{
		{"vjson", func(d []byte, v *overwriteTarget) error { return vjson.Unmarshal(d, v) }, vjson.Pin[T]},
		{"std", func(d []byte, v *overwriteTarget) error { return json.Unmarshal(d, v) }, noPin[T]},
	}
}

// pinnedRef is the copy of a field's old value a case keeps. The copy sits in
// a stack slot mark already scanned, so it must stay a word the GC scans:
// every term is pointer shaped and shares one stencil.
type pinnedRef interface {
	// oldValue | *oldValue | **oldValue | nestedValue | [2]*oldValue | map[string]*oldValue | any
	any
}

// runOverrideGC runs one case against every decoder: ref copies a field's old
// value out, the decode overwrites the field, and the next cycle must still
// reach the old value through the copy.
func runOverrideGC[T pinnedRef](t *testing.T, name, doc string, newTarget func() *overwriteTarget, ref func(*overwriteTarget) T) {
	t.Helper()
	for _, dec := range overwriteDecodersFor[T]() {
		t.Run(dec.name+"/"+name, func(t *testing.T) {
			for range 30 {
				// The old value is reachable only through the target's
				// field, and the target only through the chain tail.
				chain := newChainToTarget(newTarget(), 1_000_000)

				// Start a gc cycle. Mark scans root stacks before heap
				// objects, so this parked stack is scanned now: it holds
				// the chain head, with the target at the far end.
				done := make(chan struct{})
				go func() { runtime.GC(); close(done) }()

				// Leave a window for the GC's mark phase to scan stacks first,
				// then heaps: prevent the root scan from covering old, copied
				// after this park. One millisecond spans the scan and leaves
				// mark mid-chain.
				time.Sleep(time.Millisecond)

				// Reach the tail before mark does. Mark walks the same
				// chain node by node but pays per-node scan work, so this
				// load loop gets there first and the copy below runs
				// before mark reads the field.
				target := chainTail(chain)

				// Stacks are scanned once per cycle, so this copy is
				// invisible to the running cycle. The target's field is
				// now the only path to the old value that mark can still
				// follow. A decoder whose stores skip the barrier leaves
				// the shading to the caller.
				old := dec.keep(ref(target))

				// The overwrite cuts that path (ref)
				if err := dec.unmarshal([]byte(doc), target); err != nil {
					t.Fatal(err)
				}
				<-done

				// The next cycle reaches the freed old value through var 'old'
				// and throws "found pointer to free object".
				runtime.GC()
				runtime.KeepAlive(old) // use old
			}
		})
	}
}

// TestUnmarshalOverrideNonNilGC: a caller copies a field's old value to its
// stack, then Unmarshal overwrites the field. When the copy lands after mark
// scanned that stack, the field is the only path to the old value that mark
// still follows, so the old value must be shaded before the overwrite, as a
// Go store's write barrier shades it. std is the control: its stores carry
// the barrier, so its copy needs nothing. vjson's stores skip it, so the
// copy is shaded by vjson.Pin, the contract a caller holding a value across
// an Unmarshal relies on.
func TestUnmarshalOverrideNonNilGC(t *testing.T) {
	runOverrideGC(t, "string", `{"s":"new"}`,
		func() *overwriteTarget { return &overwriteTarget{S: strings.Repeat("o", 256)} },
		func(v *overwriteTarget) string { return v.S })

	runOverrideGC(t, "pointer", `{"p":null}`,
		func() *overwriteTarget { return &overwriteTarget{P: &oldValue{}} },
		func(v *overwriteTarget) *oldValue { return v.P })

	runOverrideGC(t, "map value", `{"m":{"k":{"N":[1]}}}`,
		func() *overwriteTarget { return &overwriteTarget{M: map[string]*oldValue{"k": {}}} },
		func(v *overwriteTarget) *oldValue { return v.M["k"] })

	// Null clears the map header itself, dropping the hmap rather
	// than a value inside it.
	runOverrideGC(t, "map", `{"m":null}`,
		func() *overwriteTarget { return &overwriteTarget{M: map[string]*oldValue{"k": {}}} },
		func(v *overwriteTarget) map[string]*oldValue { return v.M })

	// Null clears the interface, dropping the boxed pointer.
	runOverrideGC(t, "interface", `{"i":null}`,
		func() *overwriteTarget { return &overwriteTarget{I: &oldValue{}} },
		func(v *overwriteTarget) any { return v.I })

	// A value struct is overwritten field by field, so pinning it
	// must shade every pointer word the copy holds. The kept word
	// sits at a nonzero field offset.
	runOverrideGC(t, "struct", `{"st":{"S":"new","P":null}}`,
		func() *overwriteTarget {
			return &overwriteTarget{ST: nestedValue{S: strings.Repeat("o", 256), P: &oldValue{}}}
		},
		func(v *overwriteTarget) nestedValue { return v.ST })

	// Null clears each non-nil element, dropping both pointers. The
	// kept word is the second element, so the stride is asserted.
	runOverrideGC(t, "array", `{"ar":[null,null]}`,
		func() *overwriteTarget { return &overwriteTarget{AR: [2]*oldValue{{}, {}}} },
		func(v *overwriteTarget) [2]*oldValue { return v.AR })

	// Null clears the outermost slot, dropping the box and, through
	// it, the leaf, both without a barrier.
	runOverrideGC(t, "double pointer", `{"pp":null}`,
		func() *overwriteTarget {
			leaf := &oldValue{}
			return &overwriteTarget{PP: &leaf}
		},
		func(v *overwriteTarget) **oldValue { return v.PP })

	// A value with a nil leaf replaces the leaf pointer slot inside
	// the box, the multi-level fill path.
	runOverrideGC(t, "double pointer fill", `{"pp":{"N":[1]}}`,
		func() *overwriteTarget { var leaf *oldValue; return &overwriteTarget{PP: &leaf} },
		func(v *overwriteTarget) **oldValue { return v.PP })
}
