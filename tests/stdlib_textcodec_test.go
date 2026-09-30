package tests

import (
	"encoding/json"
	"log/slog"
	"net/netip"
	"reflect"
	"testing"

	vjson "github.com/velox-io/json"
)

// Standard library types that implement encoding.TextMarshaler /
// encoding.TextUnmarshaler (not json.Marshaler / json.Unmarshaler):
// netip.Addr, netip.AddrPort, netip.Prefix, slog.Level, slog.LevelVar.
// Every case is compared byte-for-byte or value-for-value against
// encoding/json.

// ---------- netip.Addr marshal ----------

func TestStdlib_NetipAddr_Marshal(t *testing.T) {
	addrs := []netip.Addr{
		netip.MustParseAddr("192.168.1.1"),
		netip.MustParseAddr("0.0.0.0"),
		netip.MustParseAddr("255.255.255.255"),
		netip.MustParseAddr("::1"),
		netip.MustParseAddr("2001:db8::1"),
		netip.MustParseAddr("::ffff:1.2.3.4"),
		netip.MustParseAddr("fe80::1%eth0"),
		{}, // zero Addr marshals as ""
	}
	for _, addr := range addrs {
		vjData, err := vjson.Marshal(addr)
		if err != nil {
			t.Fatalf("marshal %v: %v", addr, err)
		}
		stdData, _ := json.Marshal(addr)
		if string(vjData) != string(stdData) {
			t.Errorf("addr %v:\n  vjson:  %s\n  stdlib: %s", addr, vjData, stdData)
		}
	}
}

func TestStdlib_NetipAddr_MarshalShapes(t *testing.T) {
	type Peer struct {
		Name string      `json:"name"`
		IP   netip.Addr  `json:"ip"`
		GW   *netip.Addr `json:"gw"`
		Alt  *netip.Addr `json:"alt,omitempty"`
	}
	gw := netip.MustParseAddr("10.0.0.1")

	cases := []struct {
		name string
		val  any
	}{
		{"slice", []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("::2")}},
		{"slice_zero_elem", []netip.Addr{netip.MustParseAddr("1.1.1.1"), {}}},
		{"map", map[string]netip.Addr{"a": netip.MustParseAddr("1.1.1.1")}},
		{"struct", Peer{Name: "n1", IP: netip.MustParseAddr("2.2.2.2"), GW: &gw}},
		{"struct_nil_ptr", Peer{Name: "n2", IP: netip.MustParseAddr("2.2.2.2")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vjData, err := vjson.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			stdData, _ := json.Marshal(tc.val)
			if string(vjData) != string(stdData) {
				t.Errorf("vjson:  %s\nstdlib: %s", vjData, stdData)
			}
		})
	}
}

// ---------- netip.AddrPort / netip.Prefix marshal ----------

func TestStdlib_Netip_AddrPortPrefix_Marshal(t *testing.T) {
	cases := []any{
		netip.MustParseAddrPort("192.168.1.1:8080"),
		netip.MustParseAddrPort("[2001:db8::1]:443"),
		netip.MustParseAddrPort("[fe80::1%eth0]:22"),
		netip.AddrPort{}, // zero
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("2001:db8::/64"),
		netip.Prefix{}, // zero
		[]netip.AddrPort{netip.MustParseAddrPort("1.2.3.4:53")},
		[]netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	}
	for _, val := range cases {
		vjData, err := vjson.Marshal(val)
		if err != nil {
			t.Fatalf("marshal %v: %v", val, err)
		}
		stdData, _ := json.Marshal(val)
		if string(vjData) != string(stdData) {
			t.Errorf("%v:\n  vjson:  %s\n  stdlib: %s", val, vjData, stdData)
		}
	}
}

// ---------- netip unmarshal ----------

