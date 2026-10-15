package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Folders inside a vault.
//
// A folder's key is sealed under the key of what holds it - the vault for a
// folder at the top, the parent folder for one inside another - and an item in
// a folder has its own key sealed under the folder's. So opening anything inside
// a folder means opening the chain of folders above it first.

// Folder is one folder inside a vault.
type Folder struct {
	ShareID  string `json:"share_id"`
	FolderID string `json:"folder_id"`
	ParentID string `json:"parent_id,omitempty"`
	Vault    string `json:"vault"`
	Name     string `json:"name"`
	// Path is the folder written from its vault: Personal/Banking/Cards.
	Path string `json:"path"`

	key      []byte
	rotation int
	depth    int
}

// folderContentVersion is the version of the folder protobuf this writes.
const folderContentVersion = 1

// errFolderShut is an item inside a folder that would not open. The folder is
// counted once, and takes what is inside it with it.
var errFolderShut = errors.New("the folder holding it could not be opened")

// errNoKey is a key that never arrived: a rotation the vault does not hold, or
// a folder the vault does not list.
var errNoKey = errors.New("no key")

// folderTree is one vault's folders, opened.
type folderTree struct {
	vault string
	byID  map[string]*Folder
	// shut are the folders that would not open, and those inside them.
	shut map[string]bool
}

// list is every folder that opened, in path order.
func (t *folderTree) list() []Folder {
	out := make([]Folder, 0, len(t.byID))
	for _, f := range t.byID {
		out = append(out, *f)
	}
	slices.SortFunc(out, func(a, b Folder) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// children are the folders directly inside parentID, or at the top for "".
func (t *folderTree) children(parentID string) []*Folder {
	var out []*Folder
	for _, f := range t.byID {
		if f.ParentID == parentID {
			out = append(out, f)
		}
	}
	return out
}

// below is folderID and every folder inside it.
func (t *folderTree) below(folderID string) map[string]bool {
	out := map[string]bool{folderID: true}
	for grew := true; grew; {
		grew = false
		for id, f := range t.byID {
			if !out[id] && out[f.ParentID] {
				out[id], grew = true, true
			}
		}
	}
	return out
}

// folder is one folder of the tree, or why it cannot be had.
func (t *folderTree) folder(folderID string) (*Folder, error) {
	if f, ok := t.byID[folderID]; ok {
		return f, nil
	}
	if t.shut[folderID] {
		return nil, errFolderShut
	}
	return nil, fmt.Errorf("the vault lists no such folder: %w", errNoKey)
}

// folders opens one vault's folders, once per run.
func (s *Service) folders(ctx context.Context, shareID string) (*folderTree, error) {
	return s.trees.Do(shareID, func() (*folderTree, error) { return s.openFolders(ctx, shareID) })
}

// sealedFolder is a folder as Proton lists it.
type sealedFolder struct {
	FolderID       string
	ParentFolderID string
	KeyRotation    int
	FolderKey      string
	Content        string
}

func (s *Service) openFolders(ctx context.Context, shareID string) (*folderTree, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return nil, err
	}
	vault, err := s.vaultName(ctx, shareID)
	if err != nil {
		return nil, err
	}
	sealed, err := s.listFolders(ctx, shareID)
	if err != nil {
		return nil, err
	}
	tree := &folderTree{vault: vault, byID: map[string]*Folder{}, shut: map[string]bool{}}
	byID := make(map[string]sealedFolder, len(sealed))
	for _, f := range sealed {
		byID[f.FolderID] = f
	}
	var open func(f sealedFolder)
	open = func(f sealedFolder) {
		if _, done := tree.byID[f.FolderID]; done || tree.shut[f.FolderID] {
			return
		}
		var wrap []byte
		parentDepth := 0
		switch parent, listed := byID[f.ParentFolderID]; {
		case f.ParentFolderID == "":
			key, ok := sk.keys[f.KeyRotation]
			if !ok {
				skip.Record(ctx, skip.KindFolder, f.FolderID, skip.NoKey, nil)
				tree.shut[f.FolderID] = true
				return
			}
			wrap = key
		case !listed:
			skip.Record(ctx, skip.KindFolder, f.FolderID, skip.NoKey, nil)
			tree.shut[f.FolderID] = true
			return
		default:
			open(parent)
			opened, ok := tree.byID[parent.FolderID]
			if !ok {
				// Recorded and not counted: the folder above it is, and the warning
				// says that nothing inside a folder that would not open is listed.
				slog.DebugContext(ctx, "pass: a folder inside one that could not be opened",
					"ref", f.FolderID, "parent", parent.FolderID)
				tree.shut[f.FolderID] = true
				return
			}
			wrap, parentDepth = opened.key, opened.depth
		}
		folder, reason, err := openFolder(wrap, f)
		if err != nil {
			skip.Record(ctx, skip.KindFolder, f.FolderID, reason, err)
			tree.shut[f.FolderID] = true
			return
		}
		folder.ShareID, folder.Vault, folder.depth = shareID, vault, parentDepth+1
		tree.byID[f.FolderID] = folder
	}
	for _, f := range sealed {
		open(f)
	}
	for _, f := range tree.byID {
		f.Path = tree.path(f)
	}
	return tree, nil
}

// path writes a folder from its vault.
func (t *folderTree) path(f *Folder) string {
	parts := []string{f.Name}
	for at := t.byID[f.ParentID]; at != nil; at = t.byID[at.ParentID] {
		parts = append([]string{at.Name}, parts...)
	}
	return t.vault + "/" + strings.Join(parts, "/")
}

// openFolder unseals one folder under the key of what holds it.
func openFolder(wrap []byte, f sealedFolder) (*Folder, skip.Reason, error) {
	sealedKey, err := base64.StdEncoding.DecodeString(f.FolderKey)
	if err != nil {
		return nil, skip.Malformed, err
	}
	key, err := aead.Decrypt(wrap, sealedKey, []byte(aead.TagFolderKey))
	if err != nil {
		return nil, skip.Undecryptable, err
	}
	sealedContent, err := base64.StdEncoding.DecodeString(f.Content)
	if err != nil {
		return nil, skip.Malformed, err
	}
	plain, err := aead.Decrypt(key, sealedContent, []byte(aead.TagFolderContent))
	if err != nil {
		return nil, skip.Undecryptable, err
	}
	var content pb.Folder
	if err := proto.Unmarshal(plain, &content); err != nil {
		return nil, skip.Malformed, err
	}
	return &Folder{
		FolderID: f.FolderID, ParentID: f.ParentFolderID, Name: content.GetName(),
		key: key, rotation: f.KeyRotation,
	}, "", nil
}

// listFolders reads every folder a vault holds, still sealed.
func (s *Service) listFolders(ctx context.Context, shareID string) ([]sealedFolder, error) {
	var since string
	return proton.All(ctx, func(ctx context.Context, _ int) ([]sealedFolder, bool, error) {
		q := proton.Query()
		if since != "" {
			q.Set("Since", since)
		}
		var r struct {
			Folders struct {
				Folders   []json.RawMessage
				LastToken string
			}
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/pass/v1/share/" + shareID + "/folder", Query: q,
		}, &r); err != nil {
			return nil, false, err
		}
		out := make([]sealedFolder, 0, len(r.Folders.Folders))
		for _, raw := range r.Folders.Folders {
			var f sealedFolder
			if err := json.Unmarshal(raw, &f); err != nil {
				skip.Record(ctx, skip.KindFolder, "", skip.Malformed, err)
				continue
			}
			out = append(out, f)
		}
		since = r.Folders.LastToken
		return out, r.Folders.LastToken != "" && len(r.Folders.Folders) > 0, nil
	})
}

