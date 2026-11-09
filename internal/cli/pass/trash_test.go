package pass

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/crypto/aead"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/proton"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"github.com/roman-16/proton-cli/internal/ui"
	"google.golang.org/protobuf/proto"
)

// vaultsDoer is an account with two vaults, Work and Home, each holding one
// note in its trash and one out of it, and records the item pages it served.
type vaultsDoer struct {
	shares []map[string]any
	keys   map[string]string
	items  map[string][]map[string]any
	mu     sync.Mutex
	read   []string
}

func (d *vaultsDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (d *vaultsDoer) Decode(_ context.Context, r proton.Request, out any) error {
	var body any
	parts := strings.Split(strings.TrimPrefix(r.Path, "/pass/v1/"), "/")
	switch {
	case r.Path == "/pass/v1/share":
		body = map[string]any{"Shares": d.shares}
	case len(parts) == 3 && parts[0] == "share" && parts[2] == "key":
		body = map[string]any{"ShareKeys": map[string]any{"Keys": []map[string]any{{"Key": d.keys[parts[1]], "KeyRotation": 1}}}}
	case len(parts) == 3 && parts[0] == "share" && parts[2] == "item":
		d.mu.Lock()
		d.read = append(d.read, parts[1])
		d.mu.Unlock()
		body = map[string]any{"Items": map[string]any{"RevisionsData": d.items[parts[1]]}}
	default:
		return fmt.Errorf("nothing to answer %s %s with", r.Method, r.Path)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func sealedNote(t *testing.T, vault []byte, id, name string, state int) map[string]any {
	t.Helper()
	itemKey := bytes.Repeat([]byte{byte(len(id))}, 32)
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

// trashCommand runs `trash ARGS` against a two-vault account and returns what
// it printed, with the vaults whose items it read.
func trashCommand(t *testing.T, args ...string) (stdout string, read []string, err error) {
	t.Helper()
	key, err := pgphelper.GenerateKey("owner", "owner@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatal(err)
	}
	d := &vaultsDoer{keys: map[string]string{}, items: map[string][]map[string]any{}}
	for i, name := range []string{"Work", "Home"} {
		id := strings.ToLower(name)
		vault := bytes.Repeat([]byte{byte(i + 1)}, 32)
		sealedKey, err := pgphelper.EncryptBinary(ring, ring, vault)
		if err != nil {
			t.Fatal(err)
		}
		d.keys[id] = base64.StdEncoding.EncodeToString(sealedKey.Bytes())
		stored, err := proto.Marshal(&pb.Vault{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		sealedName, err := aead.Encrypt(vault, stored, []byte(aead.TagVaultContent))
		if err != nil {
			t.Fatal(err)
		}
		d.shares = append(d.shares, map[string]any{
			"ShareID": id, "TargetType": 1, "Owner": true, "ContentKeyRotation": 1,
			"Content": base64.StdEncoding.EncodeToString(sealedName),
		})
		d.items[id] = []map[string]any{
			sealedNote(t, vault, id+"-kept", name+" note", 1),
			sealedNote(t, vault, id+"-gone", name+" discarded", 2),
		}
	}
	unlocked := &keys.Unlocked{UserKR: ring}
	var out bytes.Buffer
	a := &app.App{
		Pass: passsvc.New(d, func(context.Context) (*keys.Unlocked, error) { return unlocked, nil }),
		UI:   ui.New(ui.Options{Format: ui.FormatText, Out: &out, Err: &bytes.Buffer{}, Width: 200}),
	}
	cmd := trashCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err = cmd.ExecuteContext(app.WithApp(context.Background(), a))
	return out.String(), d.read, err
}

// A trash listing names the vault it is about, by name or by ID, and reads that
// vault alone; without one it covers every vault, as it always has.
func TestTrashListShowsOneVaultsTrash(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		want     []string
		not      []string
		fetching int
	}{
		{"no vault", []string{"list"}, []string{"Work discarded", "Home discarded"}, []string{"Work note", "Home note"}, 2},
		{"a vault by name", []string{"list", "--vault", "Home"}, []string{"Home discarded"}, []string{"Work", "Home note"}, 1},
		{"a vault by ID", []string{"list", "--vault", "work"}, []string{"Work discarded"}, []string{"Home", "Work note"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, read, err := trashCommand(t, tc.args...)
			if err != nil {
				t.Fatalf("trash %v: %v", tc.args, err)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("the listing lacks %q:\n%s", w, out)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(out, n) {
					t.Errorf("the listing holds %q:\n%s", n, out)
				}
			}
			if len(read) != tc.fetching {
				t.Errorf("read the items of %v, want %d vault(s)", read, tc.fetching)
			}
		})
	}
}

// Restoring everything and emptying the trash take no vault: they act on the
// whole trash.
func TestOnlyTrashListTakesAVault(t *testing.T) {
	for _, args := range [][]string{{"restore", "--all", "--vault", "Work"}, {"empty", "--vault", "Work"}} {
		_, _, err := trashCommand(t, args...)
		if err == nil || !strings.Contains(err.Error(), "unknown flag: --vault") {
			t.Errorf("trash %v: err = %v, want the flag refused", args, err)
		}
	}
}

// A vault that does not exist is refused rather than listed as an empty trash,
// and none of the account's items are read to find that out.
func TestTrashListRefusesAVaultThatDoesNotExist(t *testing.T) {
	out, read, err := trashCommand(t, "list", "--vault", "Nowhere")
	if err == nil {
		t.Fatalf("trash list --vault Nowhere succeeded:\n%s", out)
	}
	if !strings.Contains(err.Error(), "Nowhere") {
		t.Errorf("the error does not name the vault: %v", err)
	}
	if out != "" || len(read) != 0 {
		t.Errorf("printed %q and read %v, want neither", out, read)
	}
}
