package godot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperMarioYL/second-edit/internal/contract"
)

const fixtureDir = "../../../testdata/hero"

func TestInitMatchesCommittedSidecar(t *testing.T) {
	c, cerr := Init(filepath.Join(fixtureDir, "hero.png"), filepath.Join(fixtureDir, "hero.tscn"))
	if cerr != nil {
		t.Fatalf("init: %v", cerr)
	}
	tmp := filepath.Join(t.TempDir(), "hero.asset.json")
	if cerr := c.Save(tmp); cerr != nil {
		t.Fatalf("save: %v", cerr)
	}
	got, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(fixtureDir, "hero.asset.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("init output drifted from the committed sidecar:\n%s", got)
	}
}

func TestInitIsRerunnable(t *testing.T) {
	a, cerr := Init(filepath.Join(fixtureDir, "hero.png"), filepath.Join(fixtureDir, "hero.tscn"))
	if cerr != nil {
		t.Fatal(cerr)
	}
	b, cerr := Init(filepath.Join(fixtureDir, "hero.png"), filepath.Join(fixtureDir, "hero.tscn"))
	if cerr != nil {
		t.Fatal(cerr)
	}
	pa := filepath.Join(t.TempDir(), "a.json")
	pb := filepath.Join(t.TempDir(), "b.json")
	if cerr := a.Save(pa); cerr != nil {
		t.Fatal(cerr)
	}
	if cerr := b.Save(pb); cerr != nil {
		t.Fatal(cerr)
	}
	da, err := os.ReadFile(pa)
	if err != nil {
		t.Fatal(err)
	}
	db, err := os.ReadFile(pb)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(da, db) {
		t.Fatal("two init runs produced different sidecars")
	}
	if a.Engine != contract.EngineGodot4 || len(a.Facts.Pivot) != 2 {
		t.Fatalf("sidecar facts are wrong: %+v", a.Facts)
	}
}

func TestInitRefusesUnmappableScene(t *testing.T) {
	_, cerr := Init(filepath.Join(fixtureDir, "hero.png"), filepath.Join(fixtureDir, "no_sprite.tscn"))
	if cerr == nil || cerr.Code != "NO_ENGINE_FACTS" {
		t.Fatalf("want NO_ENGINE_FACTS, got %v", cerr)
	}
	if cerr.Exit != contract.ExitFault {
		t.Fatalf("want exit 2, got %d", cerr.Exit)
	}
}

func TestInitRefusesUnknownEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hero.unity")
	if err := os.WriteFile(path, []byte("--- !u!1 &1\nGameObject:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, cerr := Init(filepath.Join(fixtureDir, "hero.png"), path)
	if cerr == nil || cerr.Code != "UNKNOWN_ENGINE" {
		t.Fatalf("want UNKNOWN_ENGINE, got %v", cerr)
	}
}

func TestValidateUnchangedAssetIsAllPass(t *testing.T) {
	c, cerr := contract.Load(filepath.Join(fixtureDir, "hero.asset.json"))
	if cerr != nil {
		t.Fatal(cerr)
	}
	sc, cerr := Parse(filepath.Join(fixtureDir, "hero.tscn"))
	if cerr != nil {
		t.Fatal(cerr)
	}
	checks := Validate(c.Facts, sc, c.Source.Width, c.Source.Height)
	if len(checks) != 4 {
		t.Fatalf("want 4 checks, got %d", len(checks))
	}
	for _, chk := range checks {
		if chk.Status != "ok" {
			t.Fatalf("unchanged asset must be all-PASS, %s is %s: %s", chk.Name, chk.Status, chk.Detail)
		}
	}
}

func TestValidateNamesEachViolation(t *testing.T) {
	cases := []struct {
		scene string
		check string
		code  string
	}{
		{"pivot_drift.tscn", "pivot", "PIVOT_DRIFT"},
		{"scale_mismatch.tscn", "scale", "SCALE_MISMATCH"},
		{"collision_out_of_bounds.tscn", "collision", "COLLISION_OUT_OF_BOUNDS"},
		{"atlas_region_shift.tscn", "atlas", "ATLAS_REGION_SHIFT"},
	}
	c, cerr := contract.Load(filepath.Join(fixtureDir, "hero.asset.json"))
	if cerr != nil {
		t.Fatal(cerr)
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			sc, cerr := Parse(filepath.Join(fixtureDir, tc.scene))
			if cerr != nil {
				t.Fatal(cerr)
			}
			checks := Validate(c.Facts, sc, c.Source.Width, c.Source.Height)
			found := false
			for _, chk := range checks {
				if chk.Name != tc.check {
					continue
				}
				if chk.Code != tc.code || chk.Status != "violated" {
					t.Fatalf("%s: want %s, got %s/%s", tc.check, tc.code, chk.Status, chk.Code)
				}
				found = true
			}
			if !found {
				t.Fatalf("check %q missing from %v", tc.check, checks)
			}
			others := 0
			for _, chk := range checks {
				if chk.Status == "violated" {
					others++
				}
			}
			if others != 1 {
				t.Fatalf("%s must name exactly one violation, got %d", tc.scene, others)
			}
		})
	}
}
