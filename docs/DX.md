# hermetic: user stories, requirements, DX

What hermetic is for, who uses it, and what using it feels like: what a user types,
sees and can rely on. How it is built is out of scope.

## The promise

A project declares every binary and archive it needs in one manifest. hermetic
provides exactly those, each pinned by digest to one version and authenticated
against the publisher the manifest names before the manifest records it. A name the
project declares always runs the binary hermetic verified, never what the host
happens to have. A checkout plus hermetic is the whole toolchain, on every machine
and in CI.

hermetic seals which binary runs and with what environment, not what a binary loads
from the operating system: every macOS binary links the system library, and a
dynamically linked Linux binary loads the host's C library. hermetic records what
each binary needs from the host and checks it, so a mismatch fails at install, by
name, not at run time.

Correct is the bar, by default: every property that protects is required unless the
manifest records, per package and with a reason, that the publisher does not provide
it; nothing is required that protects nothing. hermetic is a tool for anyone's
project, and assumes nothing about one organisation's conventions.

## What hermetic defends against

What enters a project from outside it:

- a release asset replaced after the fact, or altered by a mirror, CDN or proxy;
- a version tag moved onto other code;
- a release signed or built by someone other than the publisher the manifest names,
  including a repository renamed or re-registered under the same name;
- an update to an older version, or to one published minutes ago;
- a registry that changes its description of a package;
- a host tool standing in for the one the project pinned.

Not a publisher whose own release pipeline was compromised: a release built and
signed by its real workflow verifies, whatever it contains. The cooldown narrows
that window; an SBOM audit may catch it later.

Not the project itself. Running a project's commands runs its code with the user's
rights: it can download and run anything, write anywhere, rewrite the store, set any
variable. The manifest's rules on locations, `PATH` and the environment keep
hermetic's own statements true (a declared name runs the binary hermetic verified;
the listed directories are the ones its tools write); they are not a boundary
between the project and the user. Containment is a sandbox's job, and the manifest
tells the sandbox what to allow.

What hermetic adds to a project's reach is the reason for `allow`. A project's code
runs when the user chooses: a recipe, a script, `hermetic exec`. With names resolved
by directory, typing a name the user types everywhere would run what that
directory's manifest chose, without that choice: clone a repository to read it,
`cd` in, `git log`, and the repository's own `git`, signed by its own author, runs
with the user's ssh agent in reach. Signature checks cannot help, since the manifest
names the signer; and a variable the manifest sets for an allowed tool (one that
makes `git` load a configuration from the checkout) does the same. `allow` is the
consent that running the build already implies.

## Who uses it

| Persona | Wants |
|---|---|
| **Owner** | Add, update and remove dependencies; know each is what it claims to be. |
| **Contributor** | Clone, run the project's commands, get the right tools with no setup. |
| **CI** | The same tools, fast and cached; red on anything unverified. |
| **Agent** | A coding agent in a sandbox: the same tools, no prompt it cannot answer, no write outside where allowed. |
| **Auditor** | One file: every input, its origin, how it was authenticated, every protection given up. |
| **Bot** | An update bot (Renovate or the like): a bump as a reviewable manifest change. |
| **Administrator** | Constrain what any project may pull in on a machine or in an organisation; route downloads through mirrors. |

## Vocabulary

- **Manifest**: the project's committed, reviewed, self-contained list of what it
  depends on: everything needed to fetch, authenticate and update each package.
- **Package**: an entry. A **tool** provides executables; its source is a release on
  a forge or on the publisher's own HTTPS server. An **artifact** is fetched and
  verified but never run by hermetic (a corpus, an SDK, a source tarball); its source
  is a forge release asset or any HTTPS URL, and the project asks for its path.
- **Source**: what a package is, independent of how its bytes travel: a forge kind and
  repository (`github:acme/fmt`; `github:ghe.corp.example/acme/fmt`), or an HTTPS URL
  pattern.
