package artifact

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/zeebo/blake3"
)

// Column is one column's values in input order, for Encode. Only the field
// matching the column's Type is read.
type Column struct {
	Int32  []int32  // TypeInt32; NullInt32 is NULL
	Int64  []int64  // TypeInt64; NullInt64 is NULL
	Bool   []uint8  // TypeBool: 0, 1 or NullBool
	String []string // TypeString
	// Null marks String rows that are NULL rather than their String value.
	// nil means no row is NULL.
	Null []bool
}

func (c *Column) len(t Type) int {
	switch t {
	case TypeInt32:
		return len(c.Int32)
	case TypeInt64:
		return len(c.Int64)
	case TypeBool:
		return len(c.Bool)
	default:
		return len(c.String)
	}
}

// TableData is one table's rows for Encode, columns in Schema order. Rows may
// come in any order: Encode sorts them.
type TableData struct {
	Name    string
	Columns []Column
}

// Encode builds an artifact for the data month (YYYY_MM) from every table of
// Schema, in Schema order. The bytes are a pure function of the month and the
// set of rows in each table: rows are sorted by every column, strings are
// interned first-seen (table and column names, then values in table, row and
// column order), and every section starts on an 8-byte boundary.
//
// A producer must not pass a real value equal to a type's NULL value; Encode
// cannot tell them apart.
func Encode(month string, tables []TableData) ([]byte, error) {
	monthNum, err := parseMonth(month)
	if err != nil {
		return nil, err
	}
	if len(tables) != len(schema) {
		return nil, fmt.Errorf("artifact: %d tables, want %d", len(tables), len(schema))
	}
	rows := make([]int, len(schema))
	for ti, ts := range schema {
		if rows[ti], err = checkTable(ts, &tables[ti]); err != nil {
			return nil, err
		}
	}

	in := interner{index: map[string]uint32{}}
	for _, ts := range schema {
		in.add(ts.Name)
		for _, c := range ts.Columns {
			in.add(c.Name)
		}
	}
	perms := make([][]int32, len(schema))
	refs := make([][][]uint32, len(schema)) // [table][column] for string columns
	for ti, ts := range schema {
		td := &tables[ti]
		perms[ti] = sortRows(ts, td, rows[ti])
		refs[ti] = make([][]uint32, len(ts.Columns))
		for ci, c := range ts.Columns {
			if c.Type == TypeString {
				refs[ti][ci] = make([]uint32, rows[ti])
			}
		}
		for ri, src := range perms[ti] {
			for ci, c := range ts.Columns {
				if c.Type != TypeString {
					continue
				}
				col := &td.Columns[ci]
				ref := NullRef
				if col.Null == nil || !col.Null[src] {
					ref = in.add(col.String[src])
				}
				refs[ti][ci][ri] = ref
			}
		}
	}
	if in.size > math.MaxUint32 {
		return nil, fmt.Errorf("artifact: string arena is %d bytes, over 4 GiB", in.size)
	}

	// Layout: header, directory, arena offsets, arena bytes, columns.
	nCols := 0
	for _, ts := range schema {
		nCols += len(ts.Columns)
	}
	off := headerSize + arenaEntrySize + len(schema)*tableEntrySize + nCols*columnEntrySize
	off = align8(off)
	offsetsOff := off
	off = align8(off + 4*(len(in.strs)+1))
	bytesOff := off
	off = align8(off + in.size)
	colOff := make([][]int, len(schema))
	for ti, ts := range schema {
		colOff[ti] = make([]int, len(ts.Columns))
		for ci, c := range ts.Columns {
			colOff[ti][ci] = off
			off = align8(off + rows[ti]*c.Type.width())
		}
	}
	size := off

	b := make([]byte, size)
	le := binary.LittleEndian
	copy(b, magic)
	le.PutUint32(b[8:], FormatVersion)
	le.PutUint32(b[12:], monthNum)
	le.PutUint64(b[16:], uint64(size))
	le.PutUint32(b[24:], uint32(len(schema)))
	le.PutUint32(b[28:], uint32(len(in.strs)))

	d := headerSize
	le.PutUint64(b[d:], uint64(offsetsOff))
	le.PutUint64(b[d+8:], uint64(bytesOff))
	d += arenaEntrySize
	first := 0
	for ti, ts := range schema {
		le.PutUint32(b[d:], in.index[ts.Name])
		le.PutUint32(b[d+4:], uint32(rows[ti]))
		le.PutUint32(b[d+8:], uint32(first))
		le.PutUint16(b[d+12:], uint16(len(ts.Columns)))
		le.PutUint16(b[d+14:], uint16(ts.Key))
		d += tableEntrySize
		first += len(ts.Columns)
	}
	for ti, ts := range schema {
		for ci, c := range ts.Columns {
			le.PutUint32(b[d:], in.index[c.Name])
			b[d+4] = byte(c.Type)
			le.PutUint64(b[d+8:], uint64(colOff[ti][ci]))
			d += columnEntrySize
		}
	}

	pos := 0
	for i, s := range in.strs {
		le.PutUint32(b[offsetsOff+4*i:], uint32(pos))
		pos += copy(b[bytesOff+pos:], s)
	}
	le.PutUint32(b[offsetsOff+4*len(in.strs):], uint32(pos))

	for ti, ts := range schema {
		td := &tables[ti]
		for ci, c := range ts.Columns {
			p := b[colOff[ti][ci]:]
			col := &td.Columns[ci]
			switch c.Type {
			case TypeInt32:
				for ri, src := range perms[ti] {
					le.PutUint32(p[4*ri:], uint32(col.Int32[src]))
				}
			case TypeInt64:
				for ri, src := range perms[ti] {
					le.PutUint64(p[8*ri:], uint64(col.Int64[src]))
				}
			case TypeBool:
				for ri, src := range perms[ti] {
					p[ri] = col.Bool[src]
				}
			case TypeString:
				for ri, ref := range refs[ti][ci] {
					le.PutUint32(p[4*ri:], ref)
				}
			}
		}
	}

	h := hash(b)
	copy(b[hashOff:headerSize], h[:])
	return b, nil
}

