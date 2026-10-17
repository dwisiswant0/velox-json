package tests

import (
	"testing"

	vjson "github.com/velox-io/json"
)

// Decoding an empty JSON array resets the destination slice's length to zero
// and keeps its backing and capacity, the slice[:0] semantics a caller
// round-robining a preallocated buffer over venue messages depends on. A nil
// destination still yields a non-nil empty slice.

type capLevel struct {
	Price string `json:"price"`
	Qty   string `json:"qty"`
	ID    int64  `json:"id"`
}

type capBook struct {
	Levels []capLevel `json:"levels"`
}

const (
	capEmptyDoc = `{"levels":[]}`
	capFourDoc  = `{"levels":[{"price":"1","qty":"1","id":1},{"price":"2","qty":"1","id":2},{"price":"3","qty":"1","id":3},{"price":"4","qty":"1","id":4}]}`
)

// An empty array decodes like slice[:0]: the destination keeps its capacity
// and backing array.
func TestUnmarshal_EmptyArrayKeepsCapacity(t *testing.T) {
	b := capBook{Levels: make([]capLevel, 0, 64)}
	backing := &b.Levels[:1][0]
	if err := vjson.Unmarshal([]byte(capEmptyDoc), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Levels) != 0 {
		t.Fatalf("len = %d, want 0", len(b.Levels))
	}
	if cap(b.Levels) != 64 {
		t.Fatalf("cap after [] = %d, want 64", cap(b.Levels))
	}
	if &b.Levels[:1][0] != backing {
		t.Fatal("backing array replaced by []")
	}
}

// A nil destination yields a non-nil empty slice, as before.
func TestUnmarshal_EmptyArrayNilDest(t *testing.T) {
	var b capBook
	if err := vjson.Unmarshal([]byte(capEmptyDoc), &b); err != nil {
		t.Fatal(err)
	}
	if b.Levels == nil || len(b.Levels) != 0 {
		t.Fatalf("levels = %#v, want non-nil empty", b.Levels)
	}
}

// A backing a non-empty array published (cap == len at close) survives a
// following empty array in the same destination.
func TestUnmarshal_EmptyArrayAfterNonEmpty(t *testing.T) {
	b := capBook{Levels: make([]capLevel, 0, 8)}
	if err := vjson.Unmarshal([]byte(capFourDoc), &b); err != nil {
		t.Fatal(err)
	}
	capAfterFour := cap(b.Levels)
	if capAfterFour == 0 {
		t.Fatal("cap after four elements = 0")
	}
	backing := &b.Levels[0]
	if err := vjson.Unmarshal([]byte(capEmptyDoc), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Levels) != 0 || cap(b.Levels) != capAfterFour {
		t.Fatalf("after four->[]: len/cap = %d/%d, want 0/%d", len(b.Levels), cap(b.Levels), capAfterFour)
	}
	if &b.Levels[:1][0] != backing {
		t.Fatal("backing array replaced by []")
	}
}

// The alternating empty/non-empty round robin decodes into one stable
// preallocated buffer without growing a new backing per empty array.
func TestUnmarshal_EmptyArrayRoundRobin(t *testing.T) {
	buf := make([]capLevel, 0, 64)
	backing := &buf[:1][0]
	var b capBook
	for i, in := range []string{capEmptyDoc, capFourDoc, capEmptyDoc, capFourDoc} {
		b.Levels = buf[:0]
		if err := vjson.Unmarshal([]byte(in), &b); err != nil {
			t.Fatal(err)
		}
		if len(b.Levels) == 4 && &b.Levels[0] != backing {
			t.Fatalf("decode %d: four levels decoded outside the buffer", i)
		}
		if cap(b.Levels) == 0 && len(b.Levels) == 0 {
			t.Fatalf("decode %d: capacity lost to an empty array", i)
		}
	}
}

// A parse that fails inside an array returns the unwritten tail of its
// borrowed backing to the allocator, so the failed slice's capacity must stop
// short of that tail: a later parse borrows it, and an append through the
// failed slice would otherwise overwrite the later result.
func TestUnmarshal_FailedArrayCapacityExcludesReturnedTail(t *testing.T) {
	var failed capBook
	if err := vjson.Unmarshal([]byte(`{"levels":[{"price":"1"},{"price":"2"} x]}`), &failed); err == nil {
		t.Fatal("malformed document decoded without error")
	}
	full := failed.Levels[:cap(failed.Levels)]
	for i := range 4 {
		var b capBook
		if err := vjson.Unmarshal([]byte(`{"levels":[{"price":"8"}]}`), &b); err != nil {
			t.Fatal(err)
		}
		for j := range full {
			if &full[j] == &b.Levels[0] {
				t.Fatalf("decode %d: element %d of the failed slice's capacity backs a later result", i, j)
			}
		}
	}
}
