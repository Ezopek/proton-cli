package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"google.golang.org/protobuf/proto"
)

// keyDoer answers the item-key endpoint from a fixture and records what was asked.
type keyDoer struct {
	body  []byte
	err   error
	paths []string
}

func (d *keyDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (d *keyDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.paths = append(d.paths, r.Path)
	if d.err != nil {
		return d.err
	}
	return json.Unmarshal(d.body, out)
}

func latestKeyJSON(t *testing.T, shareKey, itemKey []byte, rotation int) []byte {
	t.Helper()
	wrapped, err := aead.Encrypt(shareKey, itemKey, []byte(aead.TagItemKey))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]any{"Key": map[string]any{
		"Key":         base64.StdEncoding.EncodeToString(wrapped),
		"KeyRotation": rotation,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The rotation a write sends has to name the key the content was encrypted with.
// Fetching the newest rotation but encrypting with an older key labels the
// ciphertext with a key that cannot open it.
func TestLatestItemKeyReturnsTheKeyItOpenedAndItsOwnRotation(t *testing.T) {
	shareKey := vaultKey()
	itemKey := make([]byte, 32)
	for i := range itemKey {
		itemKey[i] = byte(255 - i)
	}
	d := &keyDoer{body: latestKeyJSON(t, shareKey, itemKey, 4)}
	sk := &shareKeys{keys: map[int][]byte{2: make([]byte, 32), 4: shareKey}}

	got, rotation, err := New(d, testKeys(nil)).latestItemKey(context.Background(), sk, "share", "item")
	if err != nil {
		t.Fatal(err)
	}
	if rotation != 4 {
		t.Errorf("rotation = %d, want the one the key came with", rotation)
	}
	if string(got) != string(itemKey) {
		t.Error("the key returned is not the key that was wrapped")
	}
	// Whatever the key opens has to open again with the same key, which is the
	// property the rotation is a claim about.
	ct, err := aead.Encrypt(got, []byte("content"), []byte(aead.TagItemContent))
	if err != nil {
		t.Fatal(err)
	}
	back, err := aead.Decrypt(itemKey, ct, []byte(aead.TagItemContent))
	if err != nil || string(back) != "content" {
		t.Errorf("content encrypted with the returned key does not open with it: %v", err)
	}
}

func TestLatestItemKeyRefusesWhatItCannotOpen(t *testing.T) {
	shareKey := vaultKey()
	itemKey := make([]byte, 32)

	t.Run("a rotation the share has no key for", func(t *testing.T) {
		d := &keyDoer{body: latestKeyJSON(t, shareKey, itemKey, 9)}
		sk := &shareKeys{keys: map[int][]byte{1: shareKey}}
		if _, _, err := New(d, testKeys(nil)).latestItemKey(context.Background(), sk, "share", "item"); err == nil {
			t.Error("accepted a rotation with no matching share key")
		}
	})

	t.Run("a key wrapped to a different share key", func(t *testing.T) {
		d := &keyDoer{body: latestKeyJSON(t, shareKey, itemKey, 1)}
		sk := &shareKeys{keys: map[int][]byte{1: make([]byte, 32)}}
		if _, _, err := New(d, testKeys(nil)).latestItemKey(context.Background(), sk, "share", "item"); err == nil {
			t.Error("accepted a key it could not open")
		}
	})

	t.Run("a key that is not base64", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"Key": map[string]any{"Key": "not base64!!", "KeyRotation": 1}})
		d := &keyDoer{body: body}
		sk := &shareKeys{keys: map[int][]byte{1: shareKey}}
		if _, _, err := New(d, testKeys(nil)).latestItemKey(context.Background(), sk, "share", "item"); err == nil {
			t.Error("accepted a key that is not base64")
		}
	})

	t.Run("a request that failed", func(t *testing.T) {
		// Discarding the error would leave the rotation at zero, which labels the
		// ciphertext with a key that cannot open it.
		d := &keyDoer{err: context.DeadlineExceeded}
		sk := &shareKeys{keys: map[int][]byte{1: shareKey}}
		if _, _, err := New(d, testKeys(nil)).latestItemKey(context.Background(), sk, "share", "item"); err == nil {
			t.Error("a failed request was treated as an answer")
		}
	})
}

