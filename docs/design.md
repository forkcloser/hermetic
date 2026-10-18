# hermetic: the toolset's own aqua

The design delta for the machine toolset's resolver, decided 2026-10-14, before any
code. What is here describes the end state; the sections at the end say what moves,
in what order, and what the human does by hand.

## The problem

Every tool a machine runs through the toolset, `limen`, the ssh fork, `taciturn`,
resolves through this chain when typed in a shell:

1. PATH finds `<aqua root>/bin/<tool>`, a symlink aqua created to `../aqua-proxy`.
2. `<aqua root>/aqua-proxy` is a symlink to the upstream aqua-proxy binary aqua
   downloaded into its package store.
3. aqua-proxy reads its own `argv[0]` and execs the literal word `aqua` with
   `exec -- <tool> <args>`. The word is resolved through the caller's PATH, and the
   child inherits the caller's environment.
4. Whatever `aqua` that is decides the root from `AQUA_ROOT_DIR` or aqua's default,
   reads the config chain (the directory's `aqua.yaml` upward, then
   `AQUA_GLOBAL_CONFIG`), finds the pin, installs it if missing, and execs the real
   binary from the package store.

Steps 3 and 4 are decided by the shell's PATH and environment at the moment the
command is typed. That is why the installer wrote the toolset into aqua's default
root and into a global config of its own at `${XDG_CONFIG_HOME:-~/.config}/aqua/aqua.yaml`
(or the file a preset `AQUA_GLOBAL_CONFIG` named, rewritten whole on every run), and
exported `AQUA_GLOBAL_CONFIG` and `AQUA_POLICY_CONFIG` into the human's rc: nothing
else makes the shims land in the toolset. aqua has no default global config; the
variable is what names one. The cost is that the human's own aqua, for anything that
is not the toolset, shares the root and reads the toolset's config and policy on
every run.

Three facts, verified in the sources at the pinned versions, bound the fix:

- aqua-proxy's command is a constant, `aqua`, resolved through PATH; nothing in it
  reads a path, a variable or its own location.
- aqua's installer relinks `<root>/bin/<tool>` to `../aqua-proxy` and
  `<root>/aqua-proxy` to upstream's download whenever the targets differ, on every
  install. A replacement proxy under aqua's own paths is overwritten on the next
  run: a second writer, not a design.
- aqua's root, config file names, `AQUA_*` variables and the proxy's name are
  library constants. The program name the CLI is built with reaches its help text
  and nothing else.

## The binary

One binary, `hermetic`, built from aqua as a Go library: the library's
controllers behind a command line of our own. It is aqua, pinned by `go.mod` and
moved by Renovate like every Go dependency, with three things of ours around it.
The command line is kong, the logger slog through primordium's, and the exit
status primordium's shutdown, as every CLI of ours: aqua's own command layer,
urfave and its helpers, is not linked, since each of its commands is a few lines
of flag reading over a controller that takes a plain logger and a parameter
struct, and those lines are ours. The parameter struct is derived from the
environment the binary sets for the library (below) and from nothing else: the
library's settings file is never read, since every value it could supply is
pinned there.

