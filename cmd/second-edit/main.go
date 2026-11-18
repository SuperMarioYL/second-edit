// Command second-edit turns "edit this generated sprite" into a transaction:
// one named region in, a verified pixel diff and an engine import check out.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuperMarioYL/second-edit/internal/contract"
	"github.com/SuperMarioYL/second-edit/internal/edit"
	"github.com/SuperMarioYL/second-edit/internal/engine/godot"
	"github.com/SuperMarioYL/second-edit/internal/receipt"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return contract.ExitFault
	}
	switch args[0] {
	case "init":
		return cmdInit(args[1:])
	case "apply":
		return cmdApply(args[1:])
	case "check":
		return cmdCheck(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("second-edit v%s\n", version)
		return contract.ExitPass
	case "help", "-h", "--help":
		usage(os.Stdout)
		return contract.ExitPass
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n", args[0])
		usage(os.Stderr)
		return contract.ExitFault
	}
}

func cmdInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	scene := fs.String("scene", "", "Godot 4 scene that owns the sprite")
	out := fs.String("out", "", "sidecar path (default: <png>.asset.json)")
	fs.StringVar(out, "o", "", "shorthand for --out")
	// The plan's surface is `init <sprite.png> --scene ...`: the positional
	// comes first, so pull it off before the flag package stops at it.
	var pngPath string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pngPath = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return contract.ExitFault
	}
	if fs.NArg() != 0 || pngPath == "" || *scene == "" {
		fmt.Fprintln(os.Stderr, "usage: second-edit init <sprite.png> --scene <scene.tscn> [-o contract.json]")
		return contract.ExitFault
	}

	c, cerr := godot.Init(pngPath, *scene)
	if cerr != nil {
		return reportFault(cerr)
	}
	outPath := *out
	if outPath == "" {
		outPath = strings.TrimSuffix(pngPath, filepath.Ext(pngPath)) + ".asset.json"
	}
	dir := filepath.Dir(outPath)
	c.Source.File = contract.RelativeRef(dir, pngPath)
	c.Scene = contract.RelativeRef(dir, *scene)
	if cerr := c.Save(outPath); cerr != nil {
		return reportFault(cerr)
	}
	fmt.Printf("wrote %s · engine %s · %d regions · source %s (%dx%d)\n",
		outPath, c.Engine, len(c.Regions), c.Source.File, c.Source.Width, c.Source.Height)
	return contract.ExitPass
}

func cmdApply(args []string) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asset := fs.String("asset", "", "asset contract produced by init")
	region := fs.String("region", "", "named region the patch may change")
	patch := fs.String("patch", "", "PNG the size of the region bounding box")
	out := fs.String("out", "", "output PNG path")
	feather := fs.Int("feather", 0, "pixels around the region excluded from the untouched hash")
	jsonOut := fs.Bool("json", false, "emit the receipt as JSON on stdout")
	if err := fs.Parse(args); err != nil {
		return contract.ExitFault
	}
	if fs.NArg() != 0 || *asset == "" || *region == "" || *patch == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: second-edit apply --asset <contract.json> --region <name> --patch <patch.png> --out <file.png> [--feather N] [--json]")
		return contract.ExitFault
	}

	res, cerr := edit.Apply(edit.Options{
		ContractPath: *asset,
		Region:       *region,
		PatchPath:    *patch,
		OutPath:      *out,
		Feather:      *feather,
	})
	if cerr != nil {
		if *jsonOut {
			emitRefusal(cerr, "REJECT")
		}
		return reportRefusal(cerr)
	}

	lines := receipt.Lines(res.Receipt)
	if *jsonOut {
		emitJSON(res.Receipt)
		for _, line := range lines {
			fmt.Fprintln(os.Stderr, line)
		}
		return contract.ExitPass
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return contract.ExitPass
}

func cmdCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asset := fs.String("asset", "", "asset contract to check")
	scene := fs.String("scene", "", "override the scene recorded in the contract")
	jsonOut := fs.Bool("json", false, "emit the check result as JSON on stdout")
	if err := fs.Parse(args); err != nil {
		return contract.ExitFault
	}
	if fs.NArg() != 0 || *asset == "" {
		fmt.Fprintln(os.Stderr, "usage: second-edit check --asset <contract.json> [--scene <scene.tscn>] [--json]")
		return contract.ExitFault
	}

	ins, cerr := edit.Inspect(*asset, *scene)
	if cerr != nil {
		return reportFault(cerr)
	}
	bad, failed := edit.Failed(ins.Checks, ins.Chain)

	if *jsonOut {
		emitCheck(ins, bad, failed)
	}
	if failed {
		fmt.Fprintf(os.Stderr, "REJECT %s: %s\n(exit %d)\n", bad.Code, bad.Detail, contract.ExitReject)
		return contract.ExitReject
	}
	// With --json the stdout stream is one JSON object and nothing else, so
	// the human lines move to stderr exactly as they do for apply.
	human := os.Stdout
	if *jsonOut {
		human = os.Stderr
	}
	fmt.Fprintf(human, "check PASS · %s\n", receipt.Summary(ins.Checks))
	fmt.Fprintf(human, "receipt chain ok (%d %s)\n", ins.Receipt, plural(ins.Receipt, "receipt", "receipts"))
	return contract.ExitPass
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func usage(w *os.File) {
	fmt.Fprintf(w, `second-edit v%s — region-locked sprite edits with Godot import checks

usage:
  second-edit init <sprite.png> --scene <scene.tscn> [-o contract.json]
  second-edit apply --asset <contract.json> --region <name> --patch <patch.png> --out <file.png> [--feather N] [--json]
  second-edit check --asset <contract.json> [--scene <scene.tscn>] [--json]
  second-edit version

exit codes: 0 pass · 1 reject · 2 usage or contract error
`, version)
}

// reportRefusal prints the two-line refusal used by every rejected edit.
func reportRefusal(e *contract.Error) int {
	if e.Exit == contract.ExitReject {
		fmt.Fprintf(os.Stderr, "REJECT %s: %s\nno file written (exit %d)\n", e.Code, e.Msg, e.Exit)
		return e.Exit
	}
	return reportFault(e)
}

func reportFault(e *contract.Error) int {
	fmt.Fprintf(os.Stderr, "error: %s: %s\n", e.Code, e.Msg)
	return e.Exit
}

func emitJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot encode JSON: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

type refusal struct {
	SchemaVersion int    `json:"schema_version"`
	Verdict       string `json:"verdict"`
	Code          string `json:"code"`
	Message       string `json:"message"`
}

func emitRefusal(e *contract.Error, verdict string) {
	emitJSON(refusal{SchemaVersion: contract.SchemaVersion, Verdict: verdict, Code: e.Code, Message: e.Msg})
}

type checkResult struct {
	SchemaVersion int              `json:"schema_version"`
	Verdict       string           `json:"verdict"`
	Code          string           `json:"code,omitempty"`
	Checks        []contract.Check `json:"checks"`
	Receipts      int              `json:"receipts"`
}

func emitCheck(ins *edit.Inspection, bad contract.Check, failed bool) {
	res := checkResult{
		SchemaVersion: contract.SchemaVersion,
		Verdict:       "PASS",
		Checks:        append(append([]contract.Check{}, ins.Checks...), ins.Chain...),
		Receipts:      ins.Receipt,
	}
	if failed {
		res.Verdict = "REJECT"
		res.Code = bad.Code
	}
	emitJSON(res)
}
