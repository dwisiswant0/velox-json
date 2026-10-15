package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"
	"weak"

	vjson "github.com/velox-io/json"
)

// TestUnmarshal_MapIntKeyTypeError pins that a key an integer map cannot
// hold is an UnmarshalTypeError naming the key type, as with encoding/json,
// and that the keys around it still land. Each case runs through Unmarshal,
// UnmarshalValue, and a chunked Decoder.
func TestUnmarshal_MapIntKeyTypeError(t *testing.T) {
	type host struct {
		M map[int8]string
		U map[uint16]int
	}
	cases := []struct {
		name, in, value string
		typ             reflect.Type
		cause           error
	}{
		{"not a number", `{"M":{"1":"a","x":"b","-2":"c"}}`, "number x", reflect.TypeFor[int8](), strconv.ErrSyntax},
		{"int overflow", `{"M":{"1":"a","128":"b"}}`, "number 128", reflect.TypeFor[int8](), strconv.ErrRange},
		{"uint negative", `{"U":{"7":1,"-1":2}}`, "number -1", reflect.TypeFor[uint16](), strconv.ErrSyntax},
		{"uint overflow", `{"U":{"65536":1,"9":2}}`, "number 65536", reflect.TypeFor[uint16](), strconv.ErrRange},
	}
	for _, tc := range cases {
		var std host
		stdErr := json.Unmarshal([]byte(tc.in), &std)
		var se *json.UnmarshalTypeError
		if !errors.As(stdErr, &se) || se.Type != tc.typ {
			t.Fatalf("%s: encoding/json gave %v, the case expects a %v type error", tc.name, stdErr, tc.typ)
		}
		decoders := []struct {
			name   string
			decode func(*host) error
		}{
			{"Unmarshal", func(h *host) error { return vjson.Unmarshal([]byte(tc.in), h) }},
			{"UnmarshalValue", func(h *host) error {
				v, err := vjson.Parse([]byte(tc.in))
				if err != nil {
					return err
				}
				return vjson.UnmarshalValue(v, h)
			}},
			{"Decoder", func(h *host) error { return vjson.NewDecoder(&chunkReader{data: []byte(tc.in), size: 3}).Decode(h) }},
		}
		for _, d := range decoders {
			var got host
			err := d.decode(&got)
			var ute *json.UnmarshalTypeError
			if !errors.As(err, &ute) {
				t.Errorf("%s/%s: got %T (%v), want *json.UnmarshalTypeError", tc.name, d.name, err, err)
				continue
			}
			if ute.Type != tc.typ || ute.Value != tc.value {
				t.Errorf("%s/%s: Type=%v Value=%q, want Type=%v Value=%q", tc.name, d.name, ute.Type, ute.Value, tc.typ, tc.value)
			}
			if !errors.Is(err, tc.cause) {
				t.Errorf("%s/%s: %v does not wrap %v", tc.name, d.name, err, tc.cause)
			}
			if !reflect.DeepEqual(got, std) {
				t.Errorf("%s/%s: decoded %+v, encoding/json %+v", tc.name, d.name, got, std)
			}
		}
	}
}

// mkText is a string-kind key whose UnmarshalText outranks its kind.
type mkText string

func (k *mkText) UnmarshalText(b []byte) error {
	*k = mkText("t:" + string(b))
	return nil
}

// mkIntText is an integer-kind key whose UnmarshalText outranks the base-10
// parse.
type mkIntText int

func (k *mkIntText) UnmarshalText(b []byte) error {
	*k = mkIntText(len(b))
	return nil
}

// mkAcc appends to what its receiver holds, so it decodes a key to its text
// only from a zero receiver.
type mkAcc struct{ S string }

func (k *mkAcc) UnmarshalText(b []byte) error {
	k.S += "|" + string(b)
	return nil
}

// mkValRecv decodes through a value receiver, so every key stays zero.
type mkValRecv struct{ S string }

func (mkValRecv) UnmarshalText([]byte) error { return nil }

// mkJSONStr is a string-kind key with only UnmarshalJSON, which a key does
// not consult.
type mkJSONStr string

func (k *mkJSONStr) UnmarshalJSON([]byte) error {
	*k = "json"
	return nil
}

type mkPlain struct{ A int }

