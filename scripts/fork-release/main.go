// Command fork-release holds the offline checks that keep this fork's release
// path closed.
//
// The release profile reads a tree, as a plain file system, and names every
// item the fork has not classified. It is a pure function over the tree: it
// reads no repository state, no network and no git ref. The command surface,
// the git adapter and the other checks arrive in later stories.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fork-release: no command")
	os.Exit(2)
}
