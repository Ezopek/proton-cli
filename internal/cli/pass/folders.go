package pass

import (
	stdctx "context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ref"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

// Folders inside a vault, named by their path from it: Personal/Banking/Cards.

func foldersCmd() *cobra.Command {
	c := &cobra.Command{Use: "folders", Short: "Folders that sort the items inside a vault"}
	c.AddCommand(foldersListCmd(), foldersCreateCmd(), foldersUpdateCmd(), foldersDeleteCmd())
	return c
}

// folderRef is the single token that addresses a folder: its vault's ID and
// its own.
func folderRef(f passsvc.Folder) string { return kit.JoinPair(f.ShareID, f.FolderID) }

func folderColumns() []ui.Column[passsvc.Folder] {
	return []ui.Column[passsvc.Folder]{
		{Header: "ID", ID: true, Cell: folderRef},
		{Header: "PATH", Flex: true, Handle: true, Cell: func(f passsvc.Folder) string { return f.Path }},
	}
}

// resolveFolder turns a reference into a folder: its two IDs, short or full, or
// its path from its vault.
func resolveFolder(c *kit.Invocation, reference string) (passsvc.Folder, error) {
	expanded, err := expandFolderRef(c, reference)
	if err != nil {
		return passsvc.Folder{}, err
	}
	return c.App.Pass.ResolveFolder(c.Ctx, expanded)
}

// resolvePlace turns a reference into somewhere an item can go: a vault, or a
// folder in one.
func resolvePlace(c *kit.Invocation, reference string) (passsvc.Place, error) {
	expanded, err := expandFolderRef(c, reference)
	if err != nil {
		return passsvc.Place{}, err
	}
	if expanded == reference {
		if expanded, err = kit.Expand(c.App, reference); err != nil {
			return passsvc.Place{}, err
		}
	}
	return c.App.Pass.ResolvePlace(c.Ctx, expanded)
}

// expandFolderRef expands a folder's two short IDs into full ones. A path has a
// slash in it too, so it is left as it was unless both halves turn out to be
// IDs.
func expandFolderRef(c *kit.Invocation, reference string) (string, error) {
	first, second, err := kit.ExpandPair(c.App, reference)
	if err != nil {
		return "", err
	}
	if ref.Full(first) && ref.Full(second) {
		return kit.JoinPair(first, second), nil
	}
	return reference, nil
}

func foldersListCmd() *cobra.Command {
	var vault string
	c := &cobra.Command{
		Use:   "list",
		Short: "List the folders in your vaults",
		Long: "List the folders in your vaults.\n\n" +
			"A hidden vault's folders are left out unless --vault names it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			vaultRef, err := kit.Expand(c.App, vault)
			if err != nil {
				return err
			}
			folders, err := c.App.Pass.FoldersList(c.Ctx, vaultRef)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Folder]{
				Noun: "folders", Columns: folderColumns(),
				Total: ui.Unknown, Page: ui.Unpaged, Filtered: vault != "",
			}, folders)
		}),
	}
	c.Flags().StringVar(&vault, "vault", "", "Match only this vault, by name or ID")
	return c
}

// folderName refuses a name a folder cannot have, which is judged from the
// command line alone.
func folderName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return kit.Fail("A folder needs a name.").Hint("--name Banking")
	case strings.Contains(name, ref.Compound):
		return kit.Fail("A folder's name cannot contain %s.", ref.Compound).
			Hint("--parent puts a folder inside another")
	}
	return nil
}

