package artifact

import "fmt"

// FormatVersion is the layout and schema version this package writes and
// reads. Any change to Schema or to the byte layout increments it.
const FormatVersion = 1

// Type is the storage type of a column.
type Type uint8

// Column types. Each one reserves one value as NULL, so NULL stays distinct
// from zero, false and the empty string. A producer must never pass a real
// value equal to the reserved one; the importer rejects such a dump.
const (
	// TypeInt32 is a little-endian int32; NullInt32 is NULL.
	TypeInt32 Type = 1
	// TypeInt64 is a little-endian int64; NullInt64 is NULL. Timestamps are
	// microseconds since 1970-01-01 00:00:00 in the dump's own (zoneless) time.
	TypeInt64 Type = 2
	// TypeBool is one byte, 0 or 1; NullBool is NULL.
	TypeBool Type = 3
	// TypeString is a uint32 reference into the string arena; NullRef is NULL.
	TypeString Type = 4
)

// The reserved NULL value of each type.
const (
	NullInt32 int32  = -1 << 31
	NullInt64 int64  = -1 << 63
	NullBool  uint8  = 0xFF
	NullRef   uint32 = 0xFFFFFFFF
)

func (t Type) String() string {
	switch t {
	case TypeInt32:
		return "int32"
	case TypeInt64:
		return "int64"
	case TypeBool:
		return "bool"
	case TypeString:
		return "string"
	}
	return fmt.Sprintf("Type(%d)", uint8(t))
}

// width is the bytes one value occupies in a column array.
func (t Type) width() int {
	switch t {
	case TypeInt64:
		return 8
	case TypeBool:
		return 1
	default:
		return 4
	}
}

// ColumnSchema names and types one column.
type ColumnSchema struct {
	Name string
	Type Type
}

// TableSchema is one table of the artifact. Rows are stored sorted by every
// column in order, ascending, NULL first and strings by bytes; the first Key
// columns are the lookup key, so a binary search on them finds a row range.
type TableSchema struct {
	Name    string
	Key     int
	Columns []ColumnSchema
}

// Schema returns the tables of FormatVersion in file order. The order also
// fixes string interning, so it is part of the format.
//
// Tables and columns are the dump's, lowercase, restricted to what
// spVinDecode reads (the port plan's logical schema). Two columns are not
// raw dump columns:
//   - changedon is coalesce(updatedon, createdon), the only way the procedure
//     reads either timestamp.
//   - lookup is the union of every table some element.lookuptable names that
//     exists in the dump: table is the relation's (lowercase) name.
//
// wmi_vinschema.yearto keeps its NULL; the procedure's coalesce(yearto, 2999)
// is the engine's to apply.
func Schema() []TableSchema {
	out := make([]TableSchema, len(schema))
	for i, t := range schema {
		out[i] = t
		out[i].Columns = append([]ColumnSchema(nil), t.Columns...)
	}
	return out
}

func i32(name string) ColumnSchema          { return ColumnSchema{name, TypeInt32} }
func i64(name string) ColumnSchema          { return ColumnSchema{name, TypeInt64} }
func boo(name string) ColumnSchema          { return ColumnSchema{name, TypeBool} }
func str(name string) ColumnSchema          { return ColumnSchema{name, TypeString} }
func cols(c ...ColumnSchema) []ColumnSchema { return c }

var schema = []TableSchema{
	{"wmi", 2, cols(str("wmi"), i32("id"), i32("manufacturerid"), i32("makeid"), i32("vehicletypeid"), i32("trucktypeid"), i64("publicavailabilitydate"), i64("changedon"))},
	{"wmi_vinschema", 2, cols(i32("wmiid"), i32("id"), i32("vinschemaid"), i32("yearfrom"), i32("yearto"))},
	{"vinschema", 1, cols(i32("id"), boo("tobeqced"))},
	{"pattern", 2, cols(i32("vinschemaid"), i32("id"), str("keys"), i32("elementid"), str("attributeid"), i64("changedon"))},
	{"element", 1, cols(i32("id"), str("name"), str("code"), str("lookuptable"), boo("isprivate"), str("groupname"), str("datatype"), str("decode"), i32("weight"))},
	{"make_model", 2, cols(i32("modelid"), i32("makeid"))},
	{"wmi_make", 2, cols(i32("wmiid"), i32("makeid"))},
	{"enginemodel", 1, cols(i32("id"), str("name"))},
	{"enginemodelpattern", 2, cols(i32("enginemodelid"), i32("id"), i32("elementid"), str("attributeid"), i64("changedon"))},
	{"defaultvalue", 2, cols(i32("vehicletypeid"), i32("id"), i32("elementid"), str("defaultvalue"), i64("changedon"))},
	{"vinexception", 1, cols(str("vin"), boo("checkdigit"))},
	{"conversion", 2, cols(i32("fromelementid"), i32("id"), i32("toelementid"), str("formula"))},
	{"vehiclespecschema", 1, cols(i32("id"), i32("makeid"), i32("vehicletypeid"), boo("tobeqced"))},
	{"vspecschemapattern", 2, cols(i32("schemaid"), i32("id"))},
	{"vehiclespecpattern", 2, cols(i32("vspecschemapatternid"), i32("id"), boo("iskey"), i32("elementid"), str("attributeid"), i64("changedon"))},
	{"vehiclespecschema_model", 2, cols(i32("modelid"), i32("vehiclespecschemaid"))},
	{"vehiclespecschema_year", 2, cols(i32("vehiclespecschemaid"), i32("year"))},
	{"errorcode", 1, cols(i32("id"), str("name"), str("additionalerrortext"), i32("weight"))},
	{"lookup", 2, cols(str("table"), i32("id"), str("name"))},
}
