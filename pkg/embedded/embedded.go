// Package embedded is one data month of NHTSA's vPIC dump as a nitrovin
// artifact, built into every binary that imports it: what nitrovin decodes
// against when it is given no other data.
//
// The bytes are this module's release asset nitrovin-<Month>.artifact.
// Before every release, the release workflow rebuilds them from NHTSA's dump
// and requires the same bytes; manifest.json, beside this file, records the
// dump, the oracle's collation and the importer commit it builds with (ADR
// 0007 in github.com/joshuasprow/nitrovin). Importing this package adds the
// artifact's size, about 61 MB, to a binary; package artifact alone adds
// nothing.
package embedded

import _ "embed"

// Month is the embedded artifact's data month, YYYY_MM.
const Month = "2026_10"

// BLAKE3 is the embedded artifact's hash in hex, as its header records it
// and artifact.Artifact.Hash returns it.
const BLAKE3 = "7518c5b46c4264fe94a8e5158cb1949b000e50803b36d388fec9ebe914a780cb"

// data is a []byte, not a string: the linker aligns a []byte like any large
// data symbol, but packs a string's bytes at any address, and artifact.Open
// copies bytes that are not 8-byte aligned, on every start.
//
//go:embed nitrovin.artifact
var data []byte

// Artifact returns the embedded artifact's bytes, to give to artifact.Open.
// Every call returns the same bytes, which must not be modified.
func Artifact() []byte { return data }
