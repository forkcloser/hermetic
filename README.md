# hermetic

The toolset's own aqua: aqua as a Go library behind one binary, `hermetic`, with
one root of its own, a project directory of its own (`.hermetic/`), a controlled
environment for the library and the verifiers it runs, and a `bin` of links to
itself that dispatch to the tools it installs. A shell's aqua, if it has one, is
never shared with the toolset.

The design, the environment the binary exposes and the migration from the
installer's aqua are in [docs/design.md](docs/design.md). The binary is installed
by [limen-install](https://github.com/farcloser/limen-install) and run by
[limen](https://github.com/farcloser/limen)'s recipes.
