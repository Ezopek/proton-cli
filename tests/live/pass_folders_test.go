package live

import (
	"strings"
	"testing"
	"time"
)

// Folders inside a vault, which a plan gates: made, filled, listed, renamed and
// deleted on the paid account, in vaults the run made and takes away again.

// folderPaths are the paths `folders list` shows for one vault.
func folderPaths(t *testing.T, vault string) []string {
	t.Helper()
	var paths []string
	for _, row := range runJSONArrayPaid(t, "pass", "folders", "list", "--vault", vault) {
		m, _ := row.(map[string]interface{})
		path, _ := m["path"].(string)
		paths = append(paths, path)
	}
	return paths
}

// folderMade makes a folder on the paid account and waits until the vault lists
// it, answering with the reference it was given.
func folderMade(t *testing.T, vault, path string, args ...string) string {
	t.Helper()
	ref := assertBarePairRef(t, runOKPaid(t, append([]string{"pass", "folders", "create"}, args...)...),
		"pass folders create")
	if !waitFor(30*time.Second, time.Second, func() bool {
		for _, p := range folderPaths(t, vault) {
			if p == path {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("%s never listed %s", vault, path)
	}
	return ref
}

// itemsIn is the rows `items list` holds for a folder.
func itemsIn(t *testing.T, args ...string) map[string]map[string]interface{} {
	t.Helper()
	out := map[string]map[string]interface{}{}
	for _, row := range runJSONArrayPaid(t, append([]string{"pass", "items", "list"}, append(args, "--limit", "0")...)...) {
		m, _ := row.(map[string]interface{})
		name, _ := m["name"].(string)
		out[name] = m
	}
	return out
}

// The whole life of a folder: made at the top of a vault and inside another,
// filled, listed at one level and below it, emptied by a move, renamed, and
// deleted with what it holds.
func TestPassFoldersFileItems(t *testing.T) {
	shareID, vault := aVaultTheRunMade(t, "folders")
	banking := folderMade(t, vault, vault+"/Banking", "--name", "Banking", "--vault", vault)
	if !strings.HasPrefix(banking, shareID+"/") {
		t.Errorf("the folder %s is not in the vault it was made in (%s)", banking, shareID)
	}
	folderMade(t, vault, vault+"/Banking/Cards", "--name", "Cards", "--parent", vault+"/Banking")

	// A second folder of the same name in the same place could not be named
	// by its path, so it is refused.
	_, stderr, code := runPaid(t, "pass", "folders", "create", "--name", "banking", "--vault", vault)
	if code == 0 {
		t.Error("a second Banking was made beside the first")
	}
	assertContains(t, stderr, "already exists")

	name := testID() + "-in-folder"
	stdout := runOKPaid(t, "pass", "items", "create", "--name", name,
		"--folder", vault+"/Banking/Cards", "--secret-file", secretFile(t, "password", "filed-away"))
	ref := assertBarePairRef(t, stdout, "pass items create --folder")
	if !waitFor(30*time.Second, time.Second, func() bool {
		_, _, code := runPaid(t, "pass", "items", "get", "--", ref)
		return code == 0
	}) {
		t.Fatalf("pass never answered about the item it had just made: %s", ref)
	}
	got := runOKPaid(t, "pass", "items", "get", "--", ref)
	assertField(t, got, "Folder:", vault+"/Banking/Cards")
	assertField(t, got, "Password:", "filed-away")

	// A folder is one level; --recursive takes in the folders inside it.
	if _, ok := itemsIn(t, "--folder", vault+"/Banking")[name]; ok {
		t.Error("an item in Cards was listed as being in Banking itself")
	}
	row, ok := itemsIn(t, "--folder", vault+"/Banking", "--recursive")[name]
	if !ok {
		t.Fatal("--recursive did not take in the item in Cards")
	}
	if row["folder"] != vault+"/Banking/Cards" {
		t.Errorf("the listing places the item at %v", row["folder"])
	}

	// Out to the top of the vault and back in: the item stays in its vault, so
	// it keeps its ID.
	moved := strings.TrimSpace(runOKPaid(t, "pass", "items", "move", "--into", vault, "--", ref))
	if moved != ref {
		t.Errorf("a move within a vault changed the item's ID from %s to %s", ref, moved)
	}
	assertNotContains(t, runOKPaid(t, "pass", "items", "get", "--", ref), "Folder:")
	runOKPaid(t, "pass", "items", "move", "--into", vault+"/Banking", "--", ref)
	assertField(t, runOKPaid(t, "pass", "items", "get", "--", ref), "Folder:", vault+"/Banking")

	runOKPaid(t, "pass", "folders", "update", vault+"/Banking", "--name", "Finance")
	if !waitFor(30*time.Second, time.Second, func() bool {
		return strings.Join(folderPaths(t, vault), ",") == vault+"/Finance,"+vault+"/Finance/Cards"
	}) {
		t.Errorf("after the rename the folders are %v", folderPaths(t, vault))
	}

	// A dry run of the deletion says what goes with the folder.
	_, preview := runOKStderrPaid(t, "--dry-run", "pass", "folders", "delete", vault+"/Finance")
	assertContains(t, preview, "1 item and 1 folder inside it")

	runOKPaid(t, "pass", "folders", "delete", vault+"/Finance")
	if !waitFor(30*time.Second, time.Second, func() bool {
		_, _, code := runPaid(t, "pass", "items", "get", "--", ref)
		return code != 0 && len(folderPaths(t, vault)) == 0
	}) {
		t.Error("the folder, or the item inside it, outlived the deletion")
	}
}

// A plan without folders cannot make one, and says so before anything is sent.
func TestPassFoldersNeedAPaidPlan(t *testing.T) {
	_, stderr, code := run(t, "pass", "folders", "create", "--name", testID())
	if code == 0 {
		t.Fatal("a free account made a folder")
	}
	assertContains(t, stderr, "Folders need a paid Pass plan.")
	// Reading them is any plan's.
	runJSONArray(t, "pass", "folders", "list")
}
