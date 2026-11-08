package artifact

import (
	"cmp"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"
	"unsafe"
)

// Layout constants; doc.go describes the layout.
const (
	magic           = "nitrovin"
	headerSize      = 64
	hashOff         = 32
	arenaEntrySize  = 16
	tableEntrySize  = 16
	columnEntrySize = 16
)

// Artifact is an opened artifact, read in place: its columns and strings are
// views into the bytes given to Open, which must not change afterwards.
type Artifact struct {
	data    []byte
	month   uint32
	hash    [32]byte
	offsets []uint32 // string i is arena[offsets[i]:offsets[i+1]]
	arena   string
	tables  []Table
}

// Table is one table of an Artifact.
type Table struct {
	a    *Artifact
	name string
	rows int
	key  int
	cols []column
}

type column struct {
	name string
	typ  Type
	off  int
}

// Open checks the header, the directory against Schema, the bounds of every
// section and the string arena (offsets in range, each string valid UTF-8),
// then returns the artifact without copying it, unless data is not 8-byte
// aligned, in which case it is copied once. It does not check the hash or the
// row order; Verify does.
func Open(data []byte) (*Artifact, error) {
	if !littleEndian() {
		return nil, errors.New("artifact: big-endian hosts are not supported")
	}
	if len(data) < headerSize || string(data[:8]) != magic {
		return nil, errors.New("artifact: not a nitrovin artifact")
	}
	le := binary.LittleEndian
	if v := le.Uint32(data[8:]); v != FormatVersion {
		return nil, fmt.Errorf("artifact: format %d, this reader reads %d", v, FormatVersion)
	}
	if n := le.Uint64(data[16:]); n != uint64(len(data)) {
		return nil, fmt.Errorf("artifact: header says %d bytes, have %d", n, len(data))
	}
	if uintptr(unsafe.Pointer(&data[0]))%8 != 0 {
		buf := make([]uint64, (len(data)+7)/8)
		aligned := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), len(data))
		copy(aligned, data)
		data = aligned
	}
	a := &Artifact{data: data, month: le.Uint32(data[12:])}
	copy(a.hash[:], data[hashOff:headerSize])
	if a.month%100 < 1 || a.month%100 > 12 {
		return nil, fmt.Errorf("artifact: data month %d", a.month)
	}
	nTables := int(le.Uint32(data[24:]))
	nStrings := int(le.Uint32(data[28:]))
	if nTables != len(schema) {
		return nil, fmt.Errorf("artifact: %d tables, format %d has %d", nTables, FormatVersion, len(schema))
	}
	nCols := 0
	for _, ts := range schema {
		nCols += len(ts.Columns)
	}
	dirEnd := headerSize + arenaEntrySize + nTables*tableEntrySize + nCols*columnEntrySize
	if dirEnd > len(data) {
		return nil, errors.New("artifact: truncated directory")
	}

	offsetsOff, err := section(data, le.Uint64(data[headerSize:]), uint64(nStrings+1)*4, dirEnd)
	if err != nil {
		return nil, fmt.Errorf("artifact: string offsets: %w", err)
	}
	a.offsets = unsafe.Slice((*uint32)(unsafe.Pointer(&data[offsetsOff])), nStrings+1)
	arenaLen := uint64(a.offsets[nStrings])
	bytesOff, err := section(data, le.Uint64(data[headerSize+8:]), arenaLen, dirEnd)
	if err != nil {
		return nil, fmt.Errorf("artifact: string arena: %w", err)
	}
	a.arena = unsafe.String(unsafe.SliceData(data[bytesOff:]), int(arenaLen))
	if a.offsets[0] != 0 {
		return nil, errors.New("artifact: string 0 does not start the arena")
	}
	// A valid arena whose strings all start on a rune boundary holds only
	// valid strings, and one pass over it is far cheaper than one per string.
	if !utf8.ValidString(a.arena) {
		return nil, errors.New("artifact: string arena is not UTF-8")
	}
	for i := 1; i <= nStrings; i++ {
		lo, hi := a.offsets[i-1], a.offsets[i]
		if lo > hi {
			return nil, fmt.Errorf("artifact: string %d out of the arena", i-1)
		}
		if uint64(hi) < arenaLen && !utf8.RuneStart(a.arena[hi]) {
			return nil, fmt.Errorf("artifact: string %d starts inside a character", i)
		}
	}

	name := func(ref uint32) (string, error) {
		s, ok := a.str(ref)
		if !ok {
			return "", fmt.Errorf("artifact: name reference %d out of range", ref)
		}
		return s, nil
	}
	a.tables = make([]Table, nTables)
	colDir := headerSize + arenaEntrySize + nTables*tableEntrySize
	first := 0
	for ti, ts := range schema {
		e := data[headerSize+arenaEntrySize+ti*tableEntrySize:]
		tn, err := name(le.Uint32(e))
		if err != nil {
			return nil, err
		}
		t := Table{a: a, name: tn, rows: int(le.Uint32(e[4:])), key: int(le.Uint16(e[14:]))}
		if tn != ts.Name || int(le.Uint16(e[12:])) != len(ts.Columns) || t.key != ts.Key || int(le.Uint32(e[8:])) != first {
			return nil, fmt.Errorf("artifact: table %d is %q, format %d expects %q", ti, tn, FormatVersion, ts.Name)
		}
		for ci, cs := range ts.Columns {
			ce := data[colDir+(first+ci)*columnEntrySize:]
			cn, err := name(le.Uint32(ce))
			if err != nil {
				return nil, err
			}
			c := column{name: cn, typ: Type(ce[4])}
			if cn != cs.Name || c.typ != cs.Type {
				return nil, fmt.Errorf("artifact: %s column %d is %s %s, format %d expects %s %s",
					tn, ci, cn, c.typ, FormatVersion, cs.Name, cs.Type)
			}
			if c.off, err = section(data, le.Uint64(ce[8:]), uint64(t.rows)*uint64(c.typ.width()), dirEnd); err != nil {
				return nil, fmt.Errorf("artifact: %s.%s: %w", tn, cn, err)
			}
			t.cols = append(t.cols, c)
		}
		first += len(ts.Columns)
		a.tables[ti] = t
	}
	return a, nil
}

