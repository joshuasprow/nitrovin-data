package embedded

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/joshuasprow/nitrovin-data/pkg/artifact"
)

// TestEmbedded: the embedded artifact opens and verifies, and its month,
// hash and manifest are the ones this package and manifest.json state.
func TestEmbedded(t *testing.T) {
	a, err := artifact.Open(Artifact())
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
