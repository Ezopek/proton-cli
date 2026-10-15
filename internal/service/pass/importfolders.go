package pass

import (
	"context"
	"fmt"
	"strings"

	"github.com/roman-16/proton-cli/internal/passfile"
)

// Filing what a file put in folders.
//
// A folder the vault already has at the same path takes the items rather than
// being made twice. One the plan has no room for is not made, and its items land
// in the closest folder above it that is there.

// filesFolders reports whether the file files any item in a folder.
func filesFolders(doc *passfile.Document) bool {
	for _, vault := range doc.Vaults {
		for _, entry := range vault.Items {
			if len(entry.Folder) > 0 {
				return true
			}
		}
	}
	return false
}

// foldersAsVaults is the document for a plan that has no folders. With split,
// each folder becomes a vault named after its path; without, the items stay in
// their vault and the folders are dropped.
func foldersAsVaults(doc *passfile.Document, split bool) *passfile.Document {
	out := *doc
	out.Vaults = nil
	at := map[string]int{}
	add := func(name string, entry passfile.Entry) {
		entry.Folder = nil
		if i, ok := at[name]; ok {
			out.Vaults[i].Items = append(out.Vaults[i].Items, entry)
			return
		}
		at[name] = len(out.Vaults)
		out.Vaults = append(out.Vaults, passfile.Vault{Name: name, Items: []passfile.Entry{entry}})
	}
	for _, vault := range doc.Vaults {
		for _, entry := range vault.Items {
			name := vault.Name
			if split && len(entry.Folder) > 0 {
				name = strings.Join(entry.Folder, "/")
			}
			add(name, entry)
		}
	}
	return &out
}

// limitReason is which folder limit kept a folder from being made.
type limitReason string

const (
	limitCount    limitReason = "count"
	limitChildren limitReason = "children"
	limitDepth    limitReason = "depth"
)

// folderFiling is where in one vault an import files what the file put in
// folders.
type folderFiling struct {
	// spots are where each of the file's folder paths lands, keyed by the path.
	spots map[string]importSpot
	// paths are each place written from the vault, for a preview.
	paths map[importSpot]string
	// blocked counts the folders not made, by the limit that kept each out.
	blocked map[limitReason]int
}

// spot is where an item filed under folder lands.
func (f *folderFiling) spot(folder []string) importSpot { return f.spots[folderKey(folder)] }

// path is a place written from the vault, and empty for the top.
func (f *folderFiling) path(spot importSpot) string { return f.paths[spot] }

func folderKey(folder []string) string { return strings.Join(folder, "\x00") }

// warnings say once for each limit how many folders it kept out.
func (f *folderFiling) warnings() []string {
	var out []string
	for _, reason := range []limitReason{limitCount, limitChildren, limitDepth} {
		n := f.blocked[reason]
		if n == 0 {
			continue
		}
		folders := fmt.Sprintf("%d folders were", n)
		if n == 1 {
			folders = "1 folder was"
		}
		switch reason {
		case limitCount:
			out = append(out, fmt.Sprintf("%s not made, because the vault holds the most folders it may. "+
				"The items filed there land in the closest folder above.", folders))
		case limitChildren:
			out = append(out, fmt.Sprintf("%s not made, because the place they go holds the most folders one place may. "+
				"The items filed there land in the closest folder above.", folders))
		case limitDepth:
			out = append(out, fmt.Sprintf("%s folded into the folder above, which is as deep as folders go.", folders))
		}
	}
	return out
}

// planFolders works out which of the file's folders one vault makes and where
// each item lands, adding the folders to make to the planned vault.
func (s *Service) planFolders(ctx context.Context, pv *PlannedVault, entries []passfile.Entry, limits FolderLimits) (*folderFiling, error) {
	f := &folderFiling{
		spots: map[string]importSpot{}, paths: map[importSpot]string{},
		blocked: map[limitReason]int{},
	}
	var tree *folderTree
	for _, entry := range entries {
		if len(entry.Folder) == 0 || pv.ShareID == "" || tree != nil {
			continue
		}
		var err error
		if tree, err = s.folders(ctx, pv.ShareID); err != nil {
			return nil, err
		}
	}

	total := 0
	if tree != nil {
		total = len(tree.byID) + len(tree.shut)
	}
	depth := map[importSpot]int{}
	madeUnder := map[importSpot]int{}
	held := map[string]limitReason{}
	for _, entry := range entries {
		for i := 1; i <= len(entry.Folder); i++ {
			key := folderKey(entry.Folder[:i])
			if _, seen := f.spots[key]; seen {
				continue
			}
			parentKey := folderKey(entry.Folder[:i-1])
			parent := f.spots[parentKey]
			if reason, ok := held[parentKey]; ok {
				held[key] = reason
				f.blocked[reason]++
				f.spots[key] = parent
				continue
			}
			name := entry.Folder[i-1]
			if existing := existingChild(tree, parent, name); existing != nil {
				spot := importSpot{FolderID: existing.FolderID}
				f.spots[key], f.paths[spot], depth[spot] = spot, existing.Path, existing.depth
				continue
			}
			siblings := madeUnder[parent]
			if tree != nil && parent.Made == 0 {
				siblings += len(tree.children(parent.FolderID))
			}
			var reason limitReason
			switch {
			case total >= limits.MaxCount:
				reason = limitCount
			case siblings >= limits.MaxChildren:
				reason = limitChildren
			case depth[parent] >= limits.MaxDepth:
				reason = limitDepth
			}
			if reason != "" {
				held[key] = reason
				f.blocked[reason]++
				f.spots[key] = parent
				continue
			}
			pv.Folders = append(pv.Folders, PlannedFolder{Name: name, In: parent})
			spot := importSpot{Made: len(pv.Folders)}
			above := f.paths[parent]
			if above == "" {
				above = pv.Name
			}
			f.spots[key], f.paths[spot], depth[spot] = spot, above+"/"+name, depth[parent]+1
			madeUnder[parent]++
			total++
		}
	}
	return f, nil
}

// existingChild is the folder the vault already has under parent with that
// name, or nil.
func existingChild(tree *folderTree, parent importSpot, name string) *Folder {
	if tree == nil || parent.Made != 0 {
		return nil
	}
	for _, c := range tree.children(parent.FolderID) {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}
