# hermetic: everything fetched from outside, one set of verifiers

The design for hermetic taking over the hand-fetched artifacts that limen pins today, and for
the verifiers both kinds of fetch share. Decided 2026-11-02, before any code. As in
[design.md](design.md), what is here describes the end state; the sections at the end say
what moves and in what order.

## The problem

A repository fetches two kinds of thing from outside, and two programs handle them, each
with its own verifiers:

- **Tools**, the executables on the hermetic PATH, go through hermetic: aqua's registries,
  aqua's checksum file, aqua's verifiers (cosign, minisign, SLSA, GitHub artifact
  attestations). aqua has no PGP and no CMS. aqua's GPG request, aquaproj/aqua#1346, has
  been open since 2022 with no design.
- **Archives**, the inputs a build fetches by hand (a kernel tarball, LLVM, a C library's
  sources, a vendor SDK), go through limen's `pins.yaml`:
  - `limen pins refresh` computes each sha256 through the verifier the entry names;
  - `limen pins get` hands the url and the sha256 to the build;
  - each build script downloads and compares on its own.

  limen implements its verifiers itself:
  - `download`
  - `github-attestation`
  - `github-release-asset`
  - `cosign-sha256sums`
  - `pgp-sha256sums`, with its own OpenPGP reader
  - `cms`, with its own RFC 5652 reader

Today, at the versions the fleet pins, that split costs four things:

- **Two of the checks exist twice, under different rules.** Both programs run
  `gh attestation verify` and `cosign verify-blob`. limen's attestation check pins only an
  owner (`--owner`), so any repository and workflow of that organisation passes. aqua's pins
  a signer workflow.
- **Each side lacks what the other has.** PGP and CMS exist only for archives; minisign and
  SLSA only for tools.
- **Two of the 11 tools that today rely on a checksum alone have a PGP signature upstream:**
  - Go: a detached `.asc` per file on dl.google.com;
  - CMake: a detached `.asc` over `SHA-256.txt`.

  hermetic cannot check either, and limen can, but only for archives, and only for
  clearsigned sums files.
- **Every build re-implements the download.** Each script takes a url and a sha256 and
  fetches, compares and caches its own way.

## The split

**hermetic owns everything fetched from outside**: tools, as today, and archives, with one
set of verifiers for both. **limen owns policy**:
- the rule that a repository declares what it fetches;
- the canonical files that carry the fleet's pins;
- the checks that run in lint.

limen fetches and verifies nothing. It keeps its zero-dependency rule by not doing the
cryptography at all, rather than by carrying a second copy of it. hermetic already links
aqua, so the verifiers' dependencies cost it nothing it doesn't already carry.

## The verifiers

One Go package in hermetic, `internal/verify`, holds every verifier as a function that takes
an artifact (a path to bytes already downloaded) or a sums file, plus the verifier's pinned
arguments, and returns the verified sha256 or an error that names what failed. The same
package serves the `archives:` and `tools:` sections of `.hermetic/pins.yaml` (below).

| Method | Verifies | Pinned by the entry |
|---|---|---|
| `download` | nothing: TLS is the whole guarantee | — (stated explicitly, never assumed) |
| `github-attestation` | `gh attestation verify` over the downloaded file, SLSA provenance | the **signer workflow** (`owner/repo/.github/workflows/<file>`), and optionally its ref; an owner alone is refused |
| `cosign-sha256sums` | `cosign verify-blob` over a SHA256SUMS file and its bundle; the artifact's line is taken from the verified text | identity regexp and OIDC issuer |
| `pgp-sha256sums` | a clearsigned sums file, verified in-process; the artifact's line is taken from the signed text | the key fingerprint (the key's URL is transport) |
| `pgp-detached` | a detached signature over the artifact, or over a sums file whose line is then taken, verified in-process | the key fingerprint |
| `cms` | a detached CMS signature over the artifact, verified in-process against a code-signing certificate chaining to a pinned root | the signer's subject regexp and the root's SHA-256 fingerprint |

Changes from limen's set:

- **`github-release-asset` goes.** A GitHub release attestation only proves that an asset is
  the one on an immutable GitHub release; a download from that release already is. It says
  nothing about how the asset was built. This is the reasoning aqua used to remove the same
  feature (aquaproj/aqua#4862, closed 2026-06-14). Its one user is ossein-kernel's `kata`
  entry, which becomes `download`.
- **`github-attestation` pins a signer workflow.** limen's `--owner` form accepted any
  workflow of the organisation. Four entries use it:
  - LLVM, in ossein-kernel and twice in curl, pinned to `--owner llvm`;
  - taciturn, in website-taciturn, pinned to `--owner forkcloser`.

  Each names the workflow that attests the release, read from the certificate's SAN:
  - LLVM: `llvm/llvm-project/.github/workflows/release-binaries.yml`, a reusable workflow
    that `release-tasks.yml` calls, at the release tag;
  - taciturn v0.3.0: `forkcloser/taciturn/.github/workflows/release.yaml`. Its releases
    since limen v0.22.0 run limen's reusable Apple lane, so the next one is signed by
    `farcloser/limen/.github/workflows/limen-release-apple.yaml`, a workflow of another
    organisation: `--owner forkcloser` refuses it, and gh requires `--signer-workflow`
    for a reusable workflow. The entry pins that workflow, with `--repo forkcloser/taciturn`.
- **`pgp-detached` is new.** It extends limen's OpenPGP reader from clearsigned documents to
  detached signatures: the same v4 keys and signatures, RSA or Ed25519 over SHA-2, the same
  refusal of anything else by name. Go and CMake sign this way, both with a subkey:
  - Go: a binary signature (`<file>.asc` beside each download on `dl.google.com`), RSA
    over SHA-256, by a subkey of Google's Linux Packages Signing Authority
    (`EB4C1BFD4F042F6DDDCCEC917721F63BD38B4796`, key at
    `https://dl.google.com/linux/linux_signing_key.pub`). The subkey rotates: `8461EFA0…8B8B`
    signed go1.22, `0F06FF86…6E42` go1.24 to go1.26, `0E225917…648F` go1.27. **The Windows
    archives carry no signature** (go1.27.3: `.asc` is 404 for the zip and the msi).
  - CMake: a binary signature over `cmake-<v>-SHA-256.txt`, RSA over SHA-512, by subkey
    `C6C265324BBEBDC350B513D02D2CEF1034921684` of Brad King's key
    (`CBA23971357C2E6590D9EFD3EC8FEF3A7BFB4EDA`, from keys.openpgp.org), the same subkey
    from v3.31.0 to v4.4.5. Some of its older binding signatures are SHA-1.

  limen's reader reads subkeys as keys of their own and verifies no binding signature: the
  pinned fingerprint is the trust decision. Pinning the signing subkey keeps that, and makes
  each rotation a reviewed pin change (Go's go1.27.0 bump would have failed until it).
  Pinning the primary needs binding verification (type 0x18, the newest binding, SHA-2 only).

limen's readers move with their tests: `internal/verify/openpgp` (with `openpgptest`) and
`internal/verify/cms`. They are standard-library only, and stay so. The child processes
(`gh`, `cosign`) run under hermetic's own environment control
([design.md](design.md#the-binary), "It controls the library's environment"): the scrubbed
environment aqua's verifiers already get, with the token passed as `HERMETIC_GITHUB_TOKEN`.

## The file

`pins.yaml` moves from the repository root to `.hermetic/pins.yaml`, beside `hermetic.yaml`.
It keeps limen's format, in two sections:

```yaml
archives:
  - name: kernel
    renovate: github-tags gregkh/linux
    extract-version: ^v(?<version>[0-9]+\.[0-9]+(?:\.[0-9]+)?)$
    version: 7.3
    url: https://cdn.kernel.org/pub/linux/kernel/v${major}.x/linux-${version}.tar.xz
    verify: pgp-sha256sums https://cdn.kernel.org/pub/linux/kernel/v${major}.x/sha256sums.asc <key-url> <fingerprint>
    digest:
      version: 7.3
      sha256: <64 hex>
tools:
  - name: golang/go
    verify: pgp-detached <signature-url> <key-url> <fingerprint>
  - name: koalaman/shellcheck
    verify: download
```

- **`archives:` is today's `pins:`, key for key.** Every key keeps its meaning and its order:
  `name`, `renovate`, `extract-version`, `versioning`, `version`, `commit`, `url`, `verify`,
  `digest`, and the `${version}`, `${major}` and named-group expansions. So the shared
  Renovate manager's regex is unchanged; only its file pattern moves to `.hermetic/pins.yaml`.
- **`tools:` declares a verifier for an aqua package that aqua cannot verify.**
  - The name is the package's, as `hermetic.yaml` pins it.
  - The version comes from that pin and is never repeated here, so Renovate moves one line.
  - The arguments expand `${version}` and the rest from that pin.
  - `verify: download` states, on purpose and in review, that the package is
    trust-on-first-use.
- The canonical tool set's entries live in limen's canonical
  `.limen/hermetic-pins.yaml`, content-pinned like `.limen/hermetic.yaml`. The repository's
  file imports it with `import: ../.limen/hermetic-pins.yaml`, the way `hermetic.yaml`
  imports the canonical tool set. A repository's own tools and archives go in its own file.
  A name in both is refused.

## The commands

- **`hermetic pins get <name> <field>`** is today's `limen pins get`: it prints `url`,
  `sha256`, `version` or `commit` for an archive. It refuses an entry whose digest is
  missing or stale.
- **`hermetic pins refresh [-all]`** is today's `limen pins refresh`. For each archive whose
  `digest.version` (or `digest.commit`) differs from its pin, it runs the declared verifier
  and rewrites the digest block in place.
- **`hermetic fetch <name>`** is new. It downloads an archive into
  `<root>/artifacts/sha256/<hex>`, if it isn't there, and compares its sha256 with the pin.
  It prints the path. A cached file is checked again on every fetch. A mismatch removes the
  file and fails. With the file cached, the build runs offline.
- **`hermetic check`** fails on:
  - a pins file out of shape;
  - a stale or missing digest;
  - an archive or tool whose verifier is malformed;
  - **a tool that aqua does not verify beyond its checksum and that has no `tools:` entry.**

  What counts as aqua verifying: an entry whose resolved registry entry, at the pinned
  version, has cosign, minisign, SLSA or an attestation on the asset or on its checksum file.
  So every trust-on-first-use tool is named in a file someone reviewed. The only way through
  is to verify the tool or to say `download`.

## The trust model

- **The verifier runs once, when a digest is recorded; the digest is checked on every
  fetch.** This is aqua's own model for a signed checksum file: aqua verifies the signature
  when `update-checksum` records it (`pkg/controller/updatechecksum`,
  `dlAndVerifyChecksumFile`), and installs compare against the record. It is kept here
  for the same reason:
  - the record is what review approves;
  - the network is needed only to record.
- **For a tool, the verifier runs inside `hermetic update-checksum`.** Once aqua's
  controller has written `aqua-checksums.json`, hermetic takes each new checksum of a package
  with a `tools:` verifier and runs the verifier against it:
  - for a detached signature over the asset, the asset is downloaded and verified, and its
    hash must equal the recorded one;
  - for a signed sums file, the recorded hash must equal the signed line.

  A mismatch fails the command and removes nothing from the checksum file. The checksum
  workflow then stops before it commits, so a Renovate branch never carries an unverified
  tool checksum. Install is unchanged: aqua compares the download with the recorded
  checksum, as it does today.
- **Trust-on-first-use is a written decision, never a fallback.** `verify: download` on an
  archive or a tool is the only way to have none; `hermetic check` refuses a silent one.

## Renovate and the checksum workflow

- The shared custom manager for pins moves from `pins.yaml` to `.hermetic/pins.yaml` in
  limen's canonical `.limen/renovate.json`. Its regex reads `archives:` entries exactly as
  it reads `pins:` entries today. `tools:` entries carry no version, so the manager has
  nothing to match there.
- limen's checksum workflow replaces `limen pins refresh` with `hermetic pins refresh`, after
  `hermetic update-checksum --prune`, which now also runs the `tools:` verifiers. The step
  stays in the same job, on the same release of hermetic that the setup action installs and
  verifies.

## What limen keeps

- The canonical `.limen/hermetic-pins.yaml`.
- A `pins` rule reduced to policy: a repository that fetches by hand has a
  `.hermetic/pins.yaml`, and the lint lane runs `hermetic check`.
- `fix`, which moves a root `pins.yaml` to `.hermetic/pins.yaml`, renames `## Verified (2026-11-03)

- The signer workflows: above, under `github-attestation`.
- Go's and CMake's keys: above, under `pgp-detached`.
- limen's OpenPGP reader parses both signatures' packets (v4, RSA, SHA-256 and SHA-512);
  what it lacks is the detached frame (signature type 0 over the raw bytes) and, if the
  primary is pinned, binding signatures.
- `hermetic check` resolves a standard entry offline when the pinned registry ref is cached
  (`<root>/store/registries/github_content/github.com/aquaproj/aqua-registry/<ref>/`);
  the first run after a registry bump fetches it once.

## Open

- Pin the signing subkey or the primary (above).
- Go on Windows: no signature to verify. Either those archives stay checksum-only, named so
  in the entry, or they are checked another way.

rews | 1 | file move only |
| forkcloser/go-graphviz | 10 | file move only |
| forkcloser/blake3 | 1 | file move only |
| forkcloser/openssh | 4 | file move only; CMake gets a `tools:` entry (`pgp-detached`) |
| forkcloser/erofs | 2 | file move only |
| forkcloser/godolint | 1 | file move only |
| forkcloser/curl | 2 | signer workflow (LLVM's) on both |
| forkcloser/website-taciturn | 1 | signer workflow (taciturn's release workflow) |
| forkcloser/testbin | 13 | file move only |

Each build script then moves from `limen pins get` to `hermetic pins get` (a text
change), and where it downloads itself, to `hermetic fetch`. A script that downloads inside a
container (ossein-kernel's builds take `--source-url`) gets the fetched file mounted instead
of a url, which is that repository's own change.

The tools that rely on a checksum alone get their `tools:` entries in the same limen
release:
- `pgp-detached` for Go;
- `download`, written out, for just, lychee, shellcheck and quint;
- `download`, in their repositories, for protoc, protoc-gen-go, FFmpegBin, coreutils and
  git-for-windows.

Go's entry goes in each repository's own file, beside its own Go pin. `fix` writes it as it
seeds the pin.

## Not yet verified

To check before the code relies on them:
- The workflow paths that sign LLVM's and taciturn's attestations.
- Go's signing key: the key URL, its fingerprint, and that every Go release since the
  fleet's baseline is signed with it.
- CMake's signing key and fingerprint.
- That limen's OpenPGP reader parses both signatures' packet types without change, beyond
  the detached frame.
- Whether `hermetic check` can resolve the standard registry's entry for a pinned version
  offline, from the registry aqua already caches under `<root>/store/registries/`, or needs
  the network once.

## Order of work

1. This design.
2. hermetic:
   - `internal/verify`, moved from limen with its tests, plus `pgp-detached` and the
     signer-workflow form of `github-attestation`;
   - the pins file, with its import;
   - `pins get`, `pins refresh`, `fetch` and `check`;
   - the `tools:` verifiers inside `update-checksum`;
   - a release.
3. limen:
   - the setup action's hermetic pin;
   - the canonical `.limen/hermetic-pins.yaml`;
   - the Renovate manager's file pattern;
   - the checksum workflow's step;
   - the `pins` rule and `fix`'s move;
   - the shims, and the book;

   all as one change, shipped by a release that the fleet follows on its bump.
4. The repositories: build scripts to `hermetic pins get` or `hermetic fetch`; the
   ossein-kernel, curl and website-taciturn verifier changes; the repository-owned `tools:`
   entries.
5. limen: the shims, `internal/pins` and `internal/verify` go, in the release after the
   fleet has switched.
