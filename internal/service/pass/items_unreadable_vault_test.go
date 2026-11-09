package pass

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"github.com/roman-16/proton-cli/internal/skip"
	"google.golang.org/protobuf/proto"
)

// twoVaultDoer answers the share list and the item listing of the vault "ok".
// Everything else - the keys of "bad", its items - is an error, which is how a
// vault that cannot be read looks from here.
type twoVaultDoer struct{ okItems []byte }

func (d *twoVaultDoer) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, nil
}

func (d *twoVaultDoer) Decode(_ context.Context, r proton.Request, out any) error {
	switch {
	case r.Method == "GET" && r.Path == "/pass/v1/share":
		return json.Unmarshal([]byte(`{"Shares":[
			{"ShareID":"bad","VaultID":"v1","TargetType":1,"Owner":true,"ShareRoleID":"1"},
			{"ShareID":"ok","VaultID":"v2","TargetType":1,"Owner":true,"ShareRoleID":"1"}
		]}`), out)
	case r.Method == "GET" && r.Path == "/pass/v1/share/ok/item":
		return json.Unmarshal(d.okItems, out)
	}
	return fmt.Errorf("nothing to answer %s %s with", r.Method, r.Path)
}

// sealedItems is the item listing of a vault holding one login, sealed the way
// Proton stores it: the content under a key of its own, that key under the vault's.
func sealedItems(t *testing.T, shareKey []byte, name string) []byte {
	t.Helper()
	itemKey := make([]byte, 32)
	for i := range itemKey {
		itemKey[i] = byte(100 + i)
	}
	plain, err := proto.Marshal(&pb.Item{
		Metadata: &pb.Metadata{Name: name, ItemUuid: "i1"},
		Content:  &pb.Content{Content: &pb.Content_Login{Login: &pb.ItemLogin{ItemUsername: "synthetic-user"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := aead.Encrypt(itemKey, plain, []byte(aead.TagItemContent))
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := aead.Encrypt(shareKey, itemKey, []byte(aead.TagItemKey))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"Items": map[string]any{"RevisionsData": []map[string]any{{
		"ItemID": "i1", "Revision": 1, "State": activeState, "KeyRotation": 1,
		"Content": base64.StdEncoding.EncodeToString(content), "ItemKey": base64.StdEncoding.EncodeToString(wrapped),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A vault whose keys cannot be opened, or whose items cannot be fetched, used to
// be left out of a listing without a word: the answer looked like that of an
// account with nothing in the vault, exit 0 and no count. It is skipped now - one
// vault, with the reason - and the vaults that can be read are still listed.
func TestAVaultThatCannotBeReadIsSkippedAndTheOthersAreStillListed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		badKeys bool
	}{
		{"its keys cannot be opened", false},
		{"its items cannot be fetched", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var records bytes.Buffer
			restore := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&records, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(restore) })

			key := vaultKey()
			s := New(&twoVaultDoer{okItems: sealedItems(t, key, "Kept")}, testKeys(&keys.Unlocked{}))
			_, _ = s.shareKeys.Do("ok", func() (*shareKeys, error) {
				return &shareKeys{keys: map[int][]byte{1: key}}, nil
			})
			if tc.badKeys {
				// The keys open; the listing behind them is what fails.
				_, _ = s.shareKeys.Do("bad", func() (*shareKeys, error) {
					return &shareKeys{keys: map[int][]byte{1: key}}, nil
				})
			}

			ctx, tally := skip.With(context.Background())
			items, err := s.itemsFull(ctx, "", browsed, false)
			if err != nil {
				t.Fatalf("itemsFull: %v", err)
			}
			if len(items) != 1 || items[0].Name != "Kept" {
				t.Errorf("items = %v, want the one in the vault that could be read", items)
			}
			if tally.Count() != 1 || tally.Kind() != skip.KindVault || !tally.Hides() {
				t.Errorf("tally: %d skipped (want 1), kind %q, hides %v", tally.Count(), tally.Kind(), tally.Hides())
			}
			if got, want := records.String(), "kind=vault reason=unreadable ref=bad"; !strings.Contains(got, want) {
				t.Errorf("the log does not say why the vault was left out; want %q in:\n%s", want, got)
			}
		})
	}
}
