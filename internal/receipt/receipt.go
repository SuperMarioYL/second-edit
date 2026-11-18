// Package receipt owns the edit receipt: the stable JSON record that proves an
// edit changed only the region it named, and that chains to the edit before it.
package receipt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/SuperMarioYL/second-edit/internal/contract"
)

// Receipt is schema v1. Field order is the published key order.
type Receipt struct {
	SchemaVersion int              `json:"schema_version"`
	ID            string           `json:"id"`
	ParentReceipt string           `json:"parent_receipt"`
	AssetID       string           `json:"asset_id"`
	Region        string           `json:"region"`
	BeforeSHA     string           `json:"before_sha"`
	AfterSHA      string           `json:"after_sha"`
	MaskAreaPx    int              `json:"mask_area_px"`
	ChangedPx     int              `json:"changed_px"`
	ChangedPct    float64          `json:"changed_pct"`
	UntouchedSHA  string           `json:"untouched_sha"`
	Checks        []contract.Check `json:"checks"`
	Verdict       string           `json:"verdict"`
	Output        string           `json:"output"`
}

// Save writes one receipt as receipts/<id>.json.
func Save(dir string, r Receipt) (string, *contract.Error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", contract.Fault("RECEIPT_UNWRITABLE", "cannot create %s: %v", dir, err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", contract.Fault("RECEIPT_UNWRITABLE", "cannot encode receipt: %v", err)
	}
	path := filepath.Join(dir, r.ID+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", contract.Fault("RECEIPT_UNWRITABLE", "cannot write %s: %v", path, err)
	}
	return path, nil
}

// Next returns the next receipt id and the id of the current head.
func Next(dir string) (id, parent string, err *contract.Error) {
	ids, err := ids(dir)
	if err != nil {
		return "", "", err
	}
	if len(ids) == 0 {
		return "0001", "", nil
	}
	last := ids[len(ids)-1]
	n, convErr := strconv.Atoi(last)
	if convErr != nil || n >= 9999 {
		return "", "", contract.Fault("RECEIPT_UNWRITABLE", "receipt sequence %q cannot be extended", last)
	}
	return fmt.Sprintf("%04d", n+1), last, nil
}

// VerifyChain checks that every receipt continues the one before it and that
// the chain head is the file the contract currently points at. A tampered or
// forked chain is a rejection, never a repair.
func VerifyChain(dir, headSHA string) *contract.Error {
	ids, err := ids(dir)
	if err != nil {
		return err
	}
	prev := ""
	for _, id := range ids {
		r, err := read(filepath.Join(dir, id+".json"))
		if err != nil {
			return contract.Violation("BROKEN_CHAIN", "receipt %s is not readable: %s", id, err.Msg)
		}
		if r.ID != id {
			return contract.Violation("BROKEN_CHAIN", "receipt %s claims id %q", id, r.ID)
		}
		if prev != "" && r.BeforeSHA != prev {
			return contract.Violation("BROKEN_CHAIN", "receipt %s starts at %s but the previous receipt ended at %s", id, short(r.BeforeSHA), short(prev))
		}
		prev = r.AfterSHA
	}
	if prev != "" && prev != headSHA {
		return contract.Violation("BROKEN_CHAIN", "receipt head ends at %s but the source is %s", short(prev), short(headSHA))
	}
	return nil
}

// Count reports how many receipts a directory holds.
func Count(dir string) int {
	ids, err := ids(dir)
	if err != nil {
		return 0
	}
	return len(ids)
}

// Lines is the human form: four lines for a pass, two for a refusal.
func Lines(r Receipt) []string {
	return []string{
		fmt.Sprintf("changed %d px (%.1f%%) · all inside region %q", r.ChangedPx, r.ChangedPct, r.Region),
		fmt.Sprintf("untouched region sha256 %s… unchanged", short(r.UntouchedSHA)),
		Summary(r.Checks),
		fmt.Sprintf("wrote %s + receipts/%s.json (exit 0)", filepath.Base(r.Output), r.ID),
	}
}

// Summary renders checks as the single human line apply and check share.
func Summary(checks []contract.Check) string {
	parts := make([]string, 0, len(checks))
	for _, c := range checks {
		if c.Status == "ok" {
			if c.Name == "atlas" {
				parts = append(parts, "atlas unchanged")
			} else {
				parts = append(parts, c.Name+" ok")
			}
			continue
		}
		parts = append(parts, c.Name+" "+c.Code)
	}
	return strings.Join(parts, " · ")
}

func short(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

func read(path string) (Receipt, *contract.Error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Receipt{}, contract.Fault("RECEIPT_UNREADABLE", "cannot read %s: %v", path, err)
	}
	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return Receipt{}, contract.Fault("RECEIPT_INVALID", "%s is not a valid receipt: %v", path, err)
	}
	return r, nil
}

// ids lists receipt ids in order. A missing directory is an empty chain, which
// is normal for the first edit.
func ids(dir string) ([]string, *contract.Error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, contract.Fault("RECEIPT_UNREADABLE", "cannot list %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		base := strings.TrimSuffix(name, ".json")
		if len(base) != 4 {
			continue
		}
		if _, convErr := strconv.Atoi(base); convErr != nil {
			continue
		}
		out = append(out, base)
	}
	sort.Strings(out)
	return out, nil
}