// vaultName is what a vault is called, read from its own record.
func (s *Service) vaultName(ctx context.Context, shareID string) (string, error) {
	shares, err := s.shares(ctx)
	if err != nil {
		return "", err
	}
	for _, sh := range shares {
		if sh.ShareID != shareID {
			continue
		}
		var v Vault
		if err := describe(ctx, s, sh, &v); err != nil {
			return "", err
		}
		return v.Name, nil
	}
	return "", &errs.NotFound{Kind: "vault", Ref: shareID}
}

// wrapping is the key an item's own key is sealed under where the item sits: the
// vault's, at the rotation named, at the top of a vault, and the folder's inside
// one. The folder is returned beside it, for an item that is in one.
func (s *Service) wrapping(ctx context.Context, shareID string, sk *shareKeys, folderID string, rotation int) ([]byte, *Folder, error) {
	if folderID != "" {
		vault, err := s.holdsFolders(ctx, shareID)
		if err != nil {
			return nil, nil, err
		}
		if !vault {
			folderID = ""
		}
	}
	if folderID == "" {
		key, ok := sk.keys[rotation]
		if !ok {
			return nil, nil, fmt.Errorf("the vault holds no key for rotation %d: %w", rotation, errNoKey)
		}
		return key, nil, nil
	}
	tree, err := s.folders(ctx, shareID)
	if err != nil {
		return nil, nil, err
	}
	folder, err := tree.folder(folderID)
	if err != nil {
		return nil, nil, err
	}
	return folder.key, folder, nil
}