- **Registry**: a catalogue of how to get and authenticate packages (asset patterns,
  version listings, evidence and identities), keyed by source, with no versions
  chosen. hermetic has its own lean registry format and also reads the
  [aqua registry](https://github.com/aquaproj/aqua-registry)'s; hermetic's own
  registry, an organisation's, and aqua's are all listed the same way.
- **Evidence**: a signature, attestation or provenance statement, with the identity
  it must come from. **Material**: the files it is made of, stored with the package.
- **Opt-out**: a protection given up for one package, with a reason (`opt-out:`).
  **Missing**: a record the publisher does not provide, with a reason (`missing:`).
  Kept apart so the first stands out.
- **Environment**: what a command run through hermetic sees: the project's tools
  first on `PATH`, then directories the project adds, then (unless the manifest says
  otherwise) the OS base directories; and the variables described under `exec`.
- **Links**: one directory first on the user's `PATH`, with a launcher per declared
  name that resolves it by the current directory's project.
- **Global manifest**: the user's own, same format, for tools wanted everywhere.
- **Allow**: the user's approval of everything a manifest decides about what runs and
  where it writes; without it, the project's names do not resolve through links.
- **Location**: a directory the manifest names for tools to write to; created on
  first use.
- **Store**: verified packages and their material, shared by all projects and
  processes, addressed by digest, read-only once written.
- **Policy**: rules outside any project that narrow what a manifest may do. Never
  widens anything; any number compose.
- **Host configuration**: the principal's own settings, one file under its root,
  never layered: the host's pass-list, mirrors, proxy, added certificate authority.

## Principles

1. **hermetic once per machine, one step per clone.** hermetic is installed once on a
   machine (or by a pinned CI step); after a clone, one `hermetic allow`, and nothing
   else.
2. **Correct by default, weaker only by name.** Every protection, on every package,
   where it protects; every record where it is useful. An opt-out or a missing record
   per package, with a reason. No global weakening switch; nothing required that
   serves nothing.
3. **Protection never silently degrades.** An update that loses evidence, provenance
   or a record the previous version had is refused unless the owner records why.
4. **The manifest is the whole truth.** A diff shows what changed: version, digest,
   source, identities, opt-outs, what runs and where it writes.
5. **The CLI writes it; a human can read and edit it.** hermetic refuses an edit that
   leaves an entry inconsistent.
6. **Every supported platform, at every change**, so an owner on macOS cannot break
   Linux CI or a Windows contributor.
7. **Verify where it means something**: everything when the manifest changes; the
   digest when it is used.
8. **Every error says what, why and what to do**, naming the package, the property,
   what was expected and what was found.
9. **Quiet when it works**: output only while downloading or verifying.
10. **Offline after the first fetch**: using and re-verifying need no network.
11. **Same for humans, CI and agents**: nothing waits for input; anything that could
    ask has a flag, and its absence fails with the flag to use.

## Verification

### What protects

| Property | Established by | Default |
|---|---|---|
| **Integrity** | Digest and size recorded per platform. | Always; no opt-out. |
| **Authenticity** | At least one piece of evidence that the named publisher released these bytes (below). | Required; opt-out `authenticity`. |
| **Provenance** | Built by a named workflow from a named repository at the release's tag. | Recorded when published, never silently lost; required only by policy. |
| **Freshness** | No downgrade; no version younger than the cooldown; the pinned release still exists unchanged. | Always on; each has a flag for the one time it is meant. |

Evidence for authenticity, strongest first, and what must be pinned for it to mean
anything:

| Evidence | Pinned |
|---|---|
| Build provenance (SLSA; GitHub artifact attestation) | Source: repository by name and numeric id, owner id, ref pattern (`refs/tags/v*`). Signer: the builder or workflow by repository and path, its ref as a pattern or any; hosted runners only. |
| Keyless signature (sigstore) | OIDC issuer; source and signer as for provenance, read from the certificate's separate fields; transparency-log inclusion. |
| Key signature (minisign, GPG) | Public key, or full fingerprint. |
| GitHub release attestation, immutable release | Repository by name and numeric id. Attests what was published under the tag, not how it was built. |

- **A checksum file is not evidence.** With the digest pinned, a checksum served
  beside the asset proves nothing its server could not forge. It is material when a
  signature covers it.
- **Everything offered is recorded and verified**, not only the strongest; losing any
  of it on update is refused (principle 3).
- **An identity has two parts, pinned apart: the source (whose code) and the signer
  (what built and signed it).** The signer defaults to a workflow in the source
  repository. A reusable workflow elsewhere (an organisation's release workflow, a
  SLSA builder), the usual way to the strongest provenance, is named as such; its
  commit is pinned only if the owner asks, so bumping it changes no identity.
- **Identities are patterns over stable fields**, so the next release from the same
  source and signer matches and a same-named different repository does not. A
  legitimate change of signer (renamed workflow, moved reusable workflow, rotated key)
  is accepted with `update --accept-signer` whenever the source repository, by id, is
  unchanged; it shows old and new.
- **An identity comes from a registry entry or from the owner (`--signer`)**, never
  from the release being checked. A source repository other than the entry's needs
  `--signer`.
- **Numeric ids are looked up from names at `add`.** A registry (aqua's carries none)
  or an owner names repositories; `add` asks the forge for their ids and shows the
  ids it pinned. That is trust on first use of the name, at the moment the owner
  chooses it; from then on the id holds against a rename or a re-registered name. A
  registry in hermetic's format may carry ids, and a lookup that disagrees fails
  `add`: the repository was renamed or re-registered since the registry was written.

### What is recorded

Not security properties; required because they are useful. A record a publisher does
not provide is noted under `missing:`, with a reason, never under `opt-out:`, which is
reserved for protections given up: an exception that costs nothing must not train
reviewers to skim the ones that cost protection. `verify --no-opt-outs` concerns
protections only.

| Record | Default |
|---|---|
| **SBOM**: the publisher's, as published, noting whether evidence covers it. | Required for tools (`missing: sbom`); recorded for artifacts when published. Never gates authenticity. `hermetic sbom` merges them: CycloneDX JSON, or SPDX JSON on request. |
| **Host requirements**: static, or minimum C library, or minimum OS version, per platform. | Always recorded; checked by `install`, `doctor` and on first use. |

### When verification runs

- **When the manifest changes** (`add`, `update`, and in CI `verify --changed <base>`
  for the touched entries): everything, every platform, from the sources.
- **On demand**: `verify --refetch`, from the sources; `verify --offline`, from stored
  material and a stored sigstore trust root.
- **On use** (`install`, first run, links): integrity and host requirements only. The
  manifest was reviewed and its digest binds the bytes; re-authenticating on every
  machine would tie every CI run to a forge's API and rate limits for nothing.

## Decisions

- **Hermetic's own format; each entry is the whole description**: source, asset
  pattern, what it provides, evidence with identities, records, and what was resolved
  (version, per-platform digest, size, host requirements). An unknown package is
  described in its entry; there is no separate description file.
- **Only manifest-changing commands write the manifest**, under a lock. `install`,
  `exec`, `verify` and links never do. An entry whose `version` differs from the one
  its digests were resolved for makes the manifest inconsistent, and an inconsistent
  manifest fails as a whole: every name it provides refuses to run, each refusal
  naming the one bad entry and the command that resolves it. Failing only that
  entry's names would look like the tool's own failure, which a script's fallback
  (`git log || echo none`) turns into silently wrong output. hermetic's own commands
  (`update --resolve`, `show`, `verify`) keep working, since they do not go through
  the manifest's names; a project that provides `git` is left with those, or the
  host's `git` by its full path.
- **Registries describe; the manifest decides.** A registry says how to get and
  authenticate a package; the manifest says which packages, at which versions. An
  organisation that fixes or adds a description in its registry reaches every
  project at its next `add` or `update`, with no commit to a shared baseline. Only
  manifest-changing commands consult a registry, and the entry they write stands
  alone, so installing, running, offline and air-gapped use never touch one. Four
  rules make `add` give the same result for the same project anywhere:
  - registries are listed in the manifest, each pinned by commit and digest, and
    nowhere else (not in user or machine configuration);
  - a package is named by its source (`acme/fmt`, a URL), or by an alias qualified by
    the registry that defines it (`corp:tc`); a bare name (`hermetic add jq`) fails
    with the candidates, and `hermetic search` lists them;
  - no registry takes precedence by order: two listed registries that describe the
    same source differently make `add` fail, showing both, until `--from <registry>`
    chooses;
  - every entry records the registry and commit its description came from
    (`imported:`), and `add` shows it beside the identity it imported.

  Moving a registry's pin (`registry update`, or a bot) changes no entry by itself;
  `update` re-imports an entry whose description changed, or no longer fits a new
  release (renamed assets, new evidence), and shows the difference, identities
  included. A package no registry describes is described in its own entry.
- **A source is a kind and an identity, not a transport.** `github:acme/fmt` names a
  repository on a forge whose API hermetic speaks: it lists versions, serves release
  assets, answers attestation lookups by digest, says whether a release is immutable,
  and gives the repository's numeric id that identities pin. The host defaults to the
  forge's public one (`github.com`) and is named otherwise
  (`github:ghe.corp.example/acme/fmt`, for GitHub Enterprise). Other forges are other
  kinds (`gitlab:`, `codeberg:`, …), each supported for what its API offers: a kind
  with no attestations has none to require. A plain HTTPS source is a URL pattern and
  nothing more. Because a source is not a URL, a mirror rewriting where the bytes come
  from changes no source and asks for no new `allow`; and because hermetic never
  clones, git transports (`ssh`, `git+https`) are not sources.
- **Tools come from releases, on a forge or the publisher's server; hermetic builds
  nothing.** An entry keeps apart where versions are listed (a forge source's
  releases by default; otherwise a repository's tags or the publisher's index), where
  assets come from (release assets or a URL pattern), and where material comes from.
  Versions are limited to letters, digits and `.+-_`, since they are spliced into
  URLs. To build something, fetch its source as an artifact.
- **Platforms name the C library where it matters**: `linux/amd64` is glibc,
  `linux/amd64-musl` its own platform; a static binary serves both.
- **Names resolve by directory, through links.** A launcher for every name the global
  manifest or an allowed project declares; it runs the current directory's project's
  pinned binary, else the global manifest's, else the next on `PATH`. From a shell,
  an editor, a script or an agent alike; no shell hook. On Windows (no shebang, no
  `execv`) a launcher is a hard link to hermetic's own executable under the tool's
  name (a copy where the store is on another volume), running the tool as a child
  and forwarding its exit code. A
  launcher's warm call touches no network and parses no manifest: it finds the
  project with a few `stat` calls and runs what `allow` and `install` prepared. Under
  `exec`, a tool with no `env` of its own sits directly in the sealed `PATH`, with no
  launcher in between.
- **A manifest can include others, from the same repository only.** An include is a
  manifest file in the checkout, named by a relative path that stays inside it. A
  shared baseline reaches a project as a committed file its scaffolding writes and
  keeps current (`.hermetic/base.yaml`), so its every change is a diff in the
  project's own history, and a checkout stays the whole, self-contained toolchain,
  air gap included. An include fetched from elsewhere would break both. The effective
  manifest is the union of the including manifest and its includes (packages,
  locations, `env`, `path`, `pass-env`), under four rules:
  - a name provided twice on the same platform is an error, unless the including
    manifest's entry says `overrides: <include>`, so an override is deliberate and
    visible (packages providing a name on disjoint platforms do not collide);
  - `exclude: [<name>…]` on an include drops what the project does not want;
  - the including manifest's platforms govern: an included entry not resolved for
    one of them is an error naming the include and the platform;
  - the strictest `requires` of the manifest and its includes applies.

  Includes may nest; a cycle is an error. Commands that write (`add`, `update`,
  `opt-out` and kin) write the including manifest, or the file `--manifest` names:
  an included entry is updated in its own file, or overridden here. `list` and `show`
  say which file each entry comes from. `allow` approves the effective manifest, so a
  baseline change that adds a name, an identity or a variable asks again.
- **A directory's project is the nearest manifest, within its repository.** The search
  goes up from the current directory and stops at the repository boundary (a `.git`
  file or directory): a worktree or a repository cloned inside another is always its
  own project, and one without a manifest has none, so its names resolve to the
  global manifest and then the host, never to the enclosing repository's. Nothing is
  inherited implicitly: a package of a monorepo that wants the repository's tools
  includes the repository's manifest.
- **Running sealed is `hermetic exec`.** `PATH` is the project's tools, its added
  directories and (unless the manifest says otherwise) the OS base directories:
  `/usr/bin`, `/bin`, `/usr/sbin`, `/sbin` on macOS and Linux; on Windows
  `%SystemRoot%\System32`, `%SystemRoot%`, `System32\Wbem` and
  `System32\WindowsPowerShell\v1.0`, and nothing else. Windows has no `bash` there: a
  project whose recipes need one declares it, from Git for Windows, for its Windows
  platforms, never falling back to whatever bash the host has. The
  caller's environment carries two things: knobs that change what a build does
  (`GOFLAGS`, `NODE_OPTIONS`, `LD_LIBRARY_PATH`), which sealing keeps out, and the host
  describing itself to the build (that it is CI, its OIDC token endpoint, credentials,
  where an agent's git configuration lives), which the build needs and only the host
  knows. So the environment is built, not inherited, from:
  - what a process needs on its OS, built in: `HOME`, `USER`, `TERM`, `LANG`/`LC_*`,
    `TZ`, proxy variables; on Windows `SYSTEMROOT`, `PATHEXT`, `COMSPEC`, `TEMP`,
    `USERPROFILE` and kin;
  - the host's pass-list, from the user's or machine's configuration (patterns such as
    `RUNNER_*` allowed), plus built-in profiles hermetic switches on by detection:
    on GitHub Actions, the runner's documented variables (`CI`, `GITHUB_*`,
    `RUNNER_*`, `ACTIONS_ID_TOKEN_REQUEST_*`). This is the caller's own standing
    `--pass-env`, owned by whoever runs hermetic, never by a project; a policy may
    still forbid names in it;
  - the project's `pass-env`, for what the project itself needs (a release recipe's
    `GITHUB_TOKEN`), and the manifest's `env`.

  Nothing else passes; `hermetic exec --explain` lists each variable and the layer that
  let it in. The global manifest is invisible.