// section checks that [off, off+n) lies after the directory, inside data, and
// starts 8-byte aligned.
func section(data []byte, off, n uint64, dirEnd int) (int, error) {
	if off%8 != 0 || off < uint64(dirEnd) || off > uint64(len(data)) || n > uint64(len(data))-off {
		return 0, fmt.Errorf("section [%d, +%d) out of bounds", off, n)
	}
	return int(off), nil
}

func littleEndian() bool {
	x := uint16(1)
	return *(*byte)(unsafe.Pointer(&x)) == 1
}

// Month is the data month the artifact was built from, YYYY_MM.
func (a *Artifact) Month() string { return formatMonth(a.month) }

// Hash is the blake3 recorded in the header: of the whole file except the
// hash field.
func (a *Artifact) Hash() [32]byte { return a.hash }

// Size is the artifact's length in bytes.
func (a *Artifact) Size() int { return len(a.data) }

// Strings is the number of distinct strings in the arena.
func (a *Artifact) Strings() int { return len(a.offsets) - 1 }

// Tables lists every table in file order.
func (a *Artifact) Tables() []*Table {
	out := make([]*Table, len(a.tables))
	for i := range a.tables {
		out[i] = &a.tables[i]
	}
	return out
}

// Table returns the named table, or nil when the format has none by that name.
func (a *Artifact) Table(name string) *Table {
	for i := range a.tables {
		if a.tables[i].name == name {
			return &a.tables[i]
		}
	}
	return nil
}

// String returns the arena string a reference names; false for NullRef.
// It panics on a reference out of range, which Verify rules out.
func (a *Artifact) String(ref uint32) (string, bool) {
	if ref == NullRef {
		return "", false
	}
	return a.arena[a.offsets[ref]:a.offsets[ref+1]], true
}

func (a *Artifact) str(ref uint32) (string, bool) {
	if ref == NullRef || uint64(ref) >= uint64(len(a.offsets)-1) {
		return "", false
	}
	return a.String(ref)
}

// Verify recomputes the hash and checks what Open leaves out: every string
// reference in range, every bool 0, 1 or NULL, and every table's rows in
// stored order (ascending by every column, NULL first).
func (a *Artifact) Verify() error {
	if h := hash(a.data); h != a.hash {
		return fmt.Errorf("artifact: blake3 %s, header says %s", hex.EncodeToString(h[:]), hex.EncodeToString(a.hash[:]))
	}
	n := uint32(a.Strings())
	for _, t := range a.Tables() {
		for _, c := range t.cols {
			switch c.typ {
			case TypeString:
				for i, r := range t.Strings(c.name).refs {
					if r != NullRef && r >= n {
						return fmt.Errorf("artifact: %s.%s row %d: string reference %d of %d", t.name, c.name, i, r, n)
					}
				}
			case TypeBool:
				for i, v := range t.Bools(c.name) {
					if v > 1 && v != NullBool {
						return fmt.Errorf("artifact: %s.%s row %d: bool %d", t.name, c.name, i, v)
					}
				}
			}
		}
		for i := 1; i < t.rows; i++ {
			if t.compareRows(i-1, i) > 0 {
				return fmt.Errorf("artifact: %s rows %d and %d out of order", t.name, i-1, i)
			}
		}
	}
	return nil
}

func (t *Table) compareRows(i, j int) int {
	for _, c := range t.cols {
		var r int
		switch c.typ {
		case TypeInt32:
			v := t.Int32s(c.name)
			r = cmp.Compare(v[i], v[j])
		case TypeInt64:
			v := t.Int64s(c.name)
			r = cmp.Compare(v[i], v[j])
		case TypeBool:
			v := t.Bools(c.name)
			r = cmp.Compare(boolRank(v[i]), boolRank(v[j]))
		case TypeString:
			v := t.Strings(c.name)
			x, xok := v.At(i)
			y, yok := v.At(j)
			if xok != yok {
				r = cmp.Compare(b2i(xok), b2i(yok))
			} else {
				r = cmp.Compare(x, y)
			}
		}
		if r != 0 {
			return r
		}
	}
	return 0
}

