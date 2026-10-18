package tests

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	vjson "github.com/velox-io/json"
)

// The rules encoding/json applies when choosing the fields of a struct with
// embedded structs, each pinned against the stdlib oracle: both engines must
// agree on whether decoding fails, and when both succeed the decoded values
// must be deeply equal, through Unmarshal and through the streaming Decoder.

// decodeParity decodes in with both engines into fresh targets and reports every
// divergence. It returns the stdlib result and error so callers can also pin
// the behavior each rule describes.
func decodeParity(t *testing.T, name, in string, newTarget func() any) (std any, stdErr error) {
	t.Helper()

	std = newTarget()
	stdErr = json.Unmarshal([]byte(in), std)
	got := newTarget()
	vjErr := vjson.Unmarshal([]byte(in), got)
	if (stdErr != nil) != (vjErr != nil) {
		t.Errorf("%s: error mismatch: stdlib=%v vjson=%v", name, stdErr, vjErr)
		return
	}
	if stdErr == nil && !reflect.DeepEqual(std, got) {
		t.Errorf("%s: value mismatch\n  stdlib: %#+v\n  vjson:  %#+v", name, std, got)
	}

	gotStream := newTarget()
	streamErr := vjson.NewDecoder(strings.NewReader(in)).Decode(gotStream)
	if (stdErr != nil) != (streamErr != nil) {
		t.Errorf("%s: stream decoder error mismatch: stdlib=%v vjson=%v", name, stdErr, streamErr)
		return
	}
	if stdErr == nil && !reflect.DeepEqual(std, gotStream) {
		t.Errorf("%s: stream decoder value mismatch\n  stdlib: %#+v\n  vjson:  %#+v", name, std, gotStream)
	}
	return std, stdErr
}

// -----------------------------------------------------------------------------
// Recursive promotion: a struct holds, as a plain field, a type which embeds
// it, so the field list of the inner struct is needed while the outer one is
// still being built. Every promoted field must survive the cycle.
// -----------------------------------------------------------------------------

type ancEA struct {
	X int
	L []ancEB
}
type ancEB struct {
	ancEA
	Y int
}

type ancMEA struct {
	X int
	M map[string]ancMEB
}
type ancMEB struct {
	ancMEA
	Y int
}

type AncPEA struct {
	X int
	P []*AncPEB
}
type AncPEB struct {
	*AncPEA
	Y int
}

// ancLA reaches the ancestor two embed levels down from the element type.
type ancLA struct {
	X int
	L []ancLB
}
type ancLB struct {
	ancLC
	Y int
}
type ancLC struct {
	ancLA
	Z int
}

// AncMA and AncMB embed each other through pointers.
type AncMA struct {
	*AncMB
	A int
}
type AncMB struct {
	*AncMA
	B int
}

// AncSE embeds itself through a pointer.
type AncSE struct {
	*AncSE
	Y int
}