// The identity fields are declared once and walked by the create path, the
// update path, the read path and the flag registration. If any of them stopped
// agreeing, an item would be writable in a field nothing could read back.
func TestIdentityFieldsRoundTripThroughTheDeclaration(t *testing.T) {
	values := make(map[string]string, len(IdentityFields))
	for i, f := range IdentityFields {
		values[f.Flag] = fmt.Sprintf("value-%d", i)
	}

	idn := buildIdentity(values)
	got := readIdentity(idn)
	if len(got) != len(IdentityFields) {
		t.Fatalf("wrote %d fields and read back %d", len(IdentityFields), len(got))
	}
	for flag, want := range values {
		if got[flag] != want {
			t.Errorf("%s round-tripped as %q, want %q", flag, got[flag], want)
		}
	}
}

// No two fields may share a flag, and none may point at the same place on the
// stored item - either would make one field silently overwrite another.
func TestIdentityFieldsAreDistinct(t *testing.T) {
	seenFlag := map[string]bool{}
	for _, f := range IdentityFields {
		if seenFlag[f.Flag] {
			t.Errorf("two identity fields both use --%s", f.Flag)
		}
		seenFlag[f.Flag] = true
		if f.Label == "" {
			t.Errorf("--%s has no label", f.Flag)
		}
	}
	// Two fields aliasing one storage slot: set each in turn and check nothing
	// else moved.
	for _, f := range IdentityFields {
		idn := buildIdentity(map[string]string{f.Flag: "only-this"})
		for _, other := range IdentityFields {
			if other.Flag == f.Flag {
				continue
			}
			if other.Get(idn) != "" {
				t.Errorf("setting --%s also set --%s; they share a storage slot",
					f.Flag, other.Flag)
			}
		}
	}
}

// A patch changes only what it names, so editing a job title leaves the address
// exactly as it was.
func TestPatchIdentityLeavesTheRestAlone(t *testing.T) {
	idn := buildIdentity(map[string]string{"first-name": "Jane", "city": "Vienna"})
	patchIdentity(idn, map[string]string{"city": "Graz"})

	got := readIdentity(idn)
	if got["city"] != "Graz" {
		t.Errorf("city = %q, want Graz", got["city"])
	}
	if got["first-name"] != "Jane" {
		t.Errorf("changing the city dropped the first name: %q", got["first-name"])
	}
}

// trashDoer serves each vault's items, one active and one in the trash, and
// records every request from whichever goroutine sent it.
type trashDoer struct {
	items map[string][]map[string]any
	mu    sync.Mutex
	paths []string
}