- **A typed name runs sealed too, unless its entry says why not.** Through a link, a
  declared tool runs in the same sealed environment as under `exec`, so a command
  typed in a shell behaves as it does in a recipe, and a task runner typed by name
  (`just lint`) seals every recipe it runs, shebang recipes included, for one hermetic
  start. A tool that works for the user rather than the build, starting the user's
  editor, pager, browser or credential helpers (`git`, `gh`, `docker`, an editor),
  says `unsealed: <why>` and runs in the user's environment with the manifest's `env`
  on top. A script started directly (`./build.sh`) is sealed only as
  `hermetic exec ./build.sh`. The links directory being first on `PATH` is the user's
  or the harness's setup, which `doctor` checks.
- **A global manifest holds the user's own tools**, managed with `--global`, from the
  same store; invisible to `exec`, so no build depends on them.
- **hermetic is installed per machine, never fetched by a project.** An installer on
  a workstation; in CI, a setup step the workflow pins by digest; one hermetic per
  root. A project never chooses the binary that verifies it, so a job with write
  access runs a hermetic the branch does not control, and reads the manifest only as
  data. The manifest's `schema` names its format: a hermetic reads every schema up to
  its own and refuses a newer one, naming the release that reads it; a format break
  bumps `schema`, and `hermetic migrate` rewrites the file. A project relying on
  behaviour newer than its schema implies states `requires: { hermetic: ">= <version>" }`,
  which an older hermetic refuses with that message.
- **Tools write where the project says.** Locations live under `{project}` (the
  checkout), `{state}` (the project's directory outside it, kept across `git clean`,
  shared by its worktrees) or `{shared}` (every project's on the machine, for caches
  filled once). Anywhere else (`{home}`, an absolute path) is an opt-out with its
  reason, forbiddable by policy. Environment variables, for every command or one
  tool, refer to locations and packages. `hermetic locations` lists every directory
  tools write, for a sandbox.
- **A declared name always runs the verified binary.** Added `PATH` directories come
  after the tools, never before: a `{project}/bin` ahead of them, holding a stray
  debug build of `fmt`, would run while `list` says `fmt v1.5.0` verified. Variables
  that make a binary load code from elsewhere (`LD_PRELOAD`, `DYLD_INSERT_LIBRARIES`,
  `NODE_OPTIONS`, `PYTHONPATH`, `BASH_ENV`, `GIT_CONFIG_*` and kin) cannot all be
  listed, so they are not forbidden: `allow` and `show` mark them, and a policy can
  forbid the known ones.
- **A project's names resolve only once allowed.** `allow` approves everything the
  manifest decides about what runs and where it writes: names with source and
  identity, variables, `PATH` additions, locations. Until then its links fall through
  as if it declared nothing, saying so once on standard error, or fail with
  `fallthrough: never` (for agents, or by policy). Approval is bound to a place and to
  what the manifest decides there. The place is the repository's git directory, which
  its worktrees share, so a worktree of an allowed clone is covered and another clone
  asks once; outside a repository it is the project's directory. Content alone cannot
  be the key: a manifest that points at files in the checkout (a variable naming
  `{project}/hook.js`, a `PATH` entry) leaves part of its effect to those files, and a
  hostile repository could copy an allowed manifest word for word beside files of its
  own. Nor can the repository's remote URL, which any checkout can write. A newer
  version from the same source and identity needs nothing; an older version, or any
  new name, source, identity, variable, `PATH` entry or location asks again. An
  allowed repository's own files changing on a pull are trusted as running its build
  after a pull is. `exec` needs no approval; the global manifest is the user's own.
  When `allow` cannot write a launcher (a read-only links directory, a shared
  install), it keeps the approval, names each launcher it could not create and why,
  and exits non-zero; `doctor` reports the same. Never a silent half-done state.
- **One root per principal.** Everything hermetic reads or writes (global manifest,
  host configuration, policy, approvals, project state, shared locations, store,
  links) lives under one root, `HERMETIC_HOME`, defaulting to the usual per-user
  directories when unset. Two principals on one machine (a human and an agent, two CI
  jobs) set different roots and get separate approvals and global tools: an approval
  is one principal's consent, and an agent does not run on the human's. `{state}` and
  `{shared}` follow the root.
- **Policy narrows, from outside.** Sources, registries, opt-outs, locations outside the managed
  roots, code-loading variables, required provenance, cooldown length. Every
  applicable policy narrows and none replaces another, so a variable naming one more
  policy file can only tighten. A policy file that exists but cannot be read is an
  error, never an absence: treating it as absent would lift every restriction it
  holds.
- **Mirrors are routing, and routing is the host configuration's.** The host
  configuration rewrites or blocks URLs for assets, version listings and material,
  so an organisation routes everything through its mirror, proxy or air-gapped copy
  without touching a manifest or re-allowing anything; it may add a certificate
  authority; `HTTPS_PROXY`/`NO_PROXY` are honoured. A mirror cannot forge bytes or
  evidence, both checked against what the manifest pins, but it is trusted with
  metadata: which versions exist and when they were released, so a hostile one could
  hide a release or fake its age. Routing therefore never lives in a policy, which
  any extra file may add to: one file, owned by the principal, not layered.
- **A new version waits.** `outdated` and `update` skip releases younger than the
  cooldown; naming a version or `--no-cooldown` takes it anyway and says so. The
  manifest sets a default (three days if unset) and overrides by source, so a
  project takes its own organisation's releases at once in one line
  (`"github:farcloser/*": 0`). A policy may set a minimum, with its own exceptions;
  an override below it is refused, naming the policy. A downgrade needs
  `--allow-downgrade`.
