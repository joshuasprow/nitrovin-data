package artifact

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fixture is every table of the schema, empty except three with rows given
// out of order, with NULLs, empty strings and repeated strings.
func fixture() []TableData {
	tables := make([]TableData, len(schema))
	for i, ts := range schema {
		tables[i] = TableData{Name: ts.Name, Columns: make([]Column, len(ts.Columns))}
	}
	set := func(name string, cols ...Column) {
		for i := range tables {
			if tables[i].Name == name {
				tables[i].Columns = cols
			}
		}
	}
	// wmi: wmi, id, manufacturerid, makeid, vehicletypeid, trucktypeid, publicavailabilitydate, changedon
	set("wmi",
		Column{String: []string{"1HG", "1G1", "1HG", "5YJ"}},
		Column{Int32: []int32{7, 3, 2, 9}},
		Column{Int32: []int32{1, 2, 1, NullInt32}},
		Column{Int32: []int32{10, 20, 10, 30}},
		Column{Int32: []int32{2, 2, 2, 7}},
		Column{Int32: []int32{NullInt32, 1, NullInt32, NullInt32}},
		Column{Int64: []int64{1_000_000, NullInt64, -5, 1_700_000_000_000_000}},
		Column{Int64: []int64{1, 2, 3, 4}},
	)
	// element: id, name, code, lookuptable, isprivate, groupname, datatype, decode, weight
	set("element",
		Column{Int32: []int32{26, 5, 143}},
		Column{String: []string{"Make", "Body Class", "Error Code"}},
		Column{String: []string{"Make", "BodyClass", ""}, Null: []bool{false, false, true}},
		Column{String: []string{"Make", "BodyStyle", "ErrorCode"}},
		Column{Bool: []uint8{0, NullBool, 1}},
		Column{String: []string{"General", "Exterior / Body", ""}},
		Column{String: []string{"lookup", "lookup", "string"}},
		Column{String: []string{"", "x", ""}, Null: []bool{false, false, true}}, // "" is not NULL
		Column{Int32: []int32{NullInt32, 3, 0}},
	)
	// lookup: table, id, name
	set("lookup",
		Column{String: []string{"make", "bodystyle", "make", "bodystyle"}},
		Column{Int32: []int32{474, 64, 441, 5}},
		Column{String: []string{"HONDA", "Sedan", "Citroën", "Convertible"}},
	)
	return tables
}