**It exposes what the toolset uses, and nothing else.** The grammar names the
library's commands the toolset calls, each with the flags it keeps: `install`
(`-l`, `-a`, the tags), `update`, `update-checksum` (`--prune`, `-a`), `generate`,
`which` (`--show-version` for the package's version: `-v` is the binary's own version
on every command, so a `which -v` ported from aqua prints hermetic's version and
exits 0), `policy allow` and `policy deny`, `root-dir` and `version`, under two
global flags, `-c` and `--log-level`, and `--help`. The rest of aqua is not
reachable: `exec` (a tool run through the library would get the library's
environment, not the shell's; the links in the bin are how a tool runs),
`remove`, `info`, `list`, `vacuum`, `token`, `cp`, `init`, `init-policy`,
`policy init`, `generate-registry`, `update-aqua`, shell completion, the
`--disable-*` verifier switches, `--trace`, `--cpu-profile`, and the deprecated
`--test` and `--deep`. A command or a flag outside the grammar fails as unknown,
with the usage, as exit status 1; a library failure is logged and is exit status
1; a tool's own exit status on Windows, where the tool is a child, is carried
out as it is.

**It dispatches on `argv[0]`.** Invoked under its own name it is aqua, with the
surface above. Invoked under any other name, through a link, it runs that tool, in
two phases of its own: first the library resolves the name and installs it on
first use, as `which` does, under the library's environment (below); then the
mirror; then the tool itself, with the shell's environment minus every `AQUA_*`,
replacing the process on Unix, as a child whose exit code becomes ours on Windows.
No second binary and no upstream proxy: the links point at `hermetic` itself, at
one path. A symlink survives the binary's replacement; a hard link does not, which
Windows below handles.

**It controls the library's environment.** The library and the verifiers it runs
(cosign, slsa-verifier, `gh attestation verify`) see the shell's environment minus
what is scrubbed, plus ours, and nothing else. Scrubbed: every `AQUA_*`; everything
cosign reads that steers trust, `TUF_*`, `SIGSTORE_*`, `COSIGN_*`; the CI and
cloud identities it reads, `ACTIONS_ID_TOKEN_REQUEST_*`, `GITLAB_*`,
`BUILDKITE_*`, `SPIFFE_ENDPOINT_SOCKET`, `GOOGLE_SERVICE_ACCOUNT_NAME`,
`GITHUB_HOST`; and `GITHUB_TOKEN`. Ours: `AQUA_ROOT_DIR`, `AQUA_GLOBAL_CONFIG` and
`AQUA_POLICY_CONFIG` from the root, `TUF_ROOT` as `<root>/sigstore` so the verifiers'
trust cache lives in the root, and the five variables the binary exposes, the only
`HERMETIC_*` it reads:

- `HERMETIC_ROOT_DIR`: the root.
- `HERMETIC_CONFIG`: the project config, outright, like `-c`.
- `HERMETIC_NO_GLOBAL_CONFIG=true`: no toolset pins for this run.
- `HERMETIC_GITHUB_TOKEN`: the token, set as `AQUA_GITHUB_TOKEN` and `GITHUB_TOKEN`
  for the library and the verifiers; the shell's `GITHUB_TOKEN` never reaches them.
- `HERMETIC_LOG_LEVEL`: the library's log level.

Any other `HERMETIC_*` in the environment is refused, named, so a misspelling or a
knob the binary does not expose fails loud. Nothing switches the gate or a verifier
off: the library's `DISABLE_*` and checksum knobs have no counterpart. aqua reads
one more input, its settings file at
`${XDG_CONFIG_HOME:-~/.config}/aquaproj-aqua/config.yaml`, for the three paths and
for its remaining knobs, each applying when the matching variable is empty. The
binary leaves none empty: the three paths always, and the knobs pinned to the one
value the toolset runs with, the policy gate and the lazy install on, checksums
verified and required whatever a config says, the vacuum's use tracking off, no
progress bar, the library's defaults for parallelism, log level and colour. That
file never reaches the toolset and stays the human's. `HOME` stays the shell's:
slsa-verifier caches under it, and the recipes that point it elsewhere for a
session keep doing so.

The tool a dispatch runs is not the library: it gets the shell's environment back,
minus every `AQUA_*`, with its `COSIGN_*`, `SIGSTORE_*`, `GITHUB_TOKEN` and the rest
intact, so `cosign sign-blob` under the release lane and `gh` under a dispatched
`just` work as they do from the shell. Nothing of ours is added to it: the toolset
is reached through hermetic, and a stock aqua under the tool is the human's own.

**It keeps its own `bin`.** After the subcommands that install (`install`,
`update`) and after a dispatch's resolution, before the tool runs, it
mirrors the command names aqua exposed in `<root>/store/bin`, `aqua` itself
excepted, into `<root>/bin` as links to itself, adding and removing so the two sets
match, and comparing targets rather than names: a link whose target is not the
binary, a hard link's file identity included, is recreated. aqua's `bin` is never on
anyone's PATH; ours is. aqua may relink its own directory as it likes, since
nothing reads it but this mirror. (`aqua` is excepted: the toolset has no `aqua`
command, and a mirrored one would dispatch to whatever upstream binary sat under
that name.)

## The root

```
$HERMETIC_ROOT_DIR/            default: the platform's data directory for the name, as
                                     primordium's dirs resolves it: ${XDG_DATA_HOME:-~/.local/share}/hermetic
                                     on Linux, ~/Library/Application Support/hermetic on macOS,
                                     %LOCALAPPDATA%\hermetic on Windows
├── bin/
│   ├── hermetic               the binary; the one PATH entry a shell needs
│   ├── limen -> hermetic      the mirrored links (hard links on Windows)
│   ├── ssh -> hermetic
│   └── …
├── hermetic.yaml                    the global config: the toolset's pins (AQUA_GLOBAL_CONFIG)
├── aqua-checksums.json              its checksums, under the library's fixed name; written by
                                     `update-checksum -c <root>/hermetic.yaml` (or -a): the
                                     library's own finder never sees a config by that name
├── registry.yaml                    the farcloser registry
├── policy.yaml                      the policy (AQUA_POLICY_CONFIG)
└── store/                           the library's root (AQUA_ROOT_DIR): pkgs, bin, policies, …
```

What the binary names is ours (`hermetic.yaml`, `policy.yaml`, `store/`, `.hermetic/`
and its files); what the library names from a path it is given keeps aqua's
spelling: the `aqua-checksums.json` beside each config, the subdirectories of
`store/` and the `aqua-proxy` the library still downloads and links there on every
install, which nothing of ours runs.

The machine toolset lives in the default root. A Claude Code session has its own,
`~/.local/share/limen-agent/hermetic`, named by `HERMETIC_ROOT_DIR` in the
sessions' settings env where `AQUA_ROOT_DIR` is named today; the sandbox write list
follows. Baked defaults apply only when the variable is unset, so the sessions' root
stays theirs with the same binary.

A shell's own aqua, if it has one, is untouched: its root, its global config and
its policy are what aqua's defaults say, and nothing of ours is exported into the
shell beyond the PATH entry.

## The project directory

A project's pins live in `.hermetic/`, not `.aqua/`: `hermetic.yaml` (the pins, with
the `import: ../.limen/aqua.yaml` line as today, since an import is a path),
`policy.yaml`, and `aqua-checksums.json`, the one file that keeps aqua's name
because the library writes it beside the config under a fixed name.

