package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/passfile"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"github.com/roman-16/proton-cli/internal/skip"
)

// folderDoer answers for one vault's folders and the plan, and records what
// was sent.
type folderDoer struct {
	folders []map[string]any
	plan    map[string]any
	mu      sync.Mutex
	sent    []proton.Request
}

func (d *folderDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (d *folderDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.mu.Lock()
	d.sent = append(d.sent, r)
	d.mu.Unlock()
	var body any
	switch {
	case r.Method == "GET" && r.Path == "/pass/v1/share/work/folder":
		body = map[string]any{"Folders": map[string]any{"Folders": d.folders}}
	case r.Method == "GET" && r.Path == "/pass/v1/user/access":
		body = map[string]any{"Access": map[string]any{"Plan": d.plan}}
	case r.Method == "POST" && r.Path == "/pass/v1/share/work/folder":
		body = map[string]any{"Folder": map[string]any{"FolderID": fmt.Sprintf("made-%d", len(d.sent))}}
	default:
		return fmt.Errorf("nothing to answer %s %s with", r.Method, r.Path)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// sealFolder is a folder as Proton lists it, sealed under wrap.
func sealFolder(t *testing.T, wrap, key []byte, id, parent, name string) map[string]any {
	t.Helper()
	content, err := sealFolderName(key, name)
	if err != nil {
		t.Fatal(err)
	}
	sealedKey, err := aead.Encrypt(wrap, key, []byte(aead.TagFolderKey))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"FolderID": id, "ParentFolderID": parent, "KeyRotation": 1,
		"FolderKey": base64.StdEncoding.EncodeToString(sealedKey), "Content": content,
	}
}

func testFolderKey(seed byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = seed + byte(i)
	}
	return key
}

// folderService is a service holding one vault, Personal, whose folders are
// Banking, Cards inside it, and Broken - whose key will not open - with Inner
// inside that.
func folderService(t *testing.T) (*Service, *folderDoer) {
	t.Helper()
	vault := vaultKey()
	banking, cards := testFolderKey(40), testFolderKey(80)
	d := &folderDoer{
		folders: []map[string]any{
			sealFolder(t, banking, cards, "cards", "banking", "Cards"),
			sealFolder(t, vault, banking, "banking", "", "Banking"),
			sealFolder(t, testFolderKey(120), testFolderKey(160), "broken", "", "Broken"),
			sealFolder(t, testFolderKey(160), testFolderKey(200), "inner", "broken", "Inner"),
		},
		plan: map[string]any{"FolderAllowed": true, "FolderMaxCount": 6, "FolderMaxChildren": 3, "FolderMaxDepth": 2},
	}
	s := New(d, testKeys(nil))
	_, _ = s.shareKeys.Do("work", func() (*shareKeys, error) {
		return &shareKeys{keys: map[int][]byte{1: vault}}, nil
	})
	_, _ = s.shareList.Do("", func() ([]Share, error) {
		return []Share{{
			ShareID: "work", TargetType: targetVault, Owner: true,
			Content: storedVault(t, vault, &pb.Vault{Name: "Personal"}), ContentKeyRotation: 1,
		}}, nil
	})
	return s, d
}

// A folder inside another opens with its parent's key, wherever the list puts
// it, and is named by its path from the vault.
func TestAFolderOpensThroughTheFoldersAboveIt(t *testing.T) {
	s, _ := folderService(t)
	tree, err := s.folders(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range tree.list() {
		paths = append(paths, f.Path)
	}
	if got, want := strings.Join(paths, ","), "Personal/Banking,Personal/Banking/Cards"; got != want {
		t.Errorf("the folders came back as %s, want %s", got, want)
	}
}

// A folder that will not open is counted once, and what is inside it - folders
// and items - goes with it rather than being counted again.
func TestAFolderThatWillNotOpenTakesWhatIsInsideWithIt(t *testing.T) {
	s, _ := folderService(t)
	ctx, tally := skip.With(context.Background())
	sk, err := s.decryptShareKeys(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.wrapping(ctx, "work", sk, "inner", 1); !errors.Is(err, errFolderShut) {
		t.Errorf("an item in a folder inside a broken one came back with %v", err)
	}
	if tally.Count() != 1 || !tally.Hides() {
		t.Errorf("counted %d, hiding %v; want the one broken folder, hiding its contents",
			tally.Count(), tally.Hides())
	}
	key, folder, err := s.wrapping(ctx, "work", sk, "cards", 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(key) != string(testFolderKey(80)) || folder.Path != "Personal/Banking/Cards" {
		t.Errorf("an item in Cards opens with the wrong key, or is placed at %q", folder.Path)
	}
}

// A path is matched whatever its case, and a folder ID beside its vault's is
// found as it is.
func TestAFolderIsFoundByItsPathOrItsIDs(t *testing.T) {
	s, _ := folderService(t)
	got, err := s.ResolveFolder(context.Background(), "personal/banking/cards")
	if err != nil {
		t.Fatal(err)
	}
	if got.FolderID != "cards" {
		t.Errorf("the path found %s", got.FolderID)
	}
	if _, err := s.ResolveFolder(context.Background(), "Personal/Cards"); err == nil {
		t.Error("a folder was found at a path it is not at")
	}
}

// A vault and a folder are both places an item can go, and the vault alone is
// its top.
func TestAPlaceIsAVaultOrAFolder(t *testing.T) {
	s, _ := folderService(t)
	top, err := s.ResolvePlace(context.Background(), "Personal")
	if err != nil {
		t.Fatal(err)
	}
	if top.ShareID != "work" || top.FolderID != "" {
		t.Errorf("the vault resolved to %+v", top)
	}
	in, err := s.ResolvePlace(context.Background(), "Personal/Banking")
	if err != nil {
		t.Fatal(err)
	}
	if in.FolderID != "banking" || in.Label != "Personal/Banking" {
		t.Errorf("the folder resolved to %+v", in)
	}
}

// What the plan or the place has no room for is refused before anything is sent.
func TestAFolderIsCheckedBeforeItIsMade(t *testing.T) {
	for _, tc := range []struct {
		name, parent, folder, except string
		plan                         map[string]any
		want                         string
	}{
		{name: "a plan without folders", folder: "Taxes",
			plan: map[string]any{"FolderAllowed": false}, want: "need a paid Pass plan"},
		{name: "a name the place already has", parent: "banking", folder: "cards", want: "already exists"},
		{name: "a folder as deep as folders go", parent: "cards", folder: "Visa", want: "deep at most"},
		{name: "a rename to its own name", parent: "banking", folder: "Cards", except: "cards"},
		{name: "room to spare", folder: "Taxes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d := folderService(t)
			if tc.plan != nil {
				d.plan = tc.plan
			}
			err := s.CheckFolder(context.Background(), "work", tc.parent, tc.folder, tc.except)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("got %v, want a refusal saying %q", err, tc.want)
			}
			var problem *errs.Problem
			if err != nil && !errors.As(err, &problem) {
				t.Errorf("the refusal %v is not phrased for a person", err)
			}
		})
	}
}

// A folder the vault already has takes an import's items rather than being
// made twice, and one the vault has no room for sends its items, and whatever
// was to go inside it, to the folder above.
func TestAnImportFilesIntoTheFoldersThereAreAndMakesTheRest(t *testing.T) {
	s, _ := folderService(t)
	entry := func(folder ...string) passfile.Entry { return passfile.Entry{Name: "x", Folder: folder} }
	pv := &PlannedVault{Name: "Personal", ShareID: "work"}
	filing, err := s.planFolders(context.Background(), pv, []passfile.Entry{
		entry("banking", "Cards"),
		entry("Taxes"),
		entry("Taxes", "2026", "Q1"),
		entry(),
	}, FolderLimits{Allowed: true, MaxCount: 10, MaxChildren: 10, MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := filing.spot([]string{"banking", "Cards"}); got.FolderID != "cards" {
		t.Errorf("Banking/Cards was not filed in the folder the vault has: %+v", got)
	}
	var made []string
	for _, f := range pv.Folders {
		made = append(made, f.Name)
	}
	if got, want := strings.Join(made, ","), "Taxes,2026"; got != want {
		t.Errorf("made %s, want %s", got, want)
	}
	if got := filing.path(filing.spot([]string{"Taxes", "2026", "Q1"})); got != "Personal/Taxes/2026" {
		t.Errorf("an item too deep lands in %q, want the deepest folder made above it", got)
	}
	if got := filing.spot(nil); got != (importSpot{}) {
		t.Errorf("an item in no folder lands in %+v, want the top", got)
	}
	if warnings := filing.warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "1 folder was folded") {
		t.Errorf("the warnings are %q", warnings)
	}
}

// Without folders on the plan, a folder is a vault of its own, named by its
// path - or, when everything goes into one vault, nothing at all.
func TestAPlanWithoutFoldersFilesByVault(t *testing.T) {
	doc := &passfile.Document{Vaults: []passfile.Vault{{Items: []passfile.Entry{
		{Name: "a"}, {Name: "b", Folder: []string{"Work", "Clients"}},
	}}}}
	split := foldersAsVaults(doc, true)
	var names []string
	for _, v := range split.Vaults {
		names = append(names, v.Name)
	}
	if got, want := strings.Join(names, ","), ",Work/Clients"; got != want {
		t.Errorf("split into %q, want %q", got, want)
	}
	kept := foldersAsVaults(doc, false)
	if len(kept.Vaults) != 1 || len(kept.Vaults[0].Items) != 2 || kept.Vaults[0].Items[1].Folder != nil {
		t.Errorf("kept as %+v", kept.Vaults)
	}
}

// moveDoer answers for one item's keys and takes whatever move it is sent.
type moveDoer struct {
	keys []map[string]any
	sent []proton.Request
}

func (d *moveDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (d *moveDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.sent = append(d.sent, r)
	var body any
	switch {
	case r.Method == "GET" && r.Path == "/pass/v1/share/work/item/i1/key":
		body = map[string]any{"Keys": map[string]any{"Keys": d.keys}}
	case r.Method == "PUT" && r.Path == "/pass/v1/share/work/item/folder":
		body = map[string]any{"Items": []map[string]any{{"ItemID": "i1"}}}
	case r.Method == "PUT" && r.Path == "/pass/v1/share/work/item/share":
		body = map[string]any{"Items": []map[string]any{{"ItemID": "i2"}}}
	default:
		return fmt.Errorf("nothing to answer %s %s with", r.Method, r.Path)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// An item moved within its vault keeps its ID and goes by the folder endpoint;
// one moved to another vault comes back with a new ID. Either way every key it
// has had is sealed under where it goes, keeping the rotation it was made at.
func TestAMovedItemIsSealedUnderWhereItGoes(t *testing.T) {
	work, home, two := vaultKey(), testFolderKey(7), testFolderKey(9)
	itemOne, itemTwo := testFolderKey(11), testFolderKey(13)
	sealed := func(wrap, key []byte, rotation int) map[string]any {
		enc, err := aead.Encrypt(wrap, key, []byte(aead.TagItemKey))
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"Key": base64.StdEncoding.EncodeToString(enc), "KeyRotation": rotation}
	}
	for _, tc := range []struct {
		name, to, path, ref string
		wrap                []byte
	}{
		{"within the vault", "work", "/pass/v1/share/work/item/folder", "work/i1", two},
		{"to another vault", "home", "/pass/v1/share/work/item/share", "home/i2", home},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &moveDoer{keys: []map[string]any{sealed(work, itemOne, 1), sealed(two, itemTwo, 2)}}
			s := New(d, testKeys(nil))
			_, _ = s.shareKeys.Do("work", func() (*shareKeys, error) {
				return &shareKeys{keys: map[int][]byte{1: work, 2: two}}, nil
			})
			_, _ = s.shareKeys.Do("home", func() (*shareKeys, error) {
				return &shareKeys{keys: map[int][]byte{1: home}}, nil
			})
			_, _ = s.shareList.Do("", func() ([]Share, error) {
				return []Share{{ShareID: "work", TargetType: targetVault}, {ShareID: "home", TargetType: targetVault}}, nil
			})
			moved, refused, err := s.ItemsMove(context.Background(),
				[]Item{{ShareID: "work", ItemID: "i1", Name: "GitHub"}}, Place{ShareID: tc.to})
			if err != nil || len(refused) > 0 {
				t.Fatalf("the move failed: %v %v", err, refused)
			}
			if len(moved) != 1 || moved[0] != tc.ref {
				t.Errorf("the item answers to %v, want %s", moved, tc.ref)
			}
			last := d.sent[len(d.sent)-1]
			if last.Path != tc.path {
				t.Fatalf("the move went to %s, want %s", last.Path, tc.path)
			}
			body, _ := json.Marshal(last.Body)
			var sent struct {
				Items []struct {
					ItemKeys []struct {
						Key         string
						KeyRotation int
					}
				}
			}
			if err := json.Unmarshal(body, &sent); err != nil || len(sent.Items) != 1 {
				t.Fatalf("the move sent %s", body)
			}
			want := map[int][]byte{1: itemOne, 2: itemTwo}
			for _, k := range sent.Items[0].ItemKeys {
				raw, _ := base64.StdEncoding.DecodeString(k.Key)
				key, err := aead.Decrypt(tc.wrap, raw, []byte(aead.TagItemKey))
				if err != nil || string(key) != string(want[k.KeyRotation]) {
					t.Errorf("the key of rotation %d does not open with where the item went", k.KeyRotation)
				}
				delete(want, k.KeyRotation)
			}
			if len(want) > 0 {
				t.Errorf("rotations %v were not sent", want)
			}
		})
	}
}