- **A new requirement applies at the next change.** Existing entries keep working;
  `verify` lists those lacking it; their next `update` needs it or an opt-out.
- **The store is safe under any concurrency, and keeps what is in use.** A digest is
  downloaded once while others wait; unrelated digests never wait on each other; an
  interrupted download leaves nothing; entries never change. A store restored from
  elsewhere (a CI cache) is re-hashed before use (cost on large caches and Windows
  runners to be reassessed at implementation). `store prune` spares entries a
  manifest used on the machine within its window still pins, and entries a running
  hermetic process holds. The implementation is primordium's content-addressed store.
  A store path is an address, not an identity: anything running as the user can
  write the store, so nothing should be authorized by a binary's path. A consumer
  that must authorize one (a signing agent granting a key to `ssh`) grants it by the
  digest `hermetic show` reports, or by code signature, and renews the grant when the
  package is bumped.
- **hermetic reads a forge with its own credential.** One variable per forge host
  (`HERMETIC_GITHUB_TOKEN`; a GitHub Enterprise host has its own), read-only scope
  sufficing; never the ambient `GITHUB_TOKEN` or `GH_TOKEN`, which in CI often carry
  write scopes meant for something else. Without one, hermetic works until the
  forge's rate limit, and the error names the variable to set.
- **Nothing in the environment steers verification.** Trust roots and verification
  settings come only from hermetic and its root; `TUF_*`, `SIGSTORE_*`, `COSIGN_*`
  and their kin are ignored, since honouring them would let any process that sets
  the environment redirect verification to a root it controls. A `HERMETIC_*`
  variable hermetic does not know is refused by name, so a misspelling fails loudly.
- **Transport has a floor.** TLS 1.3, with post-quantum hybrid key exchange,
  required. A source whose host offers less is a protection opt-out on that package
  (`opt-out: { tls-pq: <why> }`); a mirror declares the same in the host
  configuration. The error names the host and what it offered.
- **Offline is a mode.** `--offline` (or `HERMETIC_OFFLINE`) turns any network access
  into an error naming what was needed. `vendor <dir>` writes every package, its
  material and the trust root for every platform; `install --from <dir>` reads them.
