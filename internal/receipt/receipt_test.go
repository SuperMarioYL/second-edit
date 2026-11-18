package receipt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/SuperMarioYL/second-edit/internal/contract"
)

// TestReceiptKeySet locks schema v1. An agent that reads receipts parses this
// key set; changing it needs a schema_version bump, not an edit to the golden.
func TestReceiptKeySet(t *testing.T) {
	r := Receipt{
		SchemaVersion: contract.SchemaVersion,
		ID:            "0002",
		ParentReceipt: "0001",
		AssetID:       "2ffb0bbbbaa6",
		Region:        "helmet",
		BeforeSHA:     "aa",
		AfterSHA:      "bb",
		MaskAreaPx:    1792,
		ChangedPx:     476,
		ChangedPct:    5.2,
		UntouchedSHA:  "cc",
		Checks:        []contract.Check{{Name: "pivot", Status: "ok"}},
		Verdict:       "PASS",
		Output:        "hero.v2.png",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)

	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "receipt_keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("receipt key set changed:\n got %v\nwant %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("receipt key set changed:\n got %v\nwant %v", got, want)
		}
	}
}

func TestSummaryDescribesAtlasAsUnchanged(t *testing.T) {
	line := Summary([]contract.Check{
		{Name: "pivot", Status: "ok"},
		{Name: "atlas", Status: "ok"},
	})
	if line != "pivot ok · atlas unchanged" {
		t.Fatalf("got %q", line)
	}
}
