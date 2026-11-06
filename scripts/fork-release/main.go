// Command fork-release holds the offline checks that keep this fork's release
// path closed.
//
// The release and mirror profiles read a tree, as a plain file system, and name
// every item the fork has not classified; both are pure functions over the
// tree. The guard command applies one of them to the tree of an exact commit,
// after checking its own build identity. The other commands arrive in later
// stories.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], productionDeps()))
}