func foldersCreateCmd() *cobra.Command {
	var name, vault, parent string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a folder",
		Long: "Create a folder, at the top of a vault or inside another folder.\n\n" +
			"Making folders needs a paid Pass plan. --parent names the folder by its\n" +
			"path from its vault, Personal/Banking, or by its ID.",
		RunE: kit.Run([]kit.Step{func(*kit.Invocation) error { return folderName(name) }},
			func(c *kit.Invocation) error {
				var shareID, parentID string
				if parent != "" {
					holder, err := resolveFolder(c, parent)
					if err != nil {
						return err
					}
					shareID, parentID = holder.ShareID, holder.FolderID
				} else {
					var err error
					if shareID, err = resolveVault(c, vault); err != nil {
						return err
					}
				}
				name = strings.TrimSpace(name)
				if err := c.App.Pass.CheckFolder(c.Ctx, shareID, parentID, name, ""); err != nil {
					return err
				}
				return kit.Create(c, ui.ResultSpec{
					Action: ui.Created, Kind: "folders", Name: name,
				}, func() (string, error) {
					made, err := c.App.Pass.FolderCreate(c.Ctx, shareID, parentID, name)
					if err != nil {
						return "", err
					}
					return folderRef(made), nil
				})
			}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new folder")
	c.Flags().StringVar(&vault, "vault", "", "Which vault, by name or ID (default: your first)")
	c.Flags().StringVar(&parent, "parent", "", "Put it inside this folder, by path or ID")
	kit.Completes(c, "parent", nil, "pass folders")
	kit.Exclusive(c, "vault", "parent")
	return c
}

func foldersUpdateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename a folder",
		Long: "Rename a folder.\n\n" +
			"Renaming needs a paid Pass plan.",
		RunE: kit.Run([]kit.Step{func(*kit.Invocation) error { return folderName(name) }},
			func(c *kit.Invocation) error {
				folder, err := resolveFolder(c, c.Args[0])
				if err != nil {
					return err
				}
				name = strings.TrimSpace(name)
				if err := c.App.Pass.CheckFolder(c.Ctx, folder.ShareID, folder.ParentID, name, folder.FolderID); err != nil {
					return err
				}
				return kit.Mutate(c, ui.ResultSpec{
					Action: ui.Updated, Kind: "folders", Count: 1, Name: name,
					IDs: []string{folderRef(folder)},
				}, func() error {
					return c.App.Pass.FolderRename(c.Ctx, folder, name)
				})
			}),
	}
	c.Flags().StringVar(&name, "name", "", "The folder's new name")
	return c
}

func foldersDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete folders, and everything inside them",
		Long: "Delete folders, and everything inside them.\n\n" +
			"The items and folders inside a folder are deleted with it, and nothing\n" +
			"brings them back.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			sel, err := kit.Select(c, kit.Selector[passsvc.Folder]{
				Noun: "folders", Columns: folderColumns(), IDOf: folderRef,
				ByRef: func(_ stdctx.Context, reference string) (passsvc.Folder, error) {
					return resolveFolder(c, reference)
				},
			})
			if err != nil {
				return err
			}
			outermost, err := outermostFolders(c, sel.Rows)
			if err != nil {
				return err
			}
			items, folders := 0, 0
			for _, f := range outermost {
				i, n, err := c.App.Pass.FolderContents(c.Ctx, f)
				if err != nil {
					return err
				}
				items, folders = items+i, folders+n
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "folders", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(f passsvc.Folder) string { return f.Name }),
				Detail:  inside(items, folders, len(outermost)),
				Preview: sel.Preview(),
			}, func() error {
				byVault := map[string][]string{}
				var order []string
				for _, f := range outermost {
					if _, seen := byVault[f.ShareID]; !seen {
						order = append(order, f.ShareID)
					}
					byVault[f.ShareID] = append(byVault[f.ShareID], f.FolderID)
				}
				for _, shareID := range order {
					if err := c.App.Pass.FolderDelete(c.Ctx, shareID, byVault[shareID]); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// outermostFolders are the folders named, less any inside another one named,
// which goes with it anyway.
func outermostFolders(c *kit.Invocation, named []passsvc.Folder) ([]passsvc.Folder, error) {
	var out []passsvc.Folder
	for _, f := range named {
		inner := false
		for _, other := range named {
			if other.ShareID != f.ShareID || other.FolderID == f.FolderID {
				continue
			}
			below, err := c.App.Pass.Below(c.Ctx, other)
			if err != nil {
				return nil, err
			}
			inner = inner || below[f.FolderID]
		}
		if !inner {
			out = append(out, f)
		}
	}
	return out, nil
}

// inside is what a deletion says goes with the folders, and nothing when they
// are empty.
func inside(items, folders, deleted int) string {
	var parts []string
	if items > 0 {
		parts = append(parts, ui.Quantity(items, "items"))
	}
	if folders > 0 {
		parts = append(parts, ui.Quantity(folders, "folders"))
	}
	if len(parts) == 0 {
		return ""
	}
	where := "inside it"
	if deleted > 1 {
		where = "inside them"
	}
	return "and the " + strings.Join(parts, " and ") + " " + where
}
