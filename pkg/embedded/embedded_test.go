package embedded

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"unsafe"

	"github.com/joshuasprow/nitrovin-data/pkg/artifact"
)

// TestEmbedded: the embedded artifact opens in place and verifies, and its
// month, hash and manifest are the ones this package and manifest.json state.
func TestEmbedded(t *testing.T) {
	b := Artifact()
	if p := uintptr(unsafe.Pointer(unsafe.SliceData(b))); p%8 != 0 {
		t.Errorf("embedded artifact at %#x, not 8-byte aligned: artifact.Open would copy it", p)
	}
	a, err := artifact.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(); err != nil {
		t.Fatal(err)
	}
	if a.Month() != Month {
		t.Errorf("artifact month %s, Month %s", a.Month(), Month)
	}
	if h := a.Hash(); hex.EncodeToString(h[:]) != BLAKE3 {
		t.Errorf("artifact blake3 %x, BLAKE3 %s", h, BLAKE3)
	}

	raw, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m artifact.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if want := a.Manifest(); !reflect.DeepEqual(m, want) {
		t.Errorf("manifest.json describes %+v; the artifact is %+v", m, want)
	}
}