func TestEmbeddedAncestorPromotion(t *testing.T) {
	std, err := decodeParity(t, "slice holds the embedder", `{"X":1,"L":[{"X":2,"Y":3}]}`,
		func() any { return &ancEA{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*ancEA); got.X != 1 || len(got.L) != 1 || got.L[0].X != 2 || got.L[0].Y != 3 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	std, err = decodeParity(t, "map holds the embedder", `{"X":1,"M":{"a":{"X":2,"Y":3}}}`,
		func() any { return &ancMEA{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*ancMEA); got.X != 1 || got.M["a"].X != 2 || got.M["a"].Y != 3 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	std, err = decodeParity(t, "pointer slice holds the embedder", `{"X":1,"P":[{"X":2,"Y":3}]}`,
		func() any { return &AncPEA{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*AncPEA); got.X != 1 || len(got.P) != 1 || got.P[0].X != 2 || got.P[0].Y != 3 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	std, err = decodeParity(t, "ancestor two levels down", `{"X":1,"L":[{"X":2,"Y":3,"Z":4}]}`,
		func() any { return &ancLA{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*ancLA); len(got.L) != 1 || got.L[0].X != 2 || got.L[0].Y != 3 || got.L[0].Z != 4 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	std, err = decodeParity(t, "mutual embedding", `{"A":1,"B":2}`,
		func() any { return &AncMA{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*AncMA); got.A != 1 || got.AncMB == nil || got.B != 2 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	std, err = decodeParity(t, "self embedding", `{"Y":5}`,
		func() any { return &AncSE{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*AncSE); got.Y != 5 || got.AncSE != nil {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}
}

// -----------------------------------------------------------------------------
// Dominance: of the fields of a key, encoding/json takes the least deep one,
// and at equal depth the only one whose key comes from its tag. Depth still
// beats the tag; two tagged or two untagged candidates at one depth cancel.
// -----------------------------------------------------------------------------

type domTA struct {
	V int
}
type domTB struct {
	W int `json:"V"`
}
type domTHost struct {
	domTA
	domTB
}

type domTT1 struct {
	V int `json:"V"`
}

// domTT2's key collides with domTT1's on purpose.
type domTT2 struct {
	W int `json:"V"`
}

type domTTHost struct {
	domTT1
	domTT2 //nolint:govet // deliberate duplicate key: two tagged fields cancel
}

// domMixHost opposes a tagged promoted field at depth 2 against an untagged
// direct field at depth 0: depth wins, the tag only breaks ties at one depth.
type domDeepTag struct {
	V int `json:"V"`
}
type domShallow struct {
	domDeepTag
}
type domMixHost struct {
	domShallow
	V string
}

func TestEmbeddedTaggedFieldDominance(t *testing.T) {
	type taggedSelf struct {
		A int `json:"B"`
		B int
	}

	std, err := decodeParity(t, "tagged field of the struct itself", `{"B":2}`,
		func() any { return &taggedSelf{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*taggedSelf); got.A != 2 || got.B != 0 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	std, err = decodeParity(t, "one tagged embed at equal depth", `{"V":7}`,
		func() any { return &domTHost{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*domTHost); got.W != 7 || got.V != 0 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	// Two tagged fields of one key at equal depth still cancel.
	std, err = decodeParity(t, "two tagged embeds at equal depth", `{"V":7}`,
		func() any { return &domTTHost{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*domTTHost); got.V != 0 || got.W != 0 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	std, err = decodeParity(t, "depth beats tag", `{"V":"s"}`,
		func() any { return &domMixHost{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*domMixHost); got.V != "s" || got.domDeepTag.V != 0 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}
}

// -----------------------------------------------------------------------------
// A promoted field reached through an embedded pointer field that is
// unexported cannot be set: reflect refuses to store the allocation into that
// slot, so encoding/json reports an error where a naive writer would allocate
// it anyway. The field's own name decides: an alias can export it over an
// unexported pointee type, or hide it over an exported one.
// -----------------------------------------------------------------------------

type nilPtrInner struct {
	Q int
}

type nilPtrMid struct {
	*nilPtrInner
}

type nilPtrHost struct {
	nilPtrMid
	R int
}

type nilPtrDirect struct {
	*nilPtrInner
	R int
}

type aliasPointee struct{ Q int }
type ExportedAlias = aliasPointee

type aliasExposedHost struct {
	*ExportedAlias
	R int
}

type exposedPointee struct{ Q int }
type unexportedAlias = exposedPointee

type aliasHiddenHost struct {
	*unexportedAlias
	R int
}

func TestEmbeddedUnexportedPointerNil(t *testing.T) {
	_, stdErr := decodeParity(t, "nil pointer under a value embed", `{"Q":1,"R":2}`,
		func() any { return &nilPtrHost{} })
	if stdErr == nil {
		t.Fatal("stdlib accepted a promoted field across a nil unexported pointer")
	}

	_, stdErr = decodeParity(t, "nil pointer at the top level", `{"Q":1,"R":2}`,
		func() any { return &nilPtrDirect{} })
	if stdErr == nil {
		t.Fatal("stdlib accepted a promoted field across a nil unexported pointer")
	}

	// A set pointer needs no store, so the promoted field decodes normally.
	std, err := decodeParity(t, "set pointer under a value embed", `{"Q":1,"R":2}`,
		func() any {
			return &nilPtrHost{nilPtrMid: nilPtrMid{nilPtrInner: &nilPtrInner{}}}
		})
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*nilPtrHost); got.R != 2 || got.Q != 1 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	// An exported alias names the embedded field, so the slot stays settable
	// even though the pointee type is unexported.
	std, err = decodeParity(t, "exported alias over unexported pointee", `{"Q":1,"R":2}`,
		func() any { return &aliasExposedHost{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*aliasExposedHost); got.Q != 1 || got.R != 2 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	// An unexported alias hides the embedded field, so the slot stays
	// unsettable even though the pointee type is exported.
	_, stdErr = decodeParity(t, "unexported alias over exported pointee", `{"Q":1,"R":2}`,
		func() any { return &aliasHiddenHost{} })
	if stdErr == nil {
		t.Fatal("stdlib accepted a promoted field across a nil unexported alias")
	}
}

// -----------------------------------------------------------------------------
// When the UnmarshalJSON (or UnmarshalText) methods of two embedded structs
// hide each other, the host implements neither interface, so encoding/json
// promotes their fields instead of routing each embed through its method.
// -----------------------------------------------------------------------------

type hideJ1 struct {
	A int
}

func (v *hideJ1) UnmarshalJSON(b []byte) error {
	v.A = 1000
	return nil
}

type hideJ2 struct {
	B int
}

func (v *hideJ2) UnmarshalJSON(b []byte) error {
	v.B = 2000
	return nil
}

type hideJHost struct {
	hideJ1
	hideJ2
}

type hideJSingle struct {
	hideJ1
}

type hideT1 struct {
	A int
}

func (v *hideT1) UnmarshalText(b []byte) error {
	v.A = 1000
	return nil
}

type hideT2 struct {
	B int
}

func (v *hideT2) UnmarshalText(b []byte) error {
	v.B = 2000
	return nil
}

type hideTHost struct {
	hideT1
	hideT2
}

func TestEmbeddedHiddenUnmarshalMethods(t *testing.T) {
	std, err := decodeParity(t, "hidden UnmarshalJSON pair", `{"A":1,"B":2}`,
		func() any { return &hideJHost{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*hideJHost); got.A != 1 || got.B != 2 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	std, err = decodeParity(t, "hidden UnmarshalText pair", `{"A":1,"B":2}`,
		func() any { return &hideTHost{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*hideTHost); got.A != 1 || got.B != 2 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}

	// With a single embed the method is promoted to the host, which then
	// implements json.Unmarshaler and takes the whole value.
	std, err = decodeParity(t, "promoted UnmarshalJSON", `{"A":1}`,
		func() any { return &hideJSingle{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*hideJSingle); got.A != 1000 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}
}

// -----------------------------------------------------------------------------
// The `,string` option of an embedded type which is not a struct applies to
// the promoted member, which carries its type's name as the key.
// -----------------------------------------------------------------------------

type StrEmbInt int

type strEmbHost struct {
	StrEmbInt `json:",string"`
	R         int
}

func TestEmbeddedNonStructStringOption(t *testing.T) {
	std, err := decodeParity(t, "embedded int with ,string", `{"StrEmbInt":"42","R":1}`,
		func() any { return &strEmbHost{} })
	if err != nil {
		t.Fatalf("stdlib: %v", err)
	}
	if got := std.(*strEmbHost); got.StrEmbInt != 42 || got.R != 1 {
		t.Fatalf("stdlib decoded wrong: %+v", got)
	}
}
