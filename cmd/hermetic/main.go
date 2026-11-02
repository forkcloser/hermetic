// hermetic is the toolset's own aqua: aqua as a library, with its three
// paths derived from one root of ours, a project directory of our own, and a bin
// of links to this very binary (docs/design.md).
package main

import "github.com/forkcloser/hermetic/internal/cli"

// version is stamped by the release build (-X main.version).
var version = ""

func main() {
	cli.Main(version)
}