// holdsFolders reports whether a share is a vault's, which is the only kind with
// folders: an item shared on its own opens with its share's key wherever its
// owner filed it.
func (s *Service) holdsFolders(ctx context.Context, shareID string) (bool, error) {
	shares, err := s.shares(ctx)
	if err != nil {
		return false, err
	}
	for _, sh := range shares {
		if sh.ShareID == shareID {
			return sh.Vault(), nil
		}
	}
	return true, nil
}

// skipReason is why an item whose key could not be found is missing.
func skipReason(err error) skip.Reason {
	if errors.Is(err, errNoKey) {
		return skip.NoKey
	}
	return skip.Unreadable
}

// keyAt is the key whatever is put somewhere new is sealed under, and the
// rotation it is written with: the vault's newest at the top of a vault, the
// folder's inside one. An item's key and a folder's are sealed the same way.
func (s *Service) keyAt(ctx context.Context, shareID, folderID string) ([]byte, int, error) {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return nil, 0, err
	}
	if folderID == "" {
		key, rotation := sk.latest()
		return key, rotation, nil
	}
	tree, err := s.folders(ctx, shareID)
	if err != nil {
		return nil, 0, err
	}
	folder, err := tree.folder(folderID)
	if err != nil {
		return nil, 0, err
	}
	return folder.key, folder.rotation, nil
}

// ── where an item sits ──

// place records which folder an item is in, so the key that opens it is found
// later in the run without asking again.
func (s *Service) place(shareID, itemID, folderID string) {
	s.placed.Store(shareID+"/"+itemID, folderID)
}

// itemFolder is the folder an item is in, or "" for one at the top of its vault.
func (s *Service) itemFolder(ctx context.Context, shareID, itemID string) (string, error) {
	if folderID, ok := s.placed.Load(shareID + "/" + itemID); ok {
		return folderID.(string), nil
	}
	head, err := s.itemHead(ctx, shareID, itemID)
	if err != nil {
		return "", err
	}
	return head.FolderID, nil
}

// ── reading ──

