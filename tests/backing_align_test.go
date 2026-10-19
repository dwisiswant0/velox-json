package tests

// A pointerless element keeps the scalar's alignment, so a span that is a whole
// number of such elements can start or end off the word grid. The runtime's
// pointer-aware clear aborts on those arguments, so every site that zeroes a
// typed span needs the barrier-free clear for a pointerless type. Each case
// below aborts the process without it.

import (
	"slices"
	"strconv"
	"testing"

	vjson "github.com/velox-io/json"
	"github.com/velox-io/json/stream"
)

// A fixed array binds the elements its document supplied and zeroes the rest.
// The tail of [3]float32 after one element starts 4 bytes in.
func TestUnmarshalFixedArrayPointerlessTail(t *testing.T) {
	var v struct {
		A [3]float32 `json:"a"`
	}
	v.A = [3]float32{9, 9, 9}
	if err := vjson.Unmarshal([]byte(`{"a":[1]}`), &v); err != nil {
		t.Fatalf("%v", err)
	}
	if v.A != [3]float32{1, 0, 0} {
		t.Fatalf("A = %v, want the supplied element followed by zeroes", v.A)
	}

	var empty struct {
		A [3]float32 `json:"a"`
	}
	empty.A = [3]float32{9, 9, 9}
	if err := vjson.Unmarshal([]byte(`{"a":[]}`), &empty); err != nil {
		t.Fatalf("%v", err)
	}
	if empty.A != [3]float32{} {
		t.Fatalf("A = %v, want every element zeroed", empty.A)
	}
}

// A 4-byte key type decodes through UnmarshalText into a key-sized scratch,
// which the drain zeroes before and after the hook.
type scalarKey float32

func (k *scalarKey) UnmarshalText(b []byte) error {
	f, err := strconv.ParseFloat(string(b), 32)
	*k = scalarKey(f)
	return err
}

func TestUnmarshalPointerlessTextKey(t *testing.T) {
	var v map[scalarKey]int
	if err := vjson.Unmarshal([]byte(`{"1":2}`), &v); err != nil {
		t.Fatalf("%v", err)
	}
	if len(v) != 1 || v[1] != 2 {
		t.Fatalf("v = %v, want one entry keyed by 1", v)
	}
}

// A leaf stream reuses a fixed four-element batch, so a one-byte element makes
// the whole backing four bytes: an element count off the word grid. The reuse
// starts with the second batch, which needs a fifth element to reach.
type streamBatchElem struct {
	A uint8 `json:"a"`
}

func TestStreamPointerlessBatchReuse(t *testing.T) {
	var v struct {
		S vjson.Stream[streamBatchElem] `json:"s"`
	}
	var got []uint8
	v.S.OnRead(func(sc stream.Scope[streamBatchElem]) error {
		sc.AllowValueReuse()
		for it := range sc.Iter() {
			if err := it.Decode(); err != nil {
				return err
			}
			got = append(got, it.Target().A)
		}
		return nil
	})
	if err := vjson.Unmarshal([]byte(`{"s":[{"a":1},{"a":2},{"a":3},{"a":4},{"a":5},{"a":6}]}`), &v); err != nil {
		t.Fatalf("%v", err)
	}
	if !slices.Equal(got, []uint8{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("got %v, want the six supplied elements", got)
	}
}