func checkTable(ts TableSchema, td *TableData) (int, error) {
	if td.Name != ts.Name {
		return 0, fmt.Errorf("artifact: table %q where %q belongs", td.Name, ts.Name)
	}
	if len(td.Columns) != len(ts.Columns) {
		return 0, fmt.Errorf("artifact: table %s: %d columns, want %d", ts.Name, len(td.Columns), len(ts.Columns))
	}
	rows := td.Columns[0].len(ts.Columns[0].Type)
	if rows > math.MaxUint32 {
		return 0, fmt.Errorf("artifact: table %s: %d rows", ts.Name, rows)
	}
	for ci, c := range ts.Columns {
		col := &td.Columns[ci]
		if n := col.len(c.Type); n != rows {
			return 0, fmt.Errorf("artifact: %s.%s: %d values, want %d", ts.Name, c.Name, n, rows)
		}
		switch c.Type {
		case TypeBool:
			for ri, v := range col.Bool {
				if v > 1 && v != NullBool {
					return 0, fmt.Errorf("artifact: %s.%s row %d: bool %d", ts.Name, c.Name, ri, v)
				}
			}
		case TypeString:
			if col.Null != nil && len(col.Null) != rows {
				return 0, fmt.Errorf("artifact: %s.%s: %d NULL flags, want %d", ts.Name, c.Name, len(col.Null), rows)
			}
			for ri, s := range col.String {
				if !utf8.ValidString(s) {
					return 0, fmt.Errorf("artifact: %s.%s row %d: invalid UTF-8", ts.Name, c.Name, ri)
				}
			}
		}
	}
	return rows, nil
}

// sortRows returns the input row indices in stored order: ascending by every
// column, NULL first.
func sortRows(ts TableSchema, td *TableData, rows int) []int32 {
	cmps := make([]func(a, b int32) int, len(ts.Columns))
	for ci, c := range ts.Columns {
		col := &td.Columns[ci]
		switch c.Type {
		case TypeInt32:
			v := col.Int32
			cmps[ci] = func(a, b int32) int { return cmp.Compare(v[a], v[b]) }
		case TypeInt64:
			v := col.Int64
			cmps[ci] = func(a, b int32) int { return cmp.Compare(v[a], v[b]) }
		case TypeBool:
			v := col.Bool
			cmps[ci] = func(a, b int32) int { return cmp.Compare(boolRank(v[a]), boolRank(v[b])) }
		case TypeString:
			v, null := col.String, col.Null
			cmps[ci] = func(a, b int32) int {
				if null != nil && (null[a] || null[b]) {
					return -cmp.Compare(b2i(null[a]), b2i(null[b]))
				}
				return strings.Compare(v[a], v[b])
			}
		}
	}
	perm := make([]int32, rows)
	for i := range perm {
		perm[i] = int32(i)
	}
	slices.SortFunc(perm, func(a, b int32) int {
		for _, c := range cmps {
			if r := c(a, b); r != 0 {
				return r
			}
		}
		return 0
	})
	return perm
}

// boolRank orders NULL before false before true.
func boolRank(v uint8) int {
	if v == NullBool {
		return -1
	}
	return int(v)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

type interner struct {
	index map[string]uint32
	strs  []string
	size  int
}

func (in *interner) add(s string) uint32 {
	if r, ok := in.index[s]; ok {
		return r
	}
	r := uint32(len(in.strs))
	in.index[s] = r
	in.strs = append(in.strs, s)
	in.size += len(s)
	return r
}

func align8(n int) int { return (n + 7) &^ 7 }

// hash is blake3 over the whole file except the hash field itself.
func hash(b []byte) [32]byte {
	h := blake3.New()
	h.Write(b[:hashOff])
	h.Write(b[headerSize:])
	var sum [32]byte
	h.Sum(sum[:0])
	return sum
}

// parseMonth turns YYYY_MM into YYYYMM.
func parseMonth(m string) (uint32, error) {
	n := uint32(0)
	for i := range len(m) {
		if i == 4 {
			continue
		}
		if m[i] < '0' || m[i] > '9' {
			n = 0
			break
		}
		n = n*10 + uint32(m[i]-'0')
	}
	if len(m) != 7 || m[4] != '_' || n%100 < 1 || n%100 > 12 {
		return 0, fmt.Errorf("artifact: data month %q is not YYYY_MM", m)
	}
	return n, nil
}

func formatMonth(n uint32) string { return fmt.Sprintf("%04d_%02d", n/100, n%100) }