// Name is the table's name.
func (t *Table) Name() string { return t.name }

// Rows is the table's row count.
func (t *Table) Rows() int { return t.rows }

// Key is how many leading columns form the table's lookup key.
func (t *Table) Key() int { return t.key }

// Columns lists the table's columns in stored (sort) order.
func (t *Table) Columns() []ColumnSchema {
	out := make([]ColumnSchema, len(t.cols))
	for i, c := range t.cols {
		out[i] = ColumnSchema{c.name, c.typ}
	}
	return out
}

func (t *Table) col(name string, typ Type) []byte {
	for _, c := range t.cols {
		if c.name == name {
			if c.typ != typ {
				panic(fmt.Sprintf("artifact: %s.%s is %s, not %s", t.name, name, c.typ, typ))
			}
			return t.a.data[c.off:]
		}
	}
	panic(fmt.Sprintf("artifact: table %s has no column %s", t.name, name))
}

// Int32s returns an int32 column; NullInt32 marks NULL. It panics when the
// table has no int32 column by that name: Open has already checked the file
// against Schema, so that is a programming error.
func (t *Table) Int32s(name string) []int32 {
	b := t.col(name, TypeInt32)
	if t.rows == 0 {
		return nil
	}
	return unsafe.Slice((*int32)(unsafe.Pointer(&b[0])), t.rows)
}

// Int64s returns an int64 column; NullInt64 marks NULL. It panics like Int32s.
func (t *Table) Int64s(name string) []int64 {
	b := t.col(name, TypeInt64)
	if t.rows == 0 {
		return nil
	}
	return unsafe.Slice((*int64)(unsafe.Pointer(&b[0])), t.rows)
}

// Bools returns a bool column. It panics like Int32s.
func (t *Table) Bools(name string) Bools {
	return Bools(t.col(name, TypeBool)[:t.rows:t.rows])
}

// Strings returns a string column. It panics like Int32s.
func (t *Table) Strings(name string) Strings {
	b := t.col(name, TypeString)
	s := Strings{a: t.a}
	if t.rows > 0 {
		s.refs = unsafe.Slice((*uint32)(unsafe.Pointer(&b[0])), t.rows)
	}
	return s
}

// Bools is a bool column: 0, 1, or NullBool per row.
type Bools []uint8

// At returns row i's value; ok is false for NULL.
func (b Bools) At(i int) (v, ok bool) { return b[i] == 1, b[i] != NullBool }

// Strings is a string column: one arena reference per row.
type Strings struct {
	a    *Artifact
	refs []uint32
}

// Len is the number of rows.
func (s Strings) Len() int { return len(s.refs) }

// At returns row i's string; ok is false for NULL. The string is a view into
// the artifact's bytes.
func (s Strings) At(i int) (v string, ok bool) { return s.a.String(s.refs[i]) }

// Ref returns row i's arena reference, NullRef for NULL. Strings are interned,
// so within one artifact two rows hold equal strings exactly when their
// references are equal.
func (s Strings) Ref(i int) uint32 { return s.refs[i] }

// EqualRange returns the rows [i, j) within [lo, hi) whose value is v, for a
// column sorted within that range (a key column, under any equal prefix).
func (s Strings) EqualRange(lo, hi int, v string) (i, j int) {
	less := func(k int, orEqual bool) bool {
		x, ok := s.At(k)
		if !ok {
			return true // NULL sorts first
		}
		if orEqual {
			return x <= v
		}
		return x < v
	}
	i = lo + sort.Search(hi-lo, func(k int) bool { return !less(lo+k, false) })
	j = i + sort.Search(hi-i, func(k int) bool { return !less(i+k, true) })
	return i, j
}

// EqualRange returns the rows [i, j) within [lo, hi) whose value is v, for a
// column sorted within that range (a key column, under any equal prefix).
func EqualRange[T cmp.Ordered](col []T, lo, hi int, v T) (i, j int) {
	i = lo + sort.Search(hi-lo, func(k int) bool { return col[lo+k] >= v })
	j = i + sort.Search(hi-i, func(k int) bool { return col[i+k] > v })
	return i, j
}

// Manifest summarises an artifact: what a release records next to it.
type Manifest struct {
	Format  int            `json:"format"`
	Month   string         `json:"data_month"`
	Bytes   int            `json:"bytes"`
	Blake3  string         `json:"blake3"`
	Strings int            `json:"strings"`
	Rows    map[string]int `json:"rows"`
}

// Manifest returns the artifact's manifest: format, data month, size, hash,
// and every table's row count.
func (a *Artifact) Manifest() Manifest {
	m := Manifest{
		Format:  FormatVersion,
		Month:   a.Month(),
		Bytes:   len(a.data),
		Blake3:  hex.EncodeToString(a.hash[:]),
		Strings: a.Strings(),
		Rows:    make(map[string]int, len(a.tables)),
	}
	for _, t := range a.tables {
		m.Rows[t.name] = t.rows
	}
	return m
}