func mustEncode(t *testing.T, tables []TableData) []byte {
	t.Helper()
	b, err := Encode("2026_10", tables)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustOpen(t *testing.T, b []byte) *Artifact {
	t.Helper()
	a, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRoundTripSortsRowsAndKeepsNulls(t *testing.T) {
	a := mustOpen(t, mustEncode(t, fixture()))
	if a.Month() != "2026_10" {
		t.Errorf("Month = %q", a.Month())
	}

	wmi := a.Table("wmi")
	codes := wmi.Strings("wmi")
	var got []string
	for i := range codes.Len() {
		s, _ := codes.At(i)
		got = append(got, s)
	}
	if want := []string{"1G1", "1HG", "1HG", "5YJ"}; !slices.Equal(got, want) {
		t.Errorf("wmi order = %v, want %v", got, want)
	}
	// Ties on the key fall to the next column: 1HG rows by id.
	if ids := wmi.Int32s("id"); !slices.Equal(ids, []int32{3, 2, 7, 9}) {
		t.Errorf("wmi ids = %v", ids)
	}
	if v := wmi.Int64s("publicavailabilitydate"); !slices.Equal(v, []int64{NullInt64, -5, 1_000_000, 1_700_000_000_000_000}) {
		t.Errorf("publicavailabilitydate = %v", v)
	}
	if codes.Ref(1) != codes.Ref(2) || codes.Ref(0) == codes.Ref(1) {
		t.Error("equal strings must share a reference, different ones must not")
	}

	el := a.Table("element")
	if ids := el.Int32s("id"); !slices.Equal(ids, []int32{5, 26, 143}) {
		t.Fatalf("element ids = %v", ids)
	}
	decode := el.Strings("decode")
	if v, ok := decode.At(1); !ok || v != "" {
		t.Errorf("Make decode = %q, %v; want empty and not NULL", v, ok)
	}
	if _, ok := decode.At(2); ok {
		t.Error("Error Code decode should be NULL")
	}
	priv := el.Bools("isprivate")
	for i, want := range []struct{ v, ok bool }{{false, false}, {false, true}, {true, true}} {
		if v, ok := priv.At(i); v != want.v || ok != want.ok {
			t.Errorf("isprivate[%d] = %v, %v; want %v, %v", i, v, ok, want.v, want.ok)
		}
	}
	if w := el.Int32s("weight"); w[1] != NullInt32 || w[2] != 0 {
		t.Errorf("weight = %v: NULL and 0 must differ", w)
	}

	m := a.Manifest()
	if m.Rows["wmi"] != 4 || m.Rows["element"] != 3 || m.Rows["pattern"] != 0 || len(m.Rows) != len(schema) {
		t.Errorf("manifest rows = %v", m.Rows)
	}
	if m.Bytes != len(a.data) || m.Format != FormatVersion || m.Month != "2026_10" {
		t.Errorf("manifest = %+v", m)
	}
}

func TestEncodeIsAFunctionOfTheRowSet(t *testing.T) {
	want := mustEncode(t, fixture())
	rng := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		tables := fixture()
		for ti := range tables {
			shuffle(rng, &tables[ti])
		}
		if got := mustEncode(t, tables); !bytes.Equal(got, want) {
			t.Fatal("shuffled input rows changed the bytes")
		}
	}
}

func shuffle(rng *rand.Rand, td *TableData) {
	cols := td.Columns
	n := len(cols[0].Int32) + len(cols[0].String)
	rng.Shuffle(n, func(i, j int) {
		for c := range cols {
			col := &cols[c]
			switch {
			case col.Int32 != nil:
				col.Int32[i], col.Int32[j] = col.Int32[j], col.Int32[i]
			case col.Int64 != nil:
				col.Int64[i], col.Int64[j] = col.Int64[j], col.Int64[i]
			case col.Bool != nil:
				col.Bool[i], col.Bool[j] = col.Bool[j], col.Bool[i]
			case col.String != nil:
				col.String[i], col.String[j] = col.String[j], col.String[i]
				if col.Null != nil {
					col.Null[i], col.Null[j] = col.Null[j], col.Null[i]
				}
			}
		}
	})
}

func TestEqualRange(t *testing.T) {
	a := mustOpen(t, mustEncode(t, fixture()))
	lk := a.Table("lookup")
	names := lk.Strings("table")
	lo, hi := names.EqualRange(0, names.Len(), "make")
	if lo != 2 || hi != 4 {
		t.Fatalf("make rows = [%d, %d), want [2, 4)", lo, hi)
	}
	i, j := EqualRange(lk.Int32s("id"), lo, hi, 474)
	if j-i != 1 {
		t.Fatalf("make 474: %d rows", j-i)
	}
	if v, _ := lk.Strings("name").At(i); v != "HONDA" {
		t.Errorf("make 474 = %q", v)
	}
	if i, j := EqualRange(lk.Int32s("id"), lo, hi, 64); i != j {
		t.Error("bodystyle 64 found under make")
	}
	if i, j := names.EqualRange(0, names.Len(), "model"); i != j || i != 4 {
		t.Errorf("absent table = [%d, %d)", i, j)
	}
}

// TestOpenMisalignedInput: go:embed data can start at any address. Open
// reads it in place where the host loads unaligned integers, copies it
// elsewhere, and reads the same values either way.
func TestOpenMisalignedInput(t *testing.T) {
	b := mustEncode(t, fixture())
	buf := make([]byte, len(b)+1)
	copy(buf[1:], b)
	a := mustOpen(t, buf[1:])
	if inPlace := &a.data[0] == &buf[1]; inPlace != unalignedLoads {
		t.Errorf("read in place: %t, want %t on %s", inPlace, unalignedLoads, runtime.GOARCH)
	}
	if err := a.Verify(); err != nil {
		t.Fatal(err)
	}
	if ids := a.Table("element").Int32s("id"); !slices.Equal(ids, []int32{5, 26, 143}) {
		t.Errorf("element ids = %v", ids)
	}
	if got, want := a.Manifest(), mustOpen(t, b).Manifest(); !reflect.DeepEqual(got, want) {
		t.Errorf("manifest %+v, aligned %+v", got, want)
	}
}

func TestOpenRejectsDamage(t *testing.T) {
	good := mustEncode(t, fixture())
	le := binary.LittleEndian
	for name, damage := range map[string]func([]byte) []byte{
		"magic":     func(b []byte) []byte { b[0] = 'N'; return b },
		"format":    func(b []byte) []byte { le.PutUint32(b[8:], FormatVersion+1); return b },
		"truncated": func(b []byte) []byte { return b[:len(b)-8] },
		"short":     func(b []byte) []byte { return b[:40] },
		"column offset": func(b []byte) []byte {
			first := headerSize + arenaEntrySize + len(schema)*tableEntrySize
			le.PutUint64(b[first+8:], uint64(len(b)))
			return b
		},
		"string offsets": func(b []byte) []byte {
			off := le.Uint64(b[headerSize:])
			le.PutUint32(b[off+4:], 1<<30)
			return b
		},
		"split character": func(b []byte) []byte {
			a, _ := Open(b)
			for r := range uint32(a.Strings()) {
				if s, _ := a.String(r); s == "Citroën" {
					off := le.Uint64(b[headerSize:]) + 4*uint64(r)
					le.PutUint32(b[off+4:], le.Uint32(b[off:])+6) // inside the ë
				}
			}
			return b
		},
	} {
		b := damage(bytes.Clone(good))
		if _, err := Open(b); err == nil {
			t.Errorf("%s: Open accepted the damaged artifact", name)
		}
	}
}

func TestVerifyCatchesHashAndOrder(t *testing.T) {
	good := mustEncode(t, fixture())

	b := bytes.Clone(good)
	b[len(b)-1] ^= 1
	a, err := Open(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(); err == nil || !strings.Contains(err.Error(), "blake3") {
		t.Errorf("flipped byte: Verify = %v", err)
	}

	// Swap two element rows' ids and re-hash: the hash is right, the order is not.
	b = bytes.Clone(good)
	a, _ = Open(b)
	ids := a.Table("element").Int32s("id")
	ids[0], ids[1] = ids[1], ids[0]
	h := hash(b)
	copy(b[hashOff:headerSize], h[:])
	a, _ = Open(b)
	if err := a.Verify(); err == nil || !strings.Contains(err.Error(), "out of order") {
		t.Errorf("swapped rows: Verify = %v", err)
	}
}

func TestEncodeRejects(t *testing.T) {
	for name, damage := range map[string]func([]TableData) []TableData{
		"table order":  func(ts []TableData) []TableData { ts[0], ts[1] = ts[1], ts[0]; return ts },
		"missing":      func(ts []TableData) []TableData { return ts[:len(ts)-1] },
		"ragged":       func(ts []TableData) []TableData { ts[0].Columns[1].Int32 = ts[0].Columns[1].Int32[:1]; return ts },
		"bool":         func(ts []TableData) []TableData { ts[4].Columns[4].Bool[0] = 2; return ts },
		"utf-8":        func(ts []TableData) []TableData { ts[0].Columns[0].String[0] = "\xff"; return ts },
		"null flags":   func(ts []TableData) []TableData { ts[4].Columns[2].Null = []bool{true}; return ts },
		"column count": func(ts []TableData) []TableData { ts[4].Columns = ts[4].Columns[:3]; return ts },
	} {
		if _, err := Encode("2026_10", damage(fixture())); err == nil {
			t.Errorf("%s: Encode accepted it", name)
		}
	}
	for _, m := range []string{"2026-10", "2026_13", "2026_00", "26_10", "2026_1x", ""} {
		if _, err := Encode(m, fixture()); err == nil {
			t.Errorf("month %q accepted", m)
		}
	}
}