func (d *trashDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (d *trashDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.mu.Lock()
	d.paths = append(d.paths, r.Path)
	d.mu.Unlock()
	share, found := strings.CutPrefix(r.Path, "/pass/v1/share/")
	share, isItems := strings.CutSuffix(share, "/item")
	if !found || !isItems || r.Method != "GET" {
		return fmt.Errorf("nothing to answer %s %s with", r.Method, r.Path)
	}
	raw, err := json.Marshal(map[string]any{"Items": map[string]any{"RevisionsData": d.items[share]}})
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// asked says whether any request went to a path with this prefix.
func (d *trashDoer) asked(prefix string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.ContainsFunc(d.paths, func(p string) bool { return strings.HasPrefix(p, prefix) })
}

// sealedItem is a note as Proton lists it, sealed under the vault key.
func sealedItem(t *testing.T, vault []byte, id, name string, state int) map[string]any {
	t.Helper()
	itemKey := make([]byte, 32)
	for i := range itemKey {
		itemKey[i] = byte(len(id) + i)
	}
	plain, err := proto.Marshal(&pb.Item{
		Metadata: &pb.Metadata{Name: name},
		Content:  &pb.Content{Content: &pb.Content_Note{Note: &pb.ItemNote{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := aead.Encrypt(itemKey, plain, []byte(aead.TagItemContent))
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := aead.Encrypt(vault, itemKey, []byte(aead.TagItemKey))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"ItemID": id, "State": state, "KeyRotation": 1,
		"Content": base64.StdEncoding.EncodeToString(content),
		"ItemKey": base64.StdEncoding.EncodeToString(wrapped),
	}
}

// trashService holds three vaults - Work, Home, and Archive, which is hidden -
// each with one item in the trash and one out of it. named says whether the
// vaults carry names, and keyed which of them have a key already in hand.
func trashService(t *testing.T, named bool, keyed ...string) (*Service, *trashDoer) {
	t.Helper()
	vault := vaultKey()
	d := &trashDoer{items: map[string][]map[string]any{}}
	var shares []Share
	for _, v := range []struct {
		id, name string
		hidden   bool
	}{{"work", "Work", false}, {"home", "Home", false}, {"archive", "Archive", true}} {
		d.items[v.id] = []map[string]any{
			sealedItem(t, vault, v.id+"-kept", v.name+" kept", activeState),
			sealedItem(t, vault, v.id+"-gone", v.name+" trashed", trashedState),
		}
		sh := Share{ShareID: v.id, TargetType: targetVault, Owner: true, Hidden: v.hidden, ContentKeyRotation: 1}
		if named {
			sh.Content = storedVault(t, vault, &pb.Vault{Name: v.name})
		}
		shares = append(shares, sh)
	}
	s := New(d, testKeys(nil))
	_, _ = s.shareList.Do("", func() ([]Share, error) { return shares, nil })
	for _, id := range keyed {
		_, _ = s.shareKeys.Do(id, func() (*shareKeys, error) {
			return &shareKeys{keys: map[int][]byte{1: vault}}, nil
		})
	}
	return s, d
}

func namesOf(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	slices.Sort(out)
	return out
}

// With no vault named the trash is read from every vault that is not hidden,
// and only what is in the trash comes back.
func TestTheTrashIsReadFromEveryVaultThatIsNotHidden(t *testing.T) {
	s, d := trashService(t, true, "work", "home", "archive")
	got, err := s.ItemsTrashed(context.Background(), "")
	if err != nil {
		t.Fatalf("ItemsTrashed: %v", err)
	}
	if want := []string{"Home trashed", "Work trashed"}; !slices.Equal(namesOf(got), want) {
		t.Errorf("the trash held %v, want %v", namesOf(got), want)
	}
	if d.asked("/pass/v1/share/archive/") {
		t.Error("the hidden vault was read by a read across vaults")
	}
}

// A vault named by its ID or by its name is the only one whose items are
// fetched; the others are not asked for anything.
func TestTheTrashOfANamedVaultIsReadAlone(t *testing.T) {
	for _, tc := range []struct {
		name  string
		vault string
		named bool
		keyed []string
		want  []string
		read  string
	}{
		{"by ID", "home", false, []string{"home"}, []string{"Home trashed"}, "home"},
		{"by name", "Work", true, []string{"work", "home", "archive"}, []string{"Work trashed"}, "work"},
		{"a hidden vault by name", "Archive", true, []string{"work", "home", "archive"}, []string{"Archive trashed"}, "archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d := trashService(t, tc.named, tc.keyed...)
			got, err := s.ItemsTrashed(context.Background(), tc.vault)
			if err != nil {
				t.Fatalf("ItemsTrashed: %v", err)
			}
			if !slices.Equal(namesOf(got), tc.want) {
				t.Errorf("the trash held %v, want %v", namesOf(got), tc.want)
			}
			if !d.asked("/pass/v1/share/" + tc.read + "/item") {
				t.Errorf("the vault named was never read")
			}
			for _, other := range []string{"work", "home", "archive"} {
				if other != tc.read && d.asked("/pass/v1/share/"+other+"/") {
					t.Errorf("the vault %s was asked for something though %s was named", other, tc.read)
				}
			}
		})
	}
}

// A vault nobody has is refused, not listed as empty: an empty trash would read
// as a complete answer. Nothing is read.
func TestTheTrashOfAnUnknownVaultIsRefusedAndNothingIsRead(t *testing.T) {
	s, d := trashService(t, true, "work", "home", "archive")
	got, err := s.ItemsTrashed(context.Background(), "Nowhere")
	var missing *errs.NotFound
	if !errors.As(err, &missing) || missing.Kind != "vault" || missing.Ref != "Nowhere" {
		t.Fatalf("err = %v, want the vault reported as not found", err)
	}
	if len(got) != 0 {
		t.Errorf("the trash held %v, want nothing", namesOf(got))
	}
	if d.asked("/pass/v1/share/") {
		t.Error("an item page was fetched though no vault matched")
	}
}
