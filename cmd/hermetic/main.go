package main

import "github.com/forkcloser/hermetic/internal/cli"

// name is the binary's name: the one the dispatch compares argv[0] against,
// the target of every link in the root's bin, the default root's directory.
const name = "hermetic"

// version is stamped by the release build (-X main.version).
var version = ""

func main() {
	cli.Main(name, version)
}