func TestStdlib_Netip_AddrPortPrefix_Unmarshal(t *testing.T) {
	type route struct {
		Src   netip.Addr     `json:"src"`
		Dst   *netip.Addr    `json:"dst"`
		Binds []netip.Addr   `json:"binds"`
		AddrP netip.AddrPort `json:"addr_p"`
		Net   netip.Prefix   `json:"net"`
	}
	input := []byte(`{
		"src": "fe80::1%en0",
		"dst": "8.8.8.8",
		"binds": ["1.1.1.1", "::ffff:9.9.9.9"],
		"addr_p": "[2001:db8::2]:80",
		"net": "10.1.0.0/16"
	}`)
	var got, want route
	if err := vjson.Unmarshal(input, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(input, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mismatch:\n  got:  %+v\n  want: %+v", got, want)
	}
}

func TestStdlib_Netip_UnmarshalNullAndTopLevel(t *testing.T) {
	type node struct {
		IP netip.Addr   `json:"ip"`
		AP netip.Addr   `json:"ap"`
		P  netip.Prefix `json:"p"`
	}
	input := []byte(`{"ip":null,"ap":null,"p":null}`)
	var got, want node
	if err := vjson.Unmarshal(input, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(input, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("null:\n  got:  %+v\n  want: %+v", got, want)
	}

	// Top-level and map values.
	var top netip.Addr
	if err := vjson.Unmarshal([]byte(`"2001:db8::9"`), &top); err != nil {
		t.Fatal(err)
	}
	if top != netip.MustParseAddr("2001:db8::9") {
		t.Errorf("top-level addr = %v", top)
	}

	// Empty string decodes to the zero Addr (Go >= 1.24 semantics),
	// completing the zero-value round trip (zero Addr marshals as "").
	var empty, emptyStd netip.Addr
	if err := vjson.Unmarshal([]byte(`""`), &empty); err != nil {
		t.Fatalf("empty string: %v", err)
	}
	if err := json.Unmarshal([]byte(`""`), &emptyStd); err != nil {
		t.Fatal(err)
	}
	if empty != emptyStd {
		t.Errorf("empty string: got %v, want %v", empty, emptyStd)
	}

	var m map[string]netip.Addr
	if err := vjson.Unmarshal([]byte(`{"a":"1.2.3.4"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m["a"] != netip.MustParseAddr("1.2.3.4") {
		t.Errorf("map addr = %v", m["a"])
	}
}

func TestStdlib_Netip_UnmarshalErrors(t *testing.T) {
	// TextUnmarshaler only applies to JSON strings; everything else and
	// invalid IP text must error, matching stdlib.
	inputs := []string{
		`"not-an-ip"`,
		`"192.168.1.256"`,
		`"1.2.3.4/24"`, // prefix text is not an Addr
		`123`,
		`true`,
		`{"a":"1.2.3.4"}`,
		`[":%70"]`,      // invalid AddrPort
		`"10.0.0.0/33"`, // invalid Prefix bits
	}
	for _, in := range inputs {
		var vjErr, stdErr error
		switch in {
		case `":%70"`:
			var ap netip.AddrPort
			vjErr = vjson.Unmarshal([]byte(in), &ap)
			stdErr = json.Unmarshal([]byte(in), &ap)
		case `"10.0.0.0/33"`:
			var p netip.Prefix
			vjErr = vjson.Unmarshal([]byte(in), &p)
			stdErr = json.Unmarshal([]byte(in), &p)
		default:
			var a netip.Addr
			vjErr = vjson.Unmarshal([]byte(in), &a)
			stdErr = json.Unmarshal([]byte(in), &a)
		}
		if stdErr == nil {
			t.Errorf("input %s: stdlib unexpectedly succeeded", in)
			continue
		}
		if vjErr == nil {
			t.Errorf("input %s: vjson succeeded, stdlib error: %v", in, stdErr)
		}
	}
}

// ---------- slog.Level / slog.LevelVar ----------

func TestStdlib_SlogLevel_Marshal(t *testing.T) {
	levels := []slog.Level{
		slog.LevelDebug, // -4
		slog.LevelInfo,  // 0
		slog.LevelWarn,  // 4
		slog.LevelError, // 8
		slog.LevelDebug + 4,
		slog.LevelError - 2,
		slog.LevelInfo + 12,
		slog.Level(-100),
	}
	for _, lv := range levels {
		vjData, err := vjson.Marshal(lv)
		if err != nil {
			t.Fatalf("marshal %v: %v", lv, err)
		}
		stdData, _ := json.Marshal(lv)
		if string(vjData) != string(stdData) {
			t.Errorf("level %v:\n  vjson:  %s\n  stdlib: %s", lv, vjData, stdData)
		}
	}

	type cfg struct {
		Name  string     `json:"name"`
		Level slog.Level `json:"level"`
	}
	val := cfg{Name: "svc", Level: slog.LevelWarn}
	vjData, err := vjson.Marshal(val)
	if err != nil {
		t.Fatal(err)
	}
	stdData, _ := json.Marshal(val)
	if string(vjData) != string(stdData) {
		t.Errorf("struct:\n  vjson:  %s\n  stdlib: %s", vjData, stdData)
	}
}

func TestStdlib_SlogLevel_Unmarshal(t *testing.T) {
	// UnmarshalText ignores case and accepts equivalent numeric offsets.
	inputs := []string{
		`"INFO"`,
		`"debug"`,
		`"Warn+3"`,
		`"Error-8"`, // == INFO
		`"DEBUG+4"`,
	}
	for _, in := range inputs {
		var got, want slog.Level
		if err := vjson.Unmarshal([]byte(in), &got); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if err := json.Unmarshal([]byte(in), &want); err != nil {
			t.Fatalf("%s: stdlib: %v", in, err)
		}
		if got != want {
			t.Errorf("%s: got %v, want %v", in, got, want)
		}
	}

	type cfg struct {
		Level slog.Level `json:"level"`
	}
	var got, want cfg
	if err := vjson.Unmarshal([]byte(`{"level":"warn"}`), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"level":"warn"}`), &want); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("struct: got %+v, want %+v", got, want)
	}
}

func TestStdlib_SlogLevel_UnmarshalErrors(t *testing.T) {
	inputs := []string{`"bogus"`, `"INFO+x"`, `"12"`, `0`, `null`, `[]`}
	for _, in := range inputs {
		var got slog.Level
		vjErr := vjson.Unmarshal([]byte(in), &got)
		stdErr := json.Unmarshal([]byte(in), new(slog.Level))
		if stdErr == nil {
			t.Errorf("input %s: stdlib unexpectedly succeeded", in)
			continue
		}
		if vjErr == nil {
			t.Errorf("input %s: vjson succeeded, stdlib error: %v", in, stdErr)
		}
	}
}

// *slog.LevelVar implements TextMarshaler/TextUnmarshaler with pointer
// receivers only.

func TestStdlib_SlogLevelVar_RoundTrip(t *testing.T) {
	type cfg struct {
		Name string         `json:"name"`
		Lvl  *slog.LevelVar `json:"lvl"`
	}
	in := []byte(`{"name":"svc","lvl":"warn"}`)

	var got, want cfg
	if err := vjson.Unmarshal(in, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(in, &want); err != nil {
		t.Fatal(err)
	}
	if got.Lvl == nil || got.Lvl.Level() != want.Lvl.Level() {
		t.Errorf("lvl: got %v, want %v", got.Lvl, want.Lvl.Level())
	}

	vjData, err := vjson.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	stdData, _ := json.Marshal(want)
	if string(vjData) != string(stdData) {
		t.Errorf("marshal:\n  vjson:  %s\n  stdlib: %s", vjData, stdData)
	}
}

// ---------- composite config ----------

func TestStdlib_NetipSlog_Config_RoundTrip(t *testing.T) {
	type server struct {
		Addr     netip.Addr     `json:"addr"`
		Endpoint netip.AddrPort `json:"endpoint"`
		Subnet   netip.Prefix   `json:"subnet"`
		LogLevel slog.Level     `json:"log_level"`
		Fallback *netip.Addr    `json:"fallback,omitempty"`
	}
	orig := server{
		Addr:     netip.MustParseAddr("2001:db8::5"),
		Endpoint: netip.MustParseAddrPort("[2001:db8::5]:8443"),
		Subnet:   netip.MustParsePrefix("2001:db8::/64"),
		LogLevel: slog.LevelError - 2,
	}
	data, err := vjson.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got server
	if err := vjson.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != orig {
		t.Errorf("round trip:\n  got:  %+v\n  want: %+v", got, orig)
	}

	stdData, _ := json.Marshal(orig)
	if string(data) != string(stdData) {
		t.Errorf("marshal:\n  vjson:  %s\n  stdlib: %s", data, stdData)
	}
}
