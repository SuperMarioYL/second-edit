# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-11-19

### Added

- `init`: reads a PNG and its Godot 4 scene, writes a re-runnable sidecar with
  the sprite pivot, pixels-per-unit, collision polygons, atlas region and named
  edit regions. Refuses a scene it cannot map (`NO_ENGINE_FACTS`) and a scene
  that is not Godot 4 (`UNKNOWN_ENGINE`).
- `apply`: composites a patch into one named region, refuses any changed pixel
  outside that region (`OUT_OF_REGION`), and writes the output PNG, an updated
  sidecar and a chained receipt only when every check passes.
- Complement hash: `untouched_sha` covers every pixel outside the region, with
  an optional feather.
- Receipt chain: edit N carries `parent_receipt` and `before_sha` from edit N-1;
  a tampered or forked chain is refused with `BROKEN_CHAIN`.
- Godot import checks as named violations: `PIVOT_DRIFT`, `SCALE_MISMATCH`,
  `COLLISION_OUT_OF_BOUNDS`, `ATLAS_REGION_SHIFT`.
- `check`: validates an existing asset independently of any edit and re-runs the
  chain verification.
- `--json` agent surface: schema v1 JSON on stdout, human output on stderr,
  exit codes 0 pass / 1 reject / 2 usage or contract error.
- Golden tests over a deterministic hero fixture, `make dist` static builds and
  a goreleaser release workflow.

[0.1.0]: https://github.com/SuperMarioYL/second-edit/releases/tag/v0.1.0