aqua's own finder walks upward for `aqua.yaml`, `.aqua/aqua.yaml` and their
variants, library constants, but `AQUA_CONFIG` names one config file outright. So
`hermetic` does the walk itself, upward from the working directory for
`.hermetic/hermetic.yaml`, and sets `AQUA_CONFIG` to what it finds before the
library runs. `-c` and `HERMETIC_CONFIG` win over the walk, for the recipe
that runs on a scratch copy of the manifest.

The project's policy keeps its gate. Everything `AQUA_POLICY_CONFIG` names is
trusted on sight by the library; only a policy its own finder locates beside a
config is validated against the record `policy allow` wrote under
`<root>/store/policies/`, and dropped with a warning when the record is missing or
stale, so a cloned repository cannot allow a registry of its choosing and have the
lazy install run that registry's binary under a tool's name. Our walk replaces the
finder, so the binary does the validation itself with the library's exported
validator: `.hermetic/policy.yaml` joins `AQUA_POLICY_CONFIG` only when its record
under `<root>/store/policies/` matches, and is otherwise dropped with the same
warning. `hermetic policy allow .hermetic/policy.yaml` stays the one-time
gate per machine, in the setup action and the book under the new path. The one
policy trusted on sight is the toolset's own, in the root, as today, and it is
listed only while the root holds one: a root with no toolset in it, a runner's
or a fresh session's, has no policy of its own, and the library refuses a
listed file that is not there, so the list is of what exists.

Two consequences:

- Renovate's aqua preset detects pins by the names `.aqua/aqua.yaml` and
  `aqua.yaml`; with ours it detects nothing. The canonical `.limen/renovate.json`
  carries its own copy of the preset's regex managers, pointed at `.hermetic/`, and
  stops extending upstream's; upstream's later changes to those managers no longer
  follow us, and they are a few regexes.
- A stock aqua finds no pins in our repositories, which is the point and the cost:
  only `hermetic` resolves them.

`fix` renames a repository's `.aqua/` to `.hermetic/` once on its limen bump, the
way it moves `tools/` to `.limen/tools/` today; the `aqua` rule, `tools.just`, the
setup action's `policy allow`, the checksum workflow and the book move with it.

## The hermetic PATH in the recipes