type mkHost struct {
	A int                  `json:"a"`
	M map[netip.Addr][]int `json:"m"`
	P map[mkPlain]int      `json:"p"`
	B int                  `json:"b"`
}

// addrObject builds an object of n distinct address keys, enough to fill
// more than one staging region.
func addrObject(n int, extra ...string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"10.0.%d.%d":%d`, i/256, i%256, i)
	}
	for _, e := range extra {
		b.WriteString(",")
		b.WriteString(e)
	}
	b.WriteByte('}')
	return b.String()
}

// TestUnmarshal_MapTextKeys pins map key conversion against encoding/json:
// a TextUnmarshaler key decodes through UnmarshalText from a zero key
// whatever its kind, a string kind without one takes the key as is, and a
// key of any other kind is a type error.
func TestUnmarshal_MapTextKeys(t *testing.T) {
	cases := []corpusCase{
		{"addr", `{"192.168.0.1":"a","::1":"b","10.0.0.1":"c"}`, newOf[map[netip.Addr]string](), true},
		{"addr escaped key", `{"1\u002e2.3.4":1}`, newOf[map[netip.Addr]int](), true},
		{"addr empty key", `{"":1}`, newOf[map[netip.Addr]int](), true},
		{"addr duplicate key", `{"1.2.3.4":1,"1.2.3.4":2}`, newOf[map[netip.Addr]int](), true},
		{"addr many keys", addrObject(70), newOf[map[netip.Addr]int](), true},
		{"addr nested", `{"1.2.3.4":{"x":1,"yy":2},"::":{}}`, newOf[map[netip.Addr]map[mkIntText]int](), true},
		{"addr in struct", `{"a":1,"m":{"1.2.3.4":[1,2],"::1":[]},"b":2}`, newOf[mkHost](), true},
		{"addr large value", `{"1.2.3.4":[1,2,3]}`, newOf[map[netip.Addr][20]int64](), true},
		{"addr deferred value", `{"1.2.3.4":{"k":[1]},"::1":"s"}`, newOf[map[netip.Addr]json.RawMessage](), false},
		{"string kind", `{"a":1,"b\u00e9":2,"":3}`, newOf[map[mkText]int](), true},
		{"int kind", `{"abc":1,"7":2}`, newOf[map[mkIntText]int](), true},
		{"zero receiver", `{"a":1,"b":2,"a":3}`, newOf[map[mkAcc]int](), true},
		{"value receiver", `{"a":1,"b":2}`, newOf[map[mkValRecv]int](), true},
		{"json-only string kind", `{"a":1,"b":2}`, newOf[map[mkJSONStr]int](), true},
		{"number", `{"1.5":1,"-2":2}`, newOf[map[json.Number]int](), true},
		{"struct key", `{"a":1}`, newOf[map[mkPlain]int](), true},
		{"bool key", `{"true":1}`, newOf[map[bool]int](), true},
		{"array key", `{"a":1}`, newOf[map[[2]int]int](), true},
		{"struct key in host", `{"a":1,"p":{"x":1},"b":2}`, newOf[mkHost](), true},
	}
	for _, c := range cases {
		assertParity(t, c)
	}
}

// decodeEntries decodes in into a fresh mk() through Unmarshal, chunked
// Decoders, and, when uv, Parse followed by UnmarshalValue, handing each
// result to check.
func decodeEntries(in string, mk func() any, uv bool, check func(entry string, v any, err error)) {
	data := []byte(in)
	v := mk()
	check("Unmarshal", v, vjson.Unmarshal(data, v))
	for _, chunk := range []int{1, len(data) + 1} {
		v = mk()
		check(fmt.Sprintf("Decoder(chunk=%d)", chunk), v, vjson.NewDecoder(&chunkReader{data: data, size: chunk}).Decode(v))
	}
	if uv {
		v = mk()
		pv, err := vjson.Parse(data)
		if err == nil {
			err = vjson.UnmarshalValue(pv, v)
		}
		check("UnmarshalValue", v, err)
	}
}

var (
	errKeyHook = errors.New("key hook failure")
	errValHook = errors.New("value hook failure")
)

// mkFailKey fails the key "bad".
type mkFailKey struct{ S string }

func (k *mkFailKey) UnmarshalText(b []byte) error {
	if string(b) == "bad" {
		return errKeyHook
	}
	k.S = string(b)
	return nil
}

type mkFailVal struct{}

func (*mkFailVal) UnmarshalText([]byte) error { return errValHook }

type mkKeyFailHost struct {
	M map[mkFailKey]int `json:"m"`
	N int               `json:"n"`
}

type mkFailHost struct {
	M map[mkFailKey]int `json:"m"`
	V mkFailVal         `json:"v"`
	N int               `json:"n"`
}

// TestUnmarshal_MapTextKeyError pins a failing key hook: the caller gets
// the error the hook returned, and the walk goes on, so every other key and
// field lands. Failures rank walk errors first, then value hooks, then keys,
// whatever their document order; encoding/json reports the first in
// document order instead.
func TestUnmarshal_MapTextKeyError(t *testing.T) {
	many := strings.ReplaceAll(addrObject(40, `"bad":40`, `"10.1.0.0":41`), `"10.0.`, `"k.`)
	for _, in := range []string{`{"m":{"ok":1,"bad":2,"ok2":3},"n":1}`, `{"m":` + many + `,"n":1}`} {
		decodeEntries(in, newOf[mkKeyFailHost](), true, func(entry string, v any, err error) {
			if err != errKeyHook {
				t.Errorf("%.30s/%s: got %v, want the key hook's error", in, entry, err)
				return
			}
			h := v.(*mkKeyFailHost)
			var std map[string]int
			if err := json.Unmarshal([]byte(in), &struct {
				M *map[string]int `json:"m"`
			}{&std}); err != nil {
				t.Fatal(err)
			}
			delete(std, "bad")
			if len(h.M) != len(std) || h.N != 1 {
				t.Errorf("%.30s/%s: kept %d keys and n=%d, want %d keys and n=1", in, entry, len(h.M), h.N, len(std))
			}
			for k, n := range std {
				if got, ok := h.M[mkFailKey{k}]; !ok || got != n {
					t.Errorf("%.30s/%s: key %q holds %d (present %v), want %d", in, entry, k, got, ok, n)
				}
			}
		})
	}
	for _, c := range []struct {
		in   string
		want func(error) bool
	}{
		{`{"m":{"bad":1},"v":"x"}`, func(err error) bool { return err == errValHook }},
		{`{"v":"x","m":{"bad":1}}`, func(err error) bool { return err == errValHook }},
		{`{"m":{"bad":1},"n":"x"}`, func(err error) bool {
			var ute *json.UnmarshalTypeError
			return errors.As(err, &ute) && ute.Type == reflect.TypeFor[int]()
		}},
	} {
		decodeEntries(c.in, newOf[mkFailHost](), false, func(entry string, _ any, err error) {
			if !c.want(err) {
				t.Errorf("%s/%s: got %v", c.in, entry, err)
			}
		})
	}
}

// mkHeld's hook leaves a fresh payload reachable only through the key it
// decodes into, then collects: the payload survives only if the GC scans
// that key's storage.
type mkHeld struct{ P *mkPayload }

type mkPayload struct{ S string }

var mkHeldLost int

func (k *mkHeld) UnmarshalText(b []byte) error {
	k.P = &mkPayload{S: string(b)}
	w := weak.Make(k.P)
	runtime.GC()
	if w.Value() == nil {
		mkHeldLost++
	}
	return nil
}

// TestUnmarshal_MapTextKeyGCVisible pins that a key hook decodes into
// storage the GC scans, so what it writes there outlives a collection before
// the map takes the key.
func TestUnmarshal_MapTextKeyGCVisible(t *testing.T) {
	decodeEntries(`{"a":1,"b":2}`, newOf[map[mkHeld]int](), true, func(entry string, v any, err error) {
		mkHeldLost = 0
		if err != nil {
			t.Fatalf("%s: %v", entry, err)
		}
		got := map[string]int{}
		for k, n := range *v.(*map[mkHeld]int) {
			got[k.P.S] = n
		}
		if !reflect.DeepEqual(got, map[string]int{"a": 1, "b": 2}) {
			t.Errorf("%s: decoded %v", entry, got)
		}
		if mkHeldLost != 0 {
			t.Errorf("%s: %d hook payloads collected while their key awaited the map", entry, mkHeldLost)
		}
	})
}

// mkBoth implements both hooks; a key decodes through UnmarshalText, as with
// Go 1.27's encoding/json.
type mkBoth struct{ S string }

func (k *mkBoth) UnmarshalJSON(b []byte) error {
	k.S = "json:" + string(b)
	return nil
}

func (k *mkBoth) UnmarshalText(b []byte) error {
	k.S = "text:" + string(b)
	return nil
}

func TestUnmarshal_MapKeyBothHooks(t *testing.T) {
	decodeEntries(`{"a":1}`, newOf[map[mkBoth]int](), true, func(entry string, v any, err error) {
		if want := (map[mkBoth]int{{"text:a"}: 1}); err != nil || !reflect.DeepEqual(*v.(*map[mkBoth]int), want) {
			t.Errorf("%s: decoded %v (%v), want %v", entry, *v.(*map[mkBoth]int), err, want)
		}
	})
}

// TestUnmarshal_MapKeyUnconvertible pins that a key of a kind no key
// converts to is an UnmarshalTypeError naming the key type, and that an
// empty object still decodes into an empty map.
func TestUnmarshal_MapKeyUnconvertible(t *testing.T) {
	for _, c := range []struct {
		mk  func() any
		typ reflect.Type
	}{
		{newOf[map[float64]int](), reflect.TypeFor[float64]()},
		{newOf[map[*mkText]int](), reflect.TypeFor[*mkText]()},
		{newOf[map[any]int](), reflect.TypeFor[any]()},
		{newOf[map[mkPlain]int](), reflect.TypeFor[mkPlain]()},
	} {
		decodeEntries(`{"1":1}`, c.mk, true, func(entry string, _ any, err error) {
			var ute *json.UnmarshalTypeError
			if !errors.As(err, &ute) || ute.Type != c.typ || ute.Value != "string" {
				t.Errorf("%v/%s: got %v, want a type error naming the key", c.typ, entry, err)
			}
		})
		decodeEntries(`{}`, c.mk, true, func(entry string, v any, err error) {
			if m := reflect.ValueOf(v).Elem(); err != nil || m.IsNil() || m.Len() != 0 {
				t.Errorf("%v/%s: decoded %v (%v), want an empty map", c.typ, entry, m, err)
			}
		})
	}
}

// TestUnmarshal_MapMergeIntoExisting pins the merge semantics of every
// statically-typed map destination: a map already live at the slot keeps its
// absent keys while present keys overwrite, exactly as encoding/json does.
// Destinations the binder stages in scratch (any efaces, map values inside a
// map) replace, matching encoding/json's fresh decode for those shapes. Every
// case runs through Unmarshal, the chunked Decoder, and UnmarshalValue.
func TestUnmarshal_MapMergeIntoExisting(t *testing.T) {
	type mmInner struct {
		M map[string]int `json:"m"`
	}
	type mmHost struct {
		Inner mmInner `json:"inner"`
	}
	type mmPtrInner struct {
		M map[string]int `json:"m"`
	}
	type mmPtrHost struct {
		Inner *mmPtrInner `json:"inner"`
	}
	type mmNested struct {
		M map[string]map[string]int `json:"m"`
	}
	type mmAnyValue struct {
		M map[string]any `json:"m"`
	}
	type mmAnyField struct {
		V any `json:"v"`
	}

	// uv selects the UnmarshalValue entry. The tape engine binds every slice
	// into a fresh backing by design, so slice-shaped roots cannot merge there
	// and run through the contiguous and streaming entries only; struct fields,
	// fixed arrays, and map roots merge through all three.
	cases := []struct {
		name string
		in   string
		uv   bool
		mk   func() any
	}{
		{"root map", `{"new":2,"keep":3}`, true, func() any {
			m := map[string]int{"keep": 1}
			return &m
		}},
		{"struct field", `{"inner":{"m":{"y":2}}}`, true, func() any {
			return &mmHost{Inner: mmInner{M: map[string]int{"x": 1}}}
		}},
		{"pointer-resolved field", `{"inner":{"m":{"y":2}}}`, true, func() any {
			return &mmPtrHost{Inner: &mmPtrInner{M: map[string]int{"x": 1}}}
		}},
		{"slice of maps", `[{"y":2}]`, false, func() any {
			s := []map[string]int{{"x": 1}}
			return &s
		}},
		{"slice of map pointers", `[{"y":2}]`, false, func() any {
			m := map[string]int{"x": 1}
			s := []*map[string]int{&m}
			return &s
		}},
		{"nested map value replaces", `{"m":{"a":{"y":2},"fresh":{"n":1}}}`, true, func() any {
			return &mmNested{M: map[string]map[string]int{"a": {"x": 1}, "keep": {"k": 9}}}
		}},
		{"any map value replaces", `{"m":{"a":{"y":2}}}`, true, func() any {
			return &mmAnyValue{M: map[string]any{"a": map[string]any{"x": 1}}}
		}},
		{"any field replaces", `{"v":{"y":2}}`, true, func() any {
			return &mmAnyField{V: map[string]any{"x": 1}}
		}},
		{"duplicate keys last wins", `{"k":1,"k":2}`, true, func() any {
			m := map[string]int{"old": 0}
			return &m
		}},
		{"empty object keeps all", `{}`, true, func() any {
			m := map[string]int{"x": 1}
			return &m
		}},
		{"array of maps", `[{"y":2}]`, true, func() any {
			a := [1]map[string]int{{"x": 1}}
			return &a
		}},
	}

	for _, tc := range cases {
		stdV := tc.mk()
		if err := json.Unmarshal([]byte(tc.in), stdV); err != nil {
			t.Fatalf("%s: encoding/json rejected the case: %v", tc.name, err)
		}
		decodeEntries(tc.in, tc.mk, tc.uv, func(entry string, v any, err error) {
			if err != nil {
				t.Errorf("%s/%s: %v", tc.name, entry, err)
				return
			}
			if !reflect.DeepEqual(v, stdV) {
				t.Errorf("%s/%s: got %#v, want encoding/json's %#v", tc.name, entry, v, stdV)
			}
		})
	}
}

// TestUnmarshal_MapMergeAcrossParses pins the sequential-reuse shape of the
// merge semantics: one destination decoded by several documents accumulates
// keys, a null map resets the slot to nil so the next document starts fresh,
// and a struct field's map follows the same rules across parses.
func TestUnmarshal_MapMergeAcrossParses(t *testing.T) {
	m := map[string]int{"x": 1}
	if err := vjson.Unmarshal([]byte(`{"y":2}`), &m); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := vjson.Unmarshal([]byte(`{"z":3,"x":4}`), &m); err != nil {
		t.Fatalf("second: %v", err)
	}
	if !reflect.DeepEqual(m, map[string]int{"x": 4, "y": 2, "z": 3}) {
		t.Errorf("accumulated map = %v, want x:4 y:2 z:3", m)
	}
	if err := vjson.Unmarshal([]byte(`null`), &m); err != nil {
		t.Fatalf("null: %v", err)
	}
	if m != nil {
		t.Errorf("null map = %v, want nil", m)
	}
	if err := vjson.Unmarshal([]byte(`{"fresh":1}`), &m); err != nil {
		t.Fatalf("after null: %v", err)
	}
	if !reflect.DeepEqual(m, map[string]int{"fresh": 1}) {
		t.Errorf("map after null = %v, want fresh:1 only", m)
	}

	type mmReuseHost struct {
		M map[string]int `json:"m"`
		N string         `json:"n"`
	}
	h := mmReuseHost{M: map[string]int{"keep": 1}}
	for _, doc := range []string{`{"m":{"a":1},"n":"x"}`, `{"m":{"b":2},"n":"y"}`} {
		if err := vjson.Unmarshal([]byte(doc), &h); err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
	}
	if !reflect.DeepEqual(h.M, map[string]int{"keep": 1, "a": 1, "b": 2}) {
		t.Errorf("field map across parses = %v, want keep:1 a:1 b:2", h.M)
	}
	if h.N != "y" {
		t.Errorf("N = %q, want y", h.N)
	}
}

// gcEachReader runs a GC and then check before serving each chunk, so a
// chunked Decoder collects garbage between every pair of native windows.
type gcEachReader struct {
	chunkReader
	check func()
}

func (r *gcEachReader) Read(p []byte) (int, error) {
	runtime.GC()
	r.check()
	return r.chunkReader.Read(p)
}

// TestUnmarshal_MapMergeRootedUntilDrain pins that a user map the binder
// merges into stays alive while its staged entries wait for the drain, even
// after a later value drops the destination's edge to it: the map slot itself
// (a duplicate key's null), a pointer above it, or the slice backing holding
// it. The trailing padding forces windows, and so collections, between the
// drop and the document end, where the drain writes into the map.
func TestUnmarshal_MapMergeRootedUntilDrain(t *testing.T) {
	type field struct {
		M map[string]int `json:"m"`
		P string         `json:"p"`
	}
	type inner struct {
		M map[string]int `json:"m"`
	}
	type ptrHost struct {
		Inner *inner `json:"inner"`
		P     string `json:"p"`
	}
	type sliceHost struct {
		L []map[string]int `json:"l"`
		P string           `json:"p"`
	}
	pad := `,"p":"` + strings.Repeat("x", 4096) + `"}`

	cases := []struct {
		name string
		in   string
		mk   func() (any, map[string]int)
	}{
		{"map slot nulled", `{"m":{"a":1},"m":null` + pad, func() (any, map[string]int) {
			h := &field{M: map[string]int{"keep": 1}}
			return h, h.M
		}},
		{"parent pointer nulled", `{"inner":{"m":{"a":1}},"inner":null` + pad, func() (any, map[string]int) {
			h := &ptrHost{Inner: &inner{M: map[string]int{"keep": 1}}}
			return h, h.Inner.M
		}},
		{"slice backing dropped", `{"l":[{"a":1}],"l":null` + pad, func() (any, map[string]int) {
			h := &sliceHost{L: []map[string]int{{"keep": 1}}}
			return h, h.L[0]
		}},
	}
	for _, tc := range cases {
		dst, live := tc.mk()
		// Weakly track the runtime map object the destination starts with.
		w := weak.Make(*(**byte)(unsafe.Pointer(&live)))
		live = nil
		collected := false
		r := &gcEachReader{chunkReader: chunkReader{data: []byte(tc.in), size: 64}, check: func() {
			collected = collected || w.Value() == nil
		}}
		if err := vjson.NewDecoder(r).Decode(dst); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		runtime.KeepAlive(dst)
		if collected {
			t.Errorf("%s: merged map collected while its entries were staged", tc.name)
		}
	}
}

// TestUnmarshal_MapValueArrayFreshAcrossParses pins that a fixed array map
// value binds from zero on the tape engine. Map entries stage in a pooled
// buffer that outlives the parse, so an array element left unzeroed would
// expose the previous parse's bytes: a stale scalar, or a stale map pointer the
// element map would merge into, aliasing the earlier result.
func TestUnmarshal_MapValueArrayFreshAcrossParses(t *testing.T) {
	type ab struct{ A, B int }
	type withMap struct {
		M map[string]int `json:"m"`
	}
	uv := func(in string, v any) {
		t.Helper()
		pv, err := vjson.Parse([]byte(in))
		if err == nil {
			err = vjson.UnmarshalValue(pv, v)
		}
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
	}

	var s1, s2 map[string][1]ab
	uv(`{"a":[{"A":1,"B":2}]}`, &s1)
	uv(`{"b":[{"A":5}]}`, &s2)
	if want := (map[string][1]ab{"b": {{A: 5}}}); !reflect.DeepEqual(s2, want) {
		t.Errorf("scalar element = %v, want %v", s2, want)
	}

	var m1, m2 map[string][1]map[string]int
	uv(`{"a":[{"x":1}]}`, &m1)
	uv(`{"b":[{"y":2}]}`, &m2)
	if !reflect.DeepEqual(m1, map[string][1]map[string]int{"a": {{"x": 1}}}) ||
		!reflect.DeepEqual(m2, map[string][1]map[string]int{"b": {{"y": 2}}}) {
		t.Errorf("map element aliased across parses: first %v, second %v", m1, m2)
	}

	var f1, f2 map[string][1]withMap
	uv(`{"a":[{"m":{"x":1}}]}`, &f1)
	uv(`{"b":[{"m":{"y":2}}]}`, &f2)
	if !reflect.DeepEqual(f1, map[string][1]withMap{"a": {{M: map[string]int{"x": 1}}}}) ||
		!reflect.DeepEqual(f2, map[string][1]withMap{"b": {{M: map[string]int{"y": 2}}}}) {
		t.Errorf("struct element map aliased across parses: first %v, second %v", f1, f2)
	}
}