// FoldersList is the folders of the vault named, or of every vault that is not
// hidden, in path order.
func (s *Service) FoldersList(ctx context.Context, vaultFilter string) ([]Folder, error) {
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	var wanted []Vault
	for _, v := range vaults {
		if vaultFilter != "" && v.ShareID != vaultFilter && v.Name != vaultFilter {
			continue
		}
		if vaultFilter == "" && v.Hidden {
			continue
		}
		wanted = append(wanted, v)
	}
	if vaultFilter != "" && len(wanted) == 0 {
		return nil, &errs.NotFound{Kind: "vault", Ref: vaultFilter}
	}
	perVault := make([][]Folder, len(wanted))
	reads := make([]func(context.Context) error, len(wanted))
	for i, v := range wanted {
		reads[i] = func(ctx context.Context) error {
			tree, err := s.folders(ctx, v.ShareID)
			if err != nil {
				skip.Record(ctx, skip.KindVault, v.ShareID, skip.Unreadable, err)
				return nil
			}
			perVault[i] = tree.list()
			return nil
		}
	}
	_ = fetch.Together(ctx, reads...)
	var out []Folder
	for _, folders := range perVault {
		out = append(out, folders...)
	}
	slices.SortFunc(out, func(a, b Folder) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

// ResolveFolder finds one folder by its two IDs or by its path from its vault.
func (s *Service) ResolveFolder(ctx context.Context, reference string) (Folder, error) {
	if shareID, folderID, ok := folderIDs(reference); ok {
		tree, err := s.folders(ctx, shareID)
		if err != nil {
			return Folder{}, err
		}
		folder, err := tree.folder(folderID)
		if err != nil {
			return Folder{}, &errs.NotFound{Kind: "folder", Ref: reference}
		}
		return *folder, nil
	}
	matches, err := s.foldersAt(ctx, reference)
	if err != nil {
		return Folder{}, err
	}
	return ref.Pick("folder", reference, matches,
		func(f Folder) string { return f.ShareID + ref.Compound + f.FolderID },
		func(f Folder) string { return f.Path })
}

// folderIDs takes apart a reference that is a vault's ID and a folder's.
func folderIDs(reference string) (shareID, folderID string, ok bool) {
	parts, occurrence := ref.Split(reference)
	if len(parts) != 2 || occurrence != "" || !ref.Full(parts[0]) || !ref.Full(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// foldersAt are the folders whose path from their vault is the one given.
func (s *Service) foldersAt(ctx context.Context, path string) ([]Folder, error) {
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	var out []Folder
	for _, v := range vaults {
		if v.Name == "" || !strings.HasPrefix(strings.ToLower(path), strings.ToLower(v.Name)+"/") {
			continue
		}
		tree, err := s.folders(ctx, v.ShareID)
		if err != nil {
			return nil, err
		}
		for _, f := range tree.byID {
			if strings.EqualFold(f.Path, path) {
				out = append(out, *f)
			}
		}
	}
	return out, nil
}

// Place is where an item can be put: the top of a vault, or a folder in one.
type Place struct {
	ShareID  string
	FolderID string
	// Label is the place as a person names it: the vault's name, or the
	// folder's path from it.
	Label string
}

// ResolvePlace finds a vault or a folder: by its IDs, by the vault's name, or
// by the folder's path from its vault.
func (s *Service) ResolvePlace(ctx context.Context, reference string) (Place, error) {
	if shareID, folderID, ok := folderIDs(reference); ok {
		folder, err := s.ResolveFolder(ctx, shareID+ref.Compound+folderID)
		if err != nil {
			return Place{}, err
		}
		return folder.place(), nil
	}
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return Place{}, err
	}
	var places []Place
	for _, v := range vaults {
		if v.ShareID == reference || v.Name == reference {
			places = append(places, Place{ShareID: v.ShareID, Label: v.Name})
		}
	}
	folders, err := s.foldersAt(ctx, reference)
	if err != nil {
		return Place{}, err
	}
	for _, f := range folders {
		places = append(places, f.place())
	}
	return ref.Pick("vault or folder", reference, places,
		func(p Place) string { return p.Ref() },
		func(p Place) string { return p.Label })
}

func (f Folder) place() Place {
	return Place{ShareID: f.ShareID, FolderID: f.FolderID, Label: f.Path}
}

// Ref writes a place the way a reference names it: the vault alone, or the
// vault and the folder.
func (p Place) Ref() string {
	if p.FolderID == "" {
		return p.ShareID
	}
	return p.ShareID + ref.Compound + p.FolderID
}

// FolderContents is how many items and folders are inside a folder, at any
// depth, the trash included.
func (s *Service) FolderContents(ctx context.Context, folder Folder) (items, folders int, err error) {
	tree, err := s.folders(ctx, folder.ShareID)
	if err != nil {
		return 0, 0, err
	}
	inside := tree.below(folder.FolderID)
	all, err := s.itemsFull(ctx, folder.ShareID, everything, true)
	if err != nil {
		return 0, 0, err
	}
	for _, it := range all {
		if it.ShareID == folder.ShareID && inside[it.FolderID] {
			items++
		}
	}
	return items, len(inside) - 1, nil
}

// Below is a folder and every folder inside it.
func (s *Service) Below(ctx context.Context, folder Folder) (map[string]bool, error) {
	tree, err := s.folders(ctx, folder.ShareID)
	if err != nil {
		return nil, err
	}
	return tree.below(folder.FolderID), nil
}

// ── judging one before it is made ──

// CheckFiling refuses a plan that cannot put things in folders. Any plan can
// read and delete the folders there are.
func (s *Service) CheckFiling(ctx context.Context) error {
	limits, err := s.Limits(ctx)
	if err != nil {
		return err
	}
	return limits.Folders.filing()
}

// filing refuses a plan that cannot put things in folders.
func (l FolderLimits) filing() error {
	if !l.Allowed {
		return errs.Problemf("Folders need a paid Pass plan.")
	}
	return nil
}

// CheckFolder refuses a folder the plan or the vault has no room for, or whose
// name its siblings already use. exceptID is the folder being renamed, which
// may keep its own name.
func (s *Service) CheckFolder(ctx context.Context, shareID, parentID, name, exceptID string) error {
	limits, err := s.Limits(ctx)
	if err != nil {
		return err
	}
	if err := limits.Folders.filing(); err != nil {
		return err
	}
	tree, err := s.folders(ctx, shareID)
	if err != nil {
		return err
	}
	where := tree.vault
	parentDepth := 0
	if parentID != "" {
		parent, err := tree.folder(parentID)
		if err != nil {
			return err
		}
		where, parentDepth = parent.Path, parent.depth
	}
	siblings := tree.children(parentID)
	for _, f := range siblings {
		if f.FolderID != exceptID && strings.EqualFold(f.Name, name) {
			return errs.Naming(f.Path, errs.Problemf("%s already exists.", f.Path))
		}
	}
	if exceptID != "" {
		return nil
	}
	switch l := limits.Folders; {
	case len(tree.byID)+len(tree.shut) >= l.MaxCount:
		return errs.Naming(tree.vault, errs.Problemf("%s already holds %d folders, the most a vault may.",
			tree.vault, l.MaxCount))
	case len(siblings) >= l.MaxChildren:
		return errs.Naming(where, errs.Problemf("%s already holds %d folders, the most one place may.",
			where, l.MaxChildren))
	case parentDepth >= l.MaxDepth:
		return errs.Naming(where, errs.Problemf("Folders go %d deep at most, and %s is that deep already.",
			l.MaxDepth, where))
	}
	return nil
}

// FolderLimits are what the plan allows of folders.
type FolderLimits struct {
	// Allowed is whether the plan lets folders be made, renamed and filled. Any
	// plan can read and delete the ones there are.
	Allowed     bool
	MaxCount    int
	MaxChildren int
	MaxDepth    int
}

// The limits Proton applies when the plan names none.
const (
	defaultFolderCount    = 100
	defaultFolderChildren = 10
	defaultFolderDepth    = 5
)

// ── changing them ──

// FolderCreate makes a folder at the top of a vault, or inside another folder.
func (s *Service) FolderCreate(ctx context.Context, shareID, parentID, name string) (Folder, error) {
	wrap, rotation, err := s.keyAt(ctx, shareID, parentID)
	if err != nil {
		return Folder{}, err
	}
	created, err := s.createFolder(ctx, shareID, parentID, wrap, rotation, name)
	if err != nil {
		return Folder{}, err
	}
	tree, err := s.folders(ctx, shareID)
	if err != nil {
		return Folder{}, err
	}
	created.Vault, created.depth = tree.vault, 1
	if parent, ok := tree.byID[parentID]; ok {
		created.depth = parent.depth + 1
	}
	tree.byID[created.FolderID] = &created
	created.Path = tree.path(&created)
	return created, nil
}

// createFolder seals a new folder under wrap, the key of what will hold it, and
// stores it. The folder that comes back carries its own key, so what goes inside
// it next can be sealed without reading the vault's folders again.
func (s *Service) createFolder(ctx context.Context, shareID, parentID string, wrap []byte, rotation int, name string) (Folder, error) {
	key, err := aead.NewKey()
	if err != nil {
		return Folder{}, err
	}
	content, err := sealFolderName(key, name)
	if err != nil {
		return Folder{}, err
	}
	sealedKey, err := aead.Encrypt(wrap, key, []byte(aead.TagFolderKey))
	if err != nil {
		return Folder{}, err
	}
	var r struct{ Folder sealedFolder }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/share/" + shareID + "/folder",
		Body: map[string]any{
			"ParentFolderID":       folderOrNull(parentID),
			"ContentFormatVersion": folderContentVersion,
			"Content":              content,
			"KeyRotation":          rotation,
			"FolderKey":            base64.StdEncoding.EncodeToString(sealedKey),
		},
	}, &r); err != nil {
		return Folder{}, err
	}
	return Folder{
		ShareID: shareID, FolderID: r.Folder.FolderID, ParentID: parentID, Name: name,
		key: key, rotation: rotation,
	}, nil
}

// FolderRename gives a folder a new name.
func (s *Service) FolderRename(ctx context.Context, folder Folder, name string) error {
	tree, err := s.folders(ctx, folder.ShareID)
	if err != nil {
		return err
	}
	opened, err := tree.folder(folder.FolderID)
	if err != nil {
		return err
	}
	content, err := sealFolderName(opened.key, name)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/pass/v1/share/" + folder.ShareID + "/folder/" + folder.FolderID,
		Body: map[string]any{"Content": map[string]any{
			"ContentFormatVersion": folderContentVersion,
			"Content":              content,
			"KeyRotation":          opened.rotation,
		}},
	}, nil)
}

// FolderDelete deletes folders of one vault, with everything inside them.
func (s *Service) FolderDelete(ctx context.Context, shareID string, folderIDs []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/pass/v1/share/" + shareID + "/folder",
		Body: map[string]any{"FolderIDs": folderIDs},
	}, nil)
}

// sealFolderName seals what a folder holds of its own - its name - under its key.
func sealFolderName(key []byte, name string) (string, error) {
	plain, err := proto.Marshal(&pb.Folder{Name: name})
	if err != nil {
		return "", err
	}
	sealed, err := aead.Encrypt(key, plain, []byte(aead.TagFolderContent))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}
