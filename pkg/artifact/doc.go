// Package artifact reads and writes nitrovin's data artifact: the tables of
// one month of NHTSA's vPIC dump that spVinDecode reads, as fixed-width
// little-endian column arrays sorted by their lookup key, with every string in
// one interned arena (ADR 0003 in github.com/joshuasprow/nitrovin).
//
// Open reads an artifact in place, from embedded bytes or an mmap'd file;
// Encode builds one. Schema lists the tables and columns of FormatVersion.
//
// # Layout
//
// All integers are little-endian. Offsets are from the start of the file.
//
//	header, 64 bytes
//	   0  [8]byte  magic "nitrovin"
//	   8  uint32   format version
//	  12  uint32   data month as YYYYMM
//	  16  uint64   file size
//	  24  uint32   table count
//	  28  uint32   string count (n)
//	  32  [32]byte blake3 of the file without bytes 32-63
//	directory, at 64
//	      uint64   offset of the string offsets
//	      uint64   offset of the string bytes
//	      table count × 16 bytes:
//	        uint32 name (string reference), uint32 rows,
//	        uint32 index of its first column entry, uint16 columns, uint16 key columns
//	      column count × 16 bytes, tables' columns in table order:
//	        uint32 name (string reference), uint8 type, 3 bytes zero, uint64 offset
//	string offsets   (n+1) × uint32: string i is bytes[off[i]:off[i+1]]
//	string bytes     UTF-8
//	column arrays    rows × width of the column's type, tables in order
//
// Every section starts on an 8-byte boundary, padded with zeros.
//
// # Determinism
//
// The bytes are a pure function of the data month and each table's set of
// rows: rows are sorted ascending by every column in order (NULL first,
// strings by bytes), strings are interned first-seen (table and column names
// in Schema order, then values table by table, row by row, column by column),
// and padding is zero.
package artifact
