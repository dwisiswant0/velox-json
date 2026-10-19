package vjson

import (
	"reflect"
	"unsafe"

	"github.com/velox-io/json/gort"
	"github.com/velox-io/json/rtcache"
)

// writeBarrier mirrors runtime.writeBarrier, trailing fields included, so
// every field offset stays aligned with the runtime declaration. Only
// enabled is read: it is the flag the compiler tests before a store
// barrier, and the runtime flips it with the world stopped, so a running
// goroutine cannot observe marking begun after its load as off.
//
//go:linkname writeBarrier runtime.writeBarrier
var writeBarrier struct {
	enabled bool
	pad     [3]byte
	alignme uint64
}

// shade clears the pointee of p through a typed store. The store carries
// the hybrid barrier's deletion half, which shades what the slot held
// before the clear, and the clear then drops that value. Callers pass
// scratch storage, never data they still read, and T must carry pointers:
// a pointer-free T compiles to a plain clear with no barrier.
//
// The call must stay opaque to its caller: the compiler emits no barrier
// for a destination it can prove is a stack slot, so the slot would be
// cleared without shading anything. nosplit keeps the marking path a leaf.
//
//go:noinline
//go:nosplit
func shade[T any](p *T) {
	var zero T
	*p = zero
}

// ptrWord is the size of a pointer word on this platform.
const ptrWord = unsafe.Sizeof(uintptr(0))

// ptrPlan is the pointer-word layout of a struct or array type. Offsets are
// relative to the value's base and cover every word the GC scans, including
// words in unexported fields.
//
// A plan is immutable once built. Types are immutable too, so concurrent
// builders produce equal plans and any of them can win the cache slot.
type ptrPlan struct {
	offs []uintptr
}

var ptrPlans rtcache.Table[*ptrPlan] // rtype -> pointer-word plan

// ptrPlanFor returns the cached plan for t, building it on first use. A
// miss that evicts a hot plan only costs a rebuild; it cannot mis-shade.
func ptrPlanFor(t reflect.Type) *ptrPlan {
	rtp := uintptr(gort.TypePtr(t))
	if p, ok := ptrPlans.Get(rtp); ok {
		return p
	}
	var offs []uintptr
	collectPtrWords(t, 0, &offs)
	p := &ptrPlan{offs: offs}
	ptrPlans.Set(rtp, p)
	return p
}

// collectPtrWords appends the offset of every pointer word the value at
// base holds. The classification matches the GC scan: a string or slice
// contributes its data word, an interface its data word, a map, channel,
// func, pointer or unsafe.Pointer its single word, and a struct or array
// its members.
func collectPtrWords(t reflect.Type, base uintptr, offs *[]uintptr) {
	switch t.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func,
		reflect.Pointer, reflect.UnsafePointer:
		*offs = append(*offs, base)
	case reflect.Interface:
		*offs = append(*offs, base+ptrWord)
	case reflect.Struct:
		for i, n := 0, t.NumField(); i < n; i++ {
			f := t.Field(i)
			if hasPointers(f.Type) {
				collectPtrWords(f.Type, base+f.Offset, offs)
			}
		}
	case reflect.Array:
		e := t.Elem()
		if !hasPointers(e) {
			return
		}
		for i, n, size := 0, t.Len(), int(e.Size()); i < n; i++ {
			collectPtrWords(e, base+uintptr(i*size), offs)
		}
	}
}

// hasPointers reports whether values of type t hold any pointer word.
func hasPointers(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func,
		reflect.Interface, reflect.Pointer, reflect.UnsafePointer:
		return true
	case reflect.Struct:
		for i, n := 0, t.NumField(); i < n; i++ {
			if hasPointers(t.Field(i).Type) {
				return true
			}
		}
	case reflect.Array:
		return hasPointers(t.Elem())
	}
	return false
}

// Pin shades what p refers to for the garbage collection cycle already
// in flight and returns p.
//
// Unmarshal overwrites a destination without Go's deletion barrier, and
// the assignment binding a field's old value to a local carries no
// barrier either. In the rare case a local outlives its field across an
// Unmarshal, the cycle in flight can free the old value while the local
// still refers to it. Pin is that shading managed by the caller:
//
//	old := vjson.Pin(v.Greeting)  // a shaded copy of the old value
//	vjson.Unmarshal(data, v)      // the overwrite drops the field without a barrier
//	log.Println(old)              // safe: the old backing survived the cycle
//
// Pin shades every pointer word p holds: the backing of a string or
// slice, the header of a map, the data word of an interface, every
// pointer word of a struct or array, and every link of a pointer chain
// (a null clears the outermost slot, a value fills the leaf).
//
// Outside a marking cycle it returns p untouched: the stack already holds
// p, so a later cycle scans it as a root.
func Pin[T any](p T) T {
	if !writeBarrier.enabled {
		return p
	}
	return pin(p)
}

// pin shades p's preimages and returns p. It runs only while a marking
// cycle has the write barrier on.
func pin[T any](p T) T {
	t := reflect.TypeFor[T]()
	switch t.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.UnsafePointer:
		scratch := *(*unsafe.Pointer)(unsafe.Pointer(&p))
		shade(&scratch)
	case reflect.Interface:
		scratch := *(*any)(unsafe.Pointer(&p))
		shade(&scratch)
	case reflect.Pointer:
		v := unsafe.Pointer(&p)
		for t.Kind() == reflect.Pointer {
			word := *(*unsafe.Pointer)(v)
			if word == nil {
				break
			}
			scratch := word
			shade(&scratch)
			v = word
			t = t.Elem()
		}
	case reflect.Struct, reflect.Array:
		base := unsafe.Pointer(&p)
		for _, off := range ptrPlanFor(t).offs {
			word := *(*unsafe.Pointer)(unsafe.Add(base, off))
			if word != nil {
				shade(&word)
			}
		}
	}
	return p
}