- **Auditing is a separate tool** ([below](#a-separate-tool-sbom-audit)); hermetic's
  part ends at the SBOM.

## The manifest

`.hermetic/hermetic.yaml`, with a schema version. At the top, project-wide settings; then one entry per package, holding
everything about it. A version bump touches only its entry, in a stable order;
comments survive the CLI rewriting the file. YAML here is illustrative: the content
is settled, not the syntax. Every package, identity and number is invented.

```yaml
schema: 1
requires: { hermetic: ">= v0.12.0" }   # optional: a behaviour newer than schema 1 implies
platforms: [darwin/arm64, linux/amd64, linux/arm64, windows/amd64]
registries:     # consulted by add and update only; never by install or run
  aqua: { source: github:aquaproj/aqua-registry, commit: 3c9f0e5b1d2a…, digest: sha256:aa41… }
  corp: { source: github:acme/hermetic-registry, commit: 7e21d0c4…, digest: sha256:0f3b… }
system-path: true   # OS base directories, after the project's tools

include:
  - path: .hermetic/base.yaml        # the organisation's baseline, written by its scaffolding
    exclude: [lint-legacy]
  - path: .hermetic/release-tools.yaml

locations:
  tmp:       "{project}/.build/tmp"
  cache:     "{state}/cache"
  tc-shared: "{shared}/tc-{version:toolchain}"
  ide-cache:
    path: "{home}/.cache/acme-ide"
    outside: the editor plugin reads this cache; a second copy would double it on disk
path: ["{project}/.build/bin"]       # after the tools, before the OS directories
env: { TMPDIR: "{location:tmp}" }    # for every command
pass-env: [GITHUB_TOKEN]   # what this project needs from the caller; the host's own variables are the host's list
cooldown: { default: 3d, "github:acme/*": 0 }   # the organisation's own releases at once

tools:
  # Build provenance and a keyless signature, both pinned: the shape to aim for.
  # The baseline also defines fmt; this project pins its own, deliberately.
  fmt:
    overrides: .hermetic/base.yaml
    imported: aqua@3c9f0e5b1d2a
    source: github:acme/fmt
    version: v1.4.2
    asset: fmt_{version}_{os}_{arch}.{ext}
    ext: { default: tar.gz, windows: zip }
    provides: [fmt]
    authenticity:
      github-attestation:
        source: { repository: acme/fmt, id: 81234567, owner-id: 9876543, ref: refs/tags/v* }
        signer: { workflow: acme/fmt/.github/workflows/release.yaml, runners: hosted }
      sigstore:
        material: [checksums.txt, checksums.txt.sigstore.json]
        issuer: https://token.actions.githubusercontent.com
        source: { repository: acme/fmt, id: 81234567, owner-id: 9876543, ref: refs/tags/v* }
        signer: { workflow: acme/fmt/.github/workflows/release.yaml }
        tlog: required
    sbom: fmt_{version}_sbom.cdx.json
    resolved:
      version: v1.4.2
      darwin/arm64:  { digest: sha256:4be1c0…, size: 5242880, host: macos>=12 }
      linux/amd64:   { digest: sha256:0c9d7a…, size: 5361664, host: static }
      linux/arm64:   { digest: sha256:77a2e4…, size: 5103616, host: static }
      windows/amd64: { digest: sha256:e3108b…, size: 5498880, host: windows>=10 }

  # An immutable release and nothing else: authentic as published; no SBOM.
  query:
    source: github:acme/query
    version: v0.9.0
    asset: query-{os}-{arch}{exe}
    provides: [query]
    authenticity:
      release-attestation:
        repository: { name: acme/query, id: 55501234, owner-id: 9876543 }
    missing:
      sbom: not published; requested at https://github.com/acme/query/issues/121
    resolved:
      version: v0.9.0
      darwin/arm64:  { digest: sha256:51f0aa…, size: 2097152, host: macos>=11 }
      linux/amd64:   { digest: sha256:a91c3e…, size: 2162688, host: glibc>=2.31 }
      linux/arm64:   { digest: sha256:2d77b0…, size: 2031616, host: glibc>=2.31 }
      windows/amd64: { digest: sha256:c4e912…, size: 2228224, host: windows>=10 }

  # Several executables, a missing platform, its own environment, a key signature
  # and SLSA provenance bound to its source and tag.
  toolchain:
    imported: corp@7e21d0c4   # added as corp:tc
    source: github:acme/toolchain
    version: v2.1.0
    asset: toolchain-{version}.{os}-{arch}.tar.gz
    provides: { tc: toolchain/bin/tc, tcfmt: toolchain/bin/tcfmt }
    env:   # only when tc or tcfmt runs
      TC_CACHE: "{location:tc-shared}"
      TC_CONFIG_HOME: "{location:cache}/tc-config"
      TC_ENGINE_SRC: "{path:engine-src}"
    platforms:
      exclude: { windows/amd64: no Windows build upstream; Windows contributors use WSL }
    authenticity:
      slsa:
        material: [multiple.intoto.jsonl]
        source: { repository: acme/toolchain, id: 61230987, ref: refs/tags/v* }
        signer: { workflow: slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml, ref: refs/tags/v* }
      minisign:
        material: [toolchain-{version}.{os}-{arch}.tar.gz.minisig]
        key: RWQ<acme toolchain release key>
    sbom: toolchain-{version}.spdx.json
    resolved:
      version: v2.1.0
      darwin/arm64: { digest: sha256:9e0c21…, size: 104857600, host: macos>=13 }
      linux/amd64:  { digest: sha256:6b43d8…, size: 110100480, host: glibc>=2.28 }
      linux/arm64:  { digest: sha256:f18a07…, size: 106954752, host: glibc>=2.28 }

  # Served from the publisher's site, tagged on GitHub, unsigned: only the digest
  # vouches for it, and the manifest says why.
  lang:
    source: https://dl.acme.example/lang/lang{version}.{os}-{arch}.{ext}
    versions: { github-tags: acme/lang, prefix: lang }
    version: 1.31.2
    ext: { default: tar.gz, windows: zip }
    provides: { lang: lang/bin/lang, langfmt: lang/bin/langfmt }
    opt-out:
      authenticity: not signed, no provenance; the checksum index is served by the same host
    missing:
      sbom: not published
    resolved:
      version: 1.31.2
      darwin/arm64:  { digest: sha256:1d84f2…, size: 73400320, host: macos>=12 }
      linux/amd64:   { digest: sha256:b20e6c…, size: 75497472, host: static }
      linux/arm64:   { digest: sha256:5fa391…, size: 71303168, host: static }
      windows/amd64: { digest: sha256:9c07ad…, size: 77594624, host: windows>=10 }

  # An irregular release: its own spelling of the OS, another layout on Windows.
  hub:
    source: github:acme/hub
    version: v2.62.0
    names: { os: { darwin: macOS } }
    asset: hub_{version}_{os}_{arch}.{ext}
    ext: { default: tar.gz, windows: zip }
    provides: { hub: "hub_{version}_{os}_{arch}/bin/hub" }
    overrides:
      windows: { provides: { hub: bin/hub.exe } }
    # authenticity, sbom and resolved as for fmt

  # The same names from two packages, on disjoint platforms.
  coreutils:
    source: github:acme/coreutils
    platforms: { only: [darwin/arm64, linux/amd64, linux/arm64] }
    provides: [cat, ls, sort, tar]
    # …
  git-windows:
    source: github:acme/git-for-windows
    platforms: { only: [windows/amd64] }
    provides: { cat: usr/bin/cat.exe, ls: usr/bin/ls.exe, sort: usr/bin/sort.exe, tar: usr/bin/tar.exe, bash: bin/bash.exe }
    # …

artifacts:
  engine-src:   # source to build, signed by a release manager
    source: github:ghe.corp.example/acme/engine   # GitHub Enterprise: the host is named
    version: v5.0.1
    asset: engine-{version}.src.tar.xz
    extract: false
    authenticity:
      gpg: { material: [engine-{version}.src.tar.xz.asc], fingerprint: <40-hex fingerprint> }
    resolved:
      version: v5.0.1
      any: { digest: sha256:83c1f9…, size: 31457280 }

  corpus:       # data from a plain HTTPS server, signed directly
    source: https://downloads.example.org/corpus/corpus-2026-10.tar.zst
    extract: true
    authenticity:
      minisign:
        material: [https://downloads.example.org/corpus/corpus-2026-10.tar.zst.minisig]
        key: RWT<example.org corpus key>
    resolved:
      any: { digest: sha256:2a6e50…, size: 524288000 }

  flac-conformance:   # one folder of a repository at a commit that has no release
    source: github:acme/flac-test-files@4f0c9a2e7b1d…   # 40 hex
    path: subset
    authenticity:
      commit: { reachable-from: [refs/heads/main] }   # or signed: <identity>, if the repository signs
    resolved:
      any: { tree: git:9d1e44c0…, digest: sha256:71b3a8…, size: 268435456 }
```

Notes:

- A bump changes `version` and `resolved`; a `version` that differs from
  `resolved.version` marks an unresolved hand edit, and nothing runs until resolved.
- `source: github:acme/fmt` is a forge and a repository, not a URL: it tells hermetic
  which API lists versions and attestations and which repository id to pin. Where the
  bytes are actually downloaded from is the host configuration's mirror rule, which
  changes no source.
- No checksum file is a check: `fmt` lists `checksums.txt` as material its signature
  covers; `lang`'s index is evidence of nothing.
- `slsa` pins the source and the tag, not only the builder: the generic builder is
  public, and alone would accept anyone's provenance.
- `query`'s release attestation shows what was published, not how it was built;
  `show` says so. Artifacts carry no SBOM requirement.
- Placeholders in asset names, URLs, material and `provides`: `{version}`, `{os}`,
  `{arch}`, `{ext}`, `{exe}` (`.exe` on Windows, empty elsewhere). `names` maps
  hermetic's spelling of an OS or architecture to the release's (`darwin: macOS`,
  `amd64: x86_64`); `overrides` replaces `asset`, `provides` or `ext` for an OS or a
  platform.
- An entry describes its current version only. When a new release no longer fits,
  `update` re-reads the registry, which may hold version-specific descriptions; the
  manifest keeps no history of them.
- A name may come from several packages whose platforms do not overlap
  (`coreutils` and `git-windows`); `platforms: { only: […] }` says which. On a
  platform where two packages provide it, it is an error.
- References in locations, `path` and `env`: `{project}`, `{state}`, `{shared}`,
  `{home}`, `{location:<name>}`, `{path:<package>}`, `{version:<package>}`; an unknown
  name is an error. A path there must lie in a location or a package; `{home}` and
  absolute paths only in a location with an `outside` reason.
- `{shared}` locations are shared by name across projects: safe only for a tool whose
  cache tolerates concurrent use and other versions of itself (`{version:<package>}`
  in the name keeps versions apart). One project's tools write what another's read;
  a policy that objects forbids `{shared}`.
- A tool's `env` applies whenever it runs (`exec`, a link, another tool), on top of
  the top-level `env`.
- A tool runs sealed whether typed or started by a recipe. `unsealed: <why>` is for a
  tool that serves the user rather than the build (`git` opening the user's editor
  and pager); `list` shows it.
- `provides` is a list when executable names match, a map when they live at paths in
  the archive; paths are relative and stay inside it.
- Every opt-out is a sentence a reviewer can judge.

## Scenarios

Output is illustrative; the content is the requirement.

### Start a project

```
$ hermetic init --platforms darwin/arm64,linux/amd64,linux/arm64,windows/amd64
created .hermetic/hermetic.yaml   (schema 1, aqua registry @3c9f0e5b1d2a)

# on a machine whose hermetic is older than the manifest
$ hermetic install
error: .hermetic/hermetic.yaml is schema 2; this hermetic (v0.11.4) reads up to schema 1
  upgrade hermetic to v0.13.0 or later
```

A justfile needs nothing hermetic-specific: `just` typed by name runs sealed, and so
does every recipe it starts.

### Start from a shared baseline

```
# the organisation's scaffolding has written .hermetic/base.yaml
$ hermetic init --include .hermetic/base.yaml
created .hermetic/hermetic.yaml   including .hermetic/base.yaml (11 packages)

$ hermetic add acme/fmt
error: fmt is already defined by .hermetic/base.yaml (v1.3.0)
  to pin this project's own:  hermetic add acme/fmt --override

$ hermetic update shellcheck
error: shellcheck is defined by .hermetic/base.yaml; update it there
  (hermetic update shellcheck --manifest .hermetic/base.yaml),
  or override it here:  hermetic update shellcheck --override

$ hermetic include add ../shared/hermetic.yaml
error: ../shared/hermetic.yaml is outside this repository; an include must be a file in the checkout
```

When the scaffolding refreshes `.hermetic/base.yaml`, the change is an ordinary diff
in the project, reviewed as such.

### Add tools

```
$ hermetic add jq
error: jq is not a source; name the one you mean
  jqlang/jq    described by aqua@3c9f0e5b1d2a
  corp:jq      alias in corp@7e21d0c4 for github:acme/jq-static
  (hermetic search jq lists every match)

$ hermetic add corp:tc
found github:acme/toolchain in registry corp@7e21d0c4 (alias tc)
…

$ hermetic add acme/gitleaks
error: acme/gitleaks is described differently by two registries
  aqua@3c9f0e5b1d2a   asset gitleaks_{version}_{os}_{arch}.tar.gz   sigstore …/release.yml@refs/tags/v*
  corp@7e21d0c4       asset gitleaks_{version}_{os}_{arch}.tar.gz   sigstore …/release.yml@refs/tags/v*, slsa
choose one:  hermetic add acme/gitleaks --from corp

$ hermetic add acme/fmt
found acme/fmt in registry aqua@3c9f0e5b1d2a
added fmt v1.4.2 (released 41 days ago)
  platforms     darwin/arm64 linux/amd64 linux/arm64 windows/amd64
  authenticity  github attestation ✓  sigstore ✓
  identity      acme/fmt (id 81234567) .github/workflows/release.yaml@refs/tags/v*, hosted runners
                from registry aqua@3c9f0e5b1d2a
  provenance ✓   sbom ✓ (CycloneDX, covered by the attestation)
  host          darwin macos>=12, linux static, windows>=10
  provides      fmt

$ hermetic add acme/query
error: acme/query v0.9.0 does not publish an SBOM
  authenticity  release attestation ✓ (immutable release)
to add it without one:  hermetic add acme/query --missing sbom="<why>"

$ hermetic add acme/lang
  assets    https://dl.acme.example/lang/    versions  tags of github.com/acme/lang
error: nothing authenticates acme/lang 1.31.2
  no signature, provenance or release attestation; its checksum index is served by the asset's own host
to add it on its digest alone:
  hermetic add acme/lang --opt-out authenticity="<why>" --missing sbom="<why>"

$ hermetic add acme/toolchain
error: acme/toolchain v2.1.0 has no asset for windows/amd64
to add it without that platform:  hermetic add acme/toolchain --exclude windows/amd64="<why>"
```

A package no registry knows: recognised asset naming (`{name}_{version}_{os}_{arch}`
and usual variants) needs no flags; otherwise hermetic shows the candidates and asks
for a pattern. Its signer is never taken from the release:

```
$ hermetic add acme/newtool --asset 'newtool-{os}{arch}.{ext}' --os darwin=mac --arch amd64=64,arm64=-aarch64
error: acme/newtool v0.3.0 is signed, and no registry says by whom it should be
  the release names  https://github.com/acme/newtool/.github/workflows/release.yml@refs/tags/v0.3.0
after checking that is the publisher's:
  hermetic add acme/newtool … --signer 'https://github.com/acme/newtool/.github/workflows/release.yml@refs/tags/v*'

$ hermetic add lang --url 'https://dl.acme.example/lang/lang{version}.{os}-{arch}.{ext}' \
    --versions github-tags:acme/lang --tag-prefix lang --opt-out authenticity="…" --missing sbom="…"
```

### Add and use artifacts

```
$ hermetic add --artifact engine-src github:ghe.corp.example/acme/engine@v5.0.1 --asset 'engine-{version}.src.tar.xz' --gpg <fingerprint>
$ hermetic add --artifact corpus https://downloads.example.org/corpus/corpus-2026-10.tar.zst \
    --minisign-key RWT<example.org corpus key> --extract
$ tar -xf "$(hermetic path engine-src)" -C build/
$ ls "$(hermetic path corpus)"     # extracted: a directory
cases/  expected/  README
```

`hermetic path <package>` takes a package name, never an executable's, and resolves as
links do: the current directory's project, else the global manifest. For an artifact
it prints the file, or the extracted directory; for a tool, the package's directory in
the store.

GitHub's generated source archives (`/archive/<tag>.tar.gz`) are refused as asset
sources: GitHub does not guarantee their bytes stay the same. An uploaded release asset
is the stable form. Source at a commit with no release is its own kind of artifact,
checked by what the archive contains, not by its bytes:

```
$ hermetic add --artifact flac-conformance github:acme/flac-test-files@4f0c9a2e7b1d… --path subset
added artifact flac-conformance (acme/flac-test-files, subset/ at 4f0c9a2e)
  integrity     git tree 9d1e44c0 ✓ (matches the commit's)  sha256 over the files 71b3a8… recorded
  authenticity  commit reachable from refs/heads/main of acme/flac-test-files ✓

$ hermetic add --artifact x github:acme/flac-test-files@0bad5eed…
error: commit 0bad5eed is not reachable from any branch or tag of acme/flac-test-files
  GitHub serves every commit of a repository's fork network under its URL; this one may be a fork's
```

- Integrity is a SHA-256 over the extracted files in a canonical order, recorded at
  `add`, plus a match of git's tree hash with the commit's, which ties the content to
  the commit.
- Authenticity is the commit being reachable from a branch or tag of the repository
  itself, checked at `add`, or its signature when the repository signs commits;
  otherwise an `authenticity` opt-out.
- A repository whose `.gitattributes` uses `export-ignore` or `export-subst` produces
  archives that differ from its tree: the tree check fails, and the error says why.

### Type a tool's name in a project

```
$ hermetic setup
add this to ~/.zshrc, after anything else that changes PATH:
  export PATH="/Users/me/.local/share/hermetic/bin:$PATH"

$ cd ~/src/project && fmt --version
hermetic: project ~/src/project is not allowed; fmt runs from the global manifest or the host
  to allow it:  hermetic allow
fmt: command not found
$ hermetic allow
allowing ~/src/project
  names
    fmt            github:acme/fmt         acme/fmt (id 81234567) release.yaml@refs/tags/v*
    query          github:acme/query       release attestation, acme/query (id 55501234)
    tc, tcfmt      github:acme/toolchain   minisign RWQ<acme toolchain release key>; slsa
    lang, langfmt  https://dl.acme.example/lang/…   nothing but the digest (opt-out)
  environment
    TMPDIR          {project}/.build/tmp
    TC_CACHE        {shared}/tc-2.1.0          (tc, tcfmt)
    TC_CONFIG_HOME  {state}/cache/tc-config    (tc, tcfmt)
    TC_ENGINE_SRC   store: engine-src          (tc, tcfmt)
  PATH, after the tools    {project}/.build/bin
  writes outside the checkout    {state}/cache, {shared}/tc-2.1.0, {home}/.cache/acme-ide (opt-out)
allowed
$ fmt --version
fetching fmt v1.5.0 (darwin/arm64) … digest ✓
fmt v1.5.0
$ cd ~ && fmt --version
fmt: command not found
```

A pull that bumps a version from the same source and identity asks nothing. One that
changes anything else `allow` covers, or pins an older version, asks again:

```
$ git status
hermetic: ~/src/project changed what it decides; git runs from the global manifest or the host
  + env  GIT_CONFIG_GLOBAL = {location:tmp}/gc   (git)   ⚠ makes git load configuration from the checkout
  to allow it:  hermetic allow
```

### Global tools, and running sealed

```
$ hermetic add --global acme/query
added query v0.9.0 to ~/.config/hermetic/hermetic.yaml
$ cd /tmp && query --version
query v0.9.0
$ cd ~/src/project && hermetic exec -- query --version
error: query is not provided by this project (.hermetic/hermetic.yaml)

$ export GOFLAGS=-mod=mod NODE_OPTIONS=--require=./hook.js
$ hermetic exec --explain -- env      # with the manifest above
HOME=/Users/me                       os
LANG=en_US.UTF-8                     os
SSH_AUTH_SOCK=…                      host (~/.config/hermetic/config.yaml: pass-env)
GITHUB_TOKEN=…                       project (pass-env)
TMPDIR=/Users/me/src/project/.build/tmp   project (env)
PATH=/Users/me/.cache/hermetic/env/4f1a…/bin:/Users/me/src/project/.build/bin:/usr/bin:/bin

$ hermetic exec -- curl https://example.org      # with system-path: false
error: curl is not provided by this project (.hermetic/hermetic.yaml)
  the host has /usr/bin/curl, which this environment does not use
  to provide it:  hermetic add <owner>/<repo>

$ eval "$(hermetic env --shell zsh)"     # the sealed environment in a shell; --json for an editor
```

With `system-path: false`, even `env` has to be declared.

### Environment and locations

```
$ hermetic location add tc-shared '{shared}/tc-{version:toolchain}'
added location tc-shared → /Users/me/.local/share/hermetic/shared/tc-2.1.0
$ hermetic env-set toolchain TC_CACHE='{location:tc-shared}'
$ hermetic env-set toolchain TC_CACHE='/Users/me/.tc'
error: TC_CACHE is a path outside every location and package
  name it as a location, with its reason:  hermetic location add <name> '{home}/.tc' --outside "<why>"
$ hermetic env-set NODE_OPTIONS='--require={project}/hook.js'
set NODE_OPTIONS for every command
  ⚠ makes node load code from the checkout; allow will show it, and a policy may forbid it
$ hermetic path-add '{project}/.build/bin'
added {project}/.build/bin to PATH, after the project's tools
```

`doctor` reports a location inside the checkout that git does not ignore.

### Host requirements

```
$ hermetic install
error: toolchain v2.1.0 (linux/amd64) needs glibc>=2.28; this host has glibc 2.24
  toolchain does not ship for linux/amd64-musl
  run it on a newer host, or pin a toolchain version built for older glibc
```

### Many projects at once

```
[project-a] $ hermetic install
fetching fmt v1.5.0 (darwin/arm64) … digest ✓
[project-b] $ hermetic install
waiting for fmt v1.5.0, being fetched by another hermetic (pid 4182) … ready
[project-c] $ hermetic install
fetching query v0.9.0 (darwin/arm64) … digest ✓
```

`fmt` is downloaded once; `project-c` never waits. Killing `project-a` mid-download
leaves no partial entry, and `project-b` takes over. A wait is announced only once it
lasts more than a moment.

### Update

```
$ hermetic outdated
fmt        v1.4.2 → v1.5.0   (9 days ago)
toolchain  v2.1.0 → v2.2.0   (6 days ago)
query      v0.9.0 → v0.9.3   (5 hours ago: cooldown, 2 days 19 hours left)

$ hermetic update fmt
updated fmt v1.4.2 → v1.5.0   authenticity ✓ provenance ✓ sbom ✓   provides fmt, fmt-lsp (new)

$ hermetic update toolchain
error: toolchain v2.2.0 loses evidence v2.1.0 had
  minisign  v2.1.0 was signed (RWQ<acme toolchain release key>); v2.2.0 is not
to accept it:  hermetic update toolchain --opt-out minisign="<why>"

$ hermetic update query@v0.9.3
updated query v0.9.0 → v0.9.3 (5 hours old, inside the cooldown: taken because named)

$ hermetic update fmt@v1.3.0
error: v1.3.0 is older than the pinned v1.5.0;  to go back: --allow-downgrade

$ hermetic update fmt
fmt v2.0.0 renamed its assets; re-read from registry aqua@3c9f0e5b1d2a: asset fmt-{os}-{arch}.{ext}
updated fmt v1.5.0 → v2.0.0

$ hermetic registry update corp
corp 7e21d0c4 → b90a113e   (no entry changed yet)
$ hermetic update
toolchain  description changed in corp@b90a113e
  + slsa provenance  source github.com/acme/toolchain, ref refs/tags/v*
  ~ minisign key  RWQ<old key> → RWQ<new key>   ⚠ identity change
error: the new description changes an identity; accept it:  hermetic update toolchain --accept-signer

$ hermetic update fmt
error: fmt v2.1.0 is signed by a signer the manifest does not name
  source  acme/fmt (id 81234567) refs/tags/v2.1.0   unchanged
  signer  pinned  acme/fmt/.github/workflows/release.yaml
          found   acme/release-workflows/.github/workflows/go-release.yaml
same source, new signer; if acme moved its releases to that workflow:  hermetic update fmt --accept-signer
```

Had the source repository's id differed, `--accept-signer` would not be offered
without `--signer`.

### A hand edit, unresolved

```
$ hermetic install
error: fmt says version v1.6.0, but its digests are v1.5.0's
  resolve it:  hermetic update --resolve fmt
$ git log
git refused: the manifest is inconsistent (fmt says v1.6.0, its digests are v1.5.0's)
  resolve it:  hermetic update --resolve fmt
```

### CI

```
# a setup step installs hermetic, pinned by digest in the workflow, never chosen by the branch,
# puts the links first on PATH, and allows the checkout
hermetic install                       # this platform: digests and host requirements
just test                              # sealed, as on a laptop
hermetic verify --changed origin/main  # touched entries: all evidence and records, all platforms, from the sources
hermetic verify --no-opt-outs          # for a project that allows none
```

The store is cached under the key `hermetic cache-key` prints. `verify --changed`
reads the forge's API, so the job sets `HERMETIC_GITHUB_TOKEN` (read-only) to stay
clear of the rate limit.

### An update bot's bump

The bot edits `version` alone, which leaves the entry unresolved. Either the bot runs
`hermetic update --resolve fmt` before committing (self-hosted Renovate with
`hermetic` in its allowed commands), or a CI job on its branches runs `hermetic update
--resolve --changed origin/main` and pushes the completed entry, which `verify
--changed` then checks like any change.

### Policy, host configuration and air gaps

```yaml
# ~/.config/hermetic/policy.yaml; a file $HERMETIC_POLICY names narrows further
sources: { github: [acme/*, aquaproj/*, ghe.corp.example/acme/*], https: [downloads.example.org, dl.acme.example] }
registries: [github:aquaproj/aqua-registry, github:acme/hermetic-registry]
require: [provenance]
cooldown: { minimum: 7d, except: ["github:acme/*"] }
opt-out: { forbid: [authenticity] }
locations: { outside: forbid }
env: { forbid-code-loading: true }
```

```yaml
# ~/.config/hermetic/config.yaml: the host configuration, one file, never layered
pass-env: [SSH_AUTH_SOCK, "RUNNER_*"]
mirrors:
  - { rewrite: "https://github.com/(.*)/releases/download/(.*)", to: "https://mirror.corp.example/github/$1/$2" }
  - { rewrite: "https://dl.acme.example/(.*)", to: "https://mirror.corp.example/acme/$1" }
ca: /etc/ssl/corp-root.pem
```

```
$ hermetic install
error: lang opts out of authenticity, which policy forbids (~/.config/hermetic/policy.yaml)
error: query has no provenance, which policy requires (~/.config/hermetic/policy.yaml)

$ hermetic vendor /media/usb/deps
wrote 6 packages, their material and the sigstore trust root, for 4 platforms (1.9 GiB)
# on the air-gapped machine
$ hermetic install --from /media/usb/deps && hermetic verify --offline
```

### Know what the project depends on

```
$ hermetic list
NAME        KIND      VERSION  AUTHENTICITY           PROVENANCE  SBOM     OPT-OUTS
fmt         tool      v1.5.0   attestation, sigstore  ✓           ✓
query       tool      v0.9.0   release attestation    —           missing
toolchain   tool      v2.1.0   slsa, minisign         ✓           ✓        (windows/amd64 excluded)
lang        tool      1.31.2   —                      —           missing  ⚠ authenticity
engine-src  artifact  v5.0.1   gpg                    —           n/a
corpus      artifact  —        minisign               —           n/a

$ hermetic show lang
lang  https://dl.acme.example/lang/…  1.31.2   provides lang, langfmt
  authenticity  none: only the digest recorded at add vouches for these bytes
  opt-out       authenticity  not signed, no provenance; the checksum index is served by the same host
  missing       sbom          not published

$ hermetic sbom > project.cdx.json          # --format spdx for SPDX
```

## User stories

What makes each persona's need done. The scenarios above illustrate them.

**Owner**
- **O1 Start**: `init` writes the manifest (schema, platforms, registries pinned); an
  optional `requires` states a minimum hermetic.
- **O2 Add a tool**: from a registry, latest stable past the cooldown unless named;
  resolved and authenticated for every platform; output lists evidence, identity and
  its origin, records, host requirements, executables.
- **O3 From a publisher's site**: URL pattern, version listing, material.
- **O3b Registries**: listed in the manifest, pinned by commit and digest; packages
  named by source or by registry-qualified alias, never a bare name; two registries
  disagreeing fail until `--from`; every entry records its origin; a moved pin
  changes nothing until `update` re-imports and shows the difference.
- **O4 Unknown to registries**: common asset naming recognised; otherwise candidates
  and the flags; a signed release needs `--signer`.
- **O5 Add an artifact**: release asset or URL; integrity and authenticity;
  extraction recorded; stable path; GitHub-generated archives refused as assets.
  Source at a commit (`github:owner/repo@<40 hex>`, optionally one `path`): checked
  by a SHA-256 over its files and git's tree hash; the commit reachable from the
  repository's own branches or tags, or signed.
- **O6 Update**: one, several or all; `outdated` shows ages and changes nothing;
  cooldown unless named; downgrade by flag; lost evidence refused with the opt-out to
  give; an entry that no longer fits re-read from the pinned registry, shown; a
  change in what a tool provides reported.
- **O7 New identity**: source and signer pinned apart; `--accept-signer` whenever the
  source repository is unchanged, however the signer moved, old and new shown; a
  different source repository needs `--signer`.
- **O8 Remove**: the entry goes; its names leave the environment at once.
- **O9 Opt out and exclude, visibly**: per package, per protection or platform,
  with a reason, or refused; missing records noted apart, with a reason; every
  listing shows both, protections given up first.
- **O10 Know what I depend on**: `list` (`--json`), `show` saying plainly what vouches
  for each package.
- **O11 Where tools write**: locations under the three roots, else an opt-out;
  variables for all commands or one tool, code-loading ones marked; `PATH` additions
  after the tools.
- **O12 Inconsistent edits caught**: an unresolved version fails everywhere with the
  fix; only manifest-changing commands write the manifest.
- **O13 Shared baseline**: `init --include`, a file in the repository only; union
  with explicit `overrides` and `exclude`; writes go to the including manifest or the
  one `--manifest` names; `list` says where each entry comes from.

**Contributor**
- **C1 Clone and go**: hermetic installed once per machine; after a clone, `allow`;
  tools fetched on first use; a manifest newer than the installed hermetic refused,
  naming the release that reads it.
- **C2 Type the project's tools**: after `allow`, in any shell, editor or script, no
  hook, sealed as under `exec` unless the entry says `unsealed: <why>`; anything beyond
  a newer version from the same source and identity asks again.
- **C3 Sealed when it matters**: under `exec`, undeclared tools not found, the
  caller's environment kept out beyond the OS essentials, the host's pass-list
  (GitHub Actions' built in) and the project's `pass-env`, shown by `--explain`;
  global tools invisible; `env` exports it for a shell or editor.
- **C4 My own tools**: `--global`, used outside projects and for undeclared names.
- **C5 Monorepos**: a package's manifest includes the repository's explicitly; a
  worktree or a nested clone is always its own project.
- **C6 Many projects**: one store, one download per digest, parallel-safe, waits
  announced; two projects, two versions, no global state.
- **C7 Understand a failure**: `doctor` checks the links directory's place on `PATH`,
  host requirements, store, policy, locations, source reachability.
- **C8 Reclaim space**: `store prune` spares what recent manifests pin and running
  processes hold.
- **C9 Windows**: links are hard links to hermetic's executable, running the tool as
  a child and forwarding its exit code.

**CI**
- **CI1** `install`: this platform, digests and host requirements, in parallel.
- **CI2** `verify --changed <base>`: every check, every platform, for touched entries.
- **CI3** `cache-key`; a restored store re-hashed (to be reassessed for cost).
- **CI4** `verify --no-opt-outs`: fails on any, listing them.

**Agent**
- **A1** Everything hermetic reads and writes lives under one `HERMETIC_HOME`, so a
  session has its own approvals and global tools; nothing waits for input.
- **A2** The project's locations cover what its tools write, so a sandbox allowing the
  checkout, `{state}` and the store suffices.
- **A3** `fallthrough: never`: an unallowed project's name fails rather than running
  the host's.

**Auditor**
- **AU1** The manifest and its includes give, per package: source, version, per-platform digest, size
  and host requirements, expected identities, opt-outs and exclusions with reasons,
  and everything the project sets around it.
- **AU2** `verify --refetch` from the sources; `verify --offline` from stored material.
- **AU3** `hermetic sbom`, CycloneDX or SPDX.

**Bot**
- **B1** A bump is a `version` edit, completed by `update --resolve` (bot or CI) and
  checked by `verify --changed`.

**Administrator**
- **P1** Policy narrows, never widens: sources, registries, required provenance, cooldown,
  opt-outs, locations, code-loading variables; errors name the file and rule.
- **P2** Mirrors and proxies, in the host configuration (one file, never layered):
  URL rewrites and blocks for assets, listings and material; `HTTPS_PROXY`; added CA;
  no manifest change, no new `allow`.
- **P3** Air gap: `vendor`, `install --from`, `verify --offline`.

## The CLI at a glance

```
hermetic init --platforms <list> [--include <path>]…
hermetic migrate
hermetic include add <path> [--exclude <name>]…
hermetic include remove <path>
# every command that writes takes --manifest <path> to write an included file instead
hermetic add [--global] <owner>/<repo> | <registry>:<alias>[@<version>] [--from <registry>] [--override] [--asset <pattern>] [--signer <identity>] [--opt-out <protection>=<why>]… [--missing <record>=<why>]… [--exclude <platform>=<why>]…
hermetic add <name> --url <pattern> --versions <github-tags:owner/repo | index URL> […]
hermetic add --artifact <name> <owner>/<repo>@<version> --asset <pattern> | <https URL> [--gpg <fpr> | --minisign-key <key> | --signer <identity>]
hermetic update [<name>[@<version>]…] [--override] [--resolve] [--changed <base>] [--accept-signer] [--allow-downgrade] [--no-cooldown]
hermetic outdated
hermetic remove <name>
hermetic opt-out <name> <requirement>=<why>
hermetic location add <name> <path> [--outside <why>]
hermetic locations [--json]
hermetic path-add <dir>
hermetic env-set [<tool>] <NAME>=<value>
hermetic env-unset [<tool>] <NAME>
hermetic search <name>
hermetic registry add <name> <source>[@<commit>]
hermetic registry update [<name>]
hermetic registry remove <name>
hermetic list [--json]
hermetic show <name>
hermetic path <package>
hermetic exec [--pass-env <NAME>]… [--explain] -- <command> [<args>…]
hermetic allow | disallow
hermetic setup
hermetic env [--shell <shell> | --json]
hermetic install [--from <dir>] [--offline]
hermetic verify [--changed <base> | --refetch | --offline] [--no-opt-outs]
hermetic vendor <dir>
hermetic cache-key
hermetic sbom [--format cyclonedx|spdx]
hermetic store prune [--unused-for <duration>]
hermetic doctor
```

## A separate tool: SBOM audit

Matching SBOMs against known vulnerabilities needs a vulnerability database, changes
its verdict when the database changes rather than when the project does, and applies
to any SBOM. It is a tool of its own, reading an SBOM (and a publisher's VEX, where
one says which findings do not apply):

```
$ sbom-audit project.cdx.json
fmt    v1.5.0  clean
query  v0.9.0  GHSA-xxxx-xxxx-xxxx  high  fixed in v0.9.3
```

hermetic neither calls it nor depends on it; `hermetic sbom` is where they meet.

## Non-goals

- Building from source: fetch it as an artifact and build it yourself.
- A closure of what a binary loads: host requirements are recorded and checked; the
  C library is not shipped, and binaries are not rewritten.
- A language package manager (Go modules, crates, npm packages) or a system package
  manager: nothing outside the store; the global manifest serves the user, not the
  OS.
- A build daemon, trusted users, or a binary cache with its own keys: hermetic builds
  nothing, so publishers' evidence is the root of trust.
- Shell hooks: links work in every process, not only a shell that ran a hook.
- Vulnerability auditing ([above](#a-separate-tool-sbom-audit)).