limen's canonical `main.just` builds the recipes' PATH today from aqua's root:
`$AQUA_ROOT_DIR/bin` or aqua's default, then the base system. It builds it from
`$HERMETIC_ROOT_DIR/bin` or our default instead, and the recipes that call
`aqua` by name (`tools.just`, `lib.just`'s download prefix) call `hermetic`.
The project files are `.hermetic/`'s (above); the canonical `.limen/aqua.yaml` they
import keeps its name.

One recipe case needs a switch of ours. `tools.just` runs aqua on a project's own
pins alone, today by `env -u AQUA_GLOBAL_CONFIG`, which the binary's derivation would
undo. `HERMETIC_NO_GLOBAL_CONFIG=true` tells the binary to set
`AQUA_GLOBAL_CONFIG` to a value that names no file, a lone list separator, which
aqua's parser skips as empty entries; not to leave it empty, since an empty variable
lets the human's settings file's `global_config` fall through. The recipe sets it
where it unset the variable, and `HERMETIC_GITHUB_TOKEN` where it set
`AQUA_GITHUB_TOKEN`.

## CI

The canonical setup action installs `hermetic` on the runner instead of
upstream aqua: the release asset from forkcloser/hermetic at a pinned version, its
checksum pinned in the action beside the version, the pair bumped by hand in limen
as the action's aqua-installer pair is today (Renovate watches `AQUA_VERSION` in
the action, in limen alone, and no sha256: the installer pair and hermetic's move
by hand), and
`gh attestation verify` against hermetic's release workflow where `gh` exists
(every GitHub runner). Then `hermetic i` as before. The action's name follows
the tool; the content-pinned rule carries the rename to the fleet with a limen
release.

## Bootstrap and release

The binary is this repository, forkcloser/hermetic: `go.mod`, `cmd/hermetic`,
goreleaser through limen's canonical release lane, binaries for darwin/arm64,
linux/amd64, linux/arm64, windows/amd64 and windows/arm64, `checksums.txt`,
attestations on every asset. `release.yaml` is seeded once and never moved by a
limen bump, so the attestation step comes here by hand from limen's seed; v0.1.0,
cut before it, carries `checksums.txt` and its cosign bundle, and verification by
attestation starts with the first tag cut after the step. limen-install keeps the
scripts; the two repositories release on their own tags, and the scripts pin the
binary's.

`limen-install` fetches the binary from hermetic's release at the pinned
version, under a checksum pinned in the script, where aqua-installer and its
checksum are pinned today, and places it at `<root>/bin/hermetic`; the toolset's
config, registry and policy go beside it, and `hermetic i -a` installs the
toolset. Renovate moves the version in the script as it moves
`AQUA_INSTALLER_VERSION`, and the script's installer-checksum workflow recomputes
the sha256 beside it on Renovate's branch, as it does for aqua-installer today:
Renovate moves a version, never a digest. There is no
`aqua update-aqua` any more: the binary is ours, and only our release replaces it.
The nightly job runs the installer as it does today and takes the new binary with
it. `limen-install.ps1` does the same with the Windows asset, with one difference. The
links there are hard links, a second name for the file as it was linked, so
replacing the binary detaches every one of them; and a running dispatch holds the
file open, since the library runs the tool as a child and waits. The installer
replaces the binary by renaming the old one aside and writing the new, then
recreates every hard link, which is what aqua does for its own proxy; the mirror's
identity comparison catches any it missed.

Renovate: the aqua library through `go.mod` (our own cooldown rule applies to
nothing here; upstream's three days do); the `hermetic` version in the installer
script through the regex manager that watches aqua-installer's today, its sha256
recomputed by the installer-checksum workflow; the pair in the setup action by
hand in limen, as its aqua-installer pair is.

## Migration

For the human, once the installer carries this:

- Remove the three lines the old installer appended to the rc: the `# aqua` PATH
  entry, `export AQUA_GLOBAL_CONFIG=…`, `export AQUA_POLICY_CONFIG=…`. The new
  installer appends its one PATH line.
- Delete `~/.config/aqua/aqua.yaml` and its `registry.yaml` and `aqua-policy.yaml`
  beside it if their header says limen-install wrote them. A preset
  `AQUA_GLOBAL_CONFIG` of the human's names their own file again, and the settings
  file at `~/.config/aquaproj-aqua/config.yaml`, if they keep one, is theirs
  untouched.
- Leave `~/.local/share/aquaproj-aqua`: it is the human's root now, and the toolset's
  packages in it are dead weight until pruned, harmless.
- Run `limen-install-agent` once more: the sessions' settings env names
  `HERMETIC_ROOT_DIR` instead of `AQUA_ROOT_DIR`, and their root moves.

## Order of work

1. This delta.
2. forkcloser/hermetic: the module, the binary, the release lane. The scripts
   still install upstream aqua; CI still uses upstream's setup action.
3. limen-install: the scripts bootstrap `hermetic`, write the root, write the
   one rc line; `limen-install-agent` names the sessions' root.
4. limen: `main.just`, `tools.just`, `lib.just`, the setup action, the `aqua` rule
   with the `.aqua/` to `.hermetic/` migration, the Renovate managers, the book, as
   one canonical change, shipped by a release; the fleet follows on its bump.
5. The human's migration, by the list above.
