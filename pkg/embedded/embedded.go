// Package embedded is one data month of NHTSA's vPIC dump as a nitrovin
// artifact, built into every binary that imports it: what nitrovin decodes
// against when it is given no other data.
//
// The bytes are this module's release asset nitrovin-<Month>.artifact.
// Before every release, the release workflow rebuilds them from NHTSA's dump
// and requires the same bytes; manifest.json, beside this file, records the
// dump, the oracle's collation and the importer commit it builds with (ADR
// 0007 in github.com/joshuasprow/nitrovin). A binary that calls Artifact
// grows by the artifact's size, about 61 MB; package artifact alone adds
// nothing.
package embedded

import (
	_ "embed"
	"unsafe"
)

// Month is the embedded artifact's data month, YYYY_MM.
const Month = "2026_11"

// BLAKE3 is the embedded artifact's hash in hex, as its header records it
// and artifact.Artifact.Hash returns it.
const BLAKE3 = "dd565ff9a1878af2366b1d5e8e26efc1684cb969ceb23abd7bc274373006af19"

// data is a string so that its bytes are read-only data in the binary: a
// write through Artifact faults instead of corrupting every decoder.
//
//go:embed nitrovin.artifact
var data string

// Artifact returns the embedded artifact's bytes, to give to artifact.Open,
// which reads them in place. Every call returns the same bytes, which are
// read-only: writing to them crashes the program.
func Artifact() []byte { return unsafe.Slice(unsafe.StringData(data), len(data)) }
