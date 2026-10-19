package bind

import (
	"slices"
	"testing"

	"github.com/velox-io/json/stream"
)

// The close yield carries the final partial batch, and its count must reach
// the handler. An element that binds without pushing a frame (a scalar or an
// empty object) leaves the header at the previous count, so the last document
// element once never appeared in any batch.
func TestStreamFinalBatchCount(t *testing.T) {
	t.Run("scalar", func(t *testing.T) {
		var s stream.Stream[uint8]
		var got []uint8
		s.OnRead(func(sc stream.Scope[uint8]) error {
			for it := range sc.Iter() {
				if err := it.Decode(); err != nil {
					return err
				}
				got = append(got, *it.Target())
			}
			return nil
		})
		if err := Unmarshal([]byte(`[1,2,3,4,5,6,7,8,9]`), &s); err != nil {
			t.Fatalf("%v", err)
		}
		want := []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9}
		if !slices.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("omitted-field-tail", func(t *testing.T) {
		type elem struct {
			A uint8 `json:"a"`
		}
		var host struct {
			S stream.Stream[elem] `json:"s"`
		}
		var got []elem
		host.S.OnRead(func(sc stream.Scope[elem]) error {
			for it := range sc.Iter() {
				if err := it.Decode(); err != nil {
					return err
				}
				got = append(got, *it.Target())
			}
			return nil
		})
		doc := `{"s":[{"a":1},{"a":2},{"a":3},{"a":4},{"a":5},{}]}`
		if err := Unmarshal([]byte(doc), &host); err != nil {
			t.Fatalf("%v", err)
		}
		want := []elem{{1}, {2}, {3}, {4}, {5}, {}}
		if !slices.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}
