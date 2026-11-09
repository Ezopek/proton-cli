package pass

import (
	"bytes"
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
	"google.golang.org/protobuf/encoding/protowire"
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

// ── editing one item's password and note ──
//
// ItemEdit is the vendor-client boundary of an edit: it opens the stored item, changes what the patch names, and seals the
// whole item again. These tests run the real ItemEdit and the real protobuf and AEAD code against a stand-in for Proton's
// three item endpoints, then open what was written with the item's own key. Nothing here is a secret.

const (
	editShare = "S"
	editItem  = "I"
)

// editDoer plays Proton's item endpoints for one item and keeps the one write it is sent.
type editDoer struct {
	t       *testing.T
	getItem []byte
	latest  []byte
	calls   []string
	put     map[string]any
}

func (d *editDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (d *editDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.calls = append(d.calls, r.Method+" "+r.Path)
	switch {
	case r.Method == "GET" && r.Path == fmt.Sprintf("/pass/v1/share/%s/item/%s/key/latest", editShare, editItem):
		return json.Unmarshal(d.latest, out)
	case r.Method == "GET" && r.Path == fmt.Sprintf("/pass/v1/share/%s/item/%s", editShare, editItem):
		return json.Unmarshal(d.getItem, out)
	case r.Method == "PUT" && r.Path == fmt.Sprintf("/pass/v1/share/%s/item/%s", editShare, editItem):
		body, ok := r.Body.(map[string]any)
		if !ok {
			d.t.Fatalf("the write body is a %T", r.Body)
		}
		d.put = body
		return json.Unmarshal([]byte(`{"Item":{"Revision":4}}`), out)
	}
	d.t.Fatalf("an edit asked for %s %s, which it should not need", r.Method, r.Path)
	return nil
}

// editFixture stores original as the item and returns a service over it, with the share's keys already open (a real run opens
// them with the account's key) and the item's own key, so that what was written can be opened again.
func editFixture(t *testing.T, original *pb.Item) (*Service, *editDoer, []byte) {
	t.Helper()
	shareKey := vaultKey()
	itemKey := make([]byte, 32)
	for i := range itemKey {
		itemKey[i] = byte(200 - i)
	}
	plain, err := proto.Marshal(original)
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
	get, err := json.Marshal(map[string]any{"Item": map[string]any{
		"Revision": 3, "KeyRotation": 1, "FolderID": "",
		"Content": base64.StdEncoding.EncodeToString(content), "ItemKey": base64.StdEncoding.EncodeToString(wrapped),
	}})
	if err != nil {
		t.Fatal(err)
	}
	d := &editDoer{t: t, getItem: get, latest: latestKeyJSON(t, shareKey, itemKey, 1)}
	s := New(d, testKeys(nil))
	if _, err := s.shareKeys.Do(editShare, func() (*shareKeys, error) {
		return &shareKeys{keys: map[int][]byte{1: shareKey}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, d, itemKey
}

// written opens the one write the edit made with the item's key and reads it as a protobuf.
func written(t *testing.T, d *editDoer, itemKey []byte) (*pb.Item, []byte) {
	t.Helper()
	if d.put == nil {
		t.Fatalf("no write was made; calls: %v", d.calls)
	}
	encoded, ok := d.put["Content"].(string)
	if !ok {
		t.Fatal("the write carries no content")
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := aead.Decrypt(itemKey, sealed, []byte(aead.TagItemContent))
	if err != nil {
		t.Fatalf("the written content does not open with the item's key: %v", err)
	}
	var it pb.Item
	if err := proto.Unmarshal(plain, &it); err != nil {
		t.Fatal(err)
	}
	return &it, sealed
}

// withUnknown puts a field this program has no name for on a message, as a newer client would have.
func withUnknown(m proto.Message, number protowire.Number, payload string) {
	raw := m.ProtoReflect().GetUnknown()
	raw = protowire.AppendTag(raw, number, protowire.BytesType)
	raw = protowire.AppendString(raw, payload)
	m.ProtoReflect().SetUnknown(raw)
}

func unknownOf(m proto.Message) []byte { return m.ProtoReflect().GetUnknown() }

// richLogin is a login that carries everything an edit of its password and note must not disturb: a username and an email,
// two addresses, a TOTP URI, a passkey, custom fields of every kind including ones whose names collide with the login's own
// (two called password, one called note, a field with no name) - and, at every level, fields this program does not know.
func richLogin() *pb.Item {
	passkey := &pb.Passkey{KeyId: "key-1", Content: []byte{0, 1, 2, 255}, Domain: "example.invalid", RpId: "example.invalid",
		RpName: "Synthetic", UserName: "synthetic-user", UserId: []byte{9, 8}, CredentialId: []byte{1, 1, 1}, UserHandle: []byte{7}}
	login := &pb.ItemLogin{
		ItemUsername: "user@example.invalid", ItemEmail: "mail@example.invalid", Password: "old-password",
		Urls:     []string{"https://one.example.invalid", "https://two.example.invalid"},
		TotpUri:  "otpauth://totp/Synthetic:me?secret=JBSWY3DPEHPK3PXP&issuer=Synthetic&digits=8&period=45",
		Passkeys: []*pb.Passkey{passkey},
	}
	hidden := &pb.ExtraField{FieldName: "password", Content: &pb.ExtraField_Hidden{Hidden: &pb.ExtraHiddenField{Content: "hidden one"}}}
	item := &pb.Item{
		Metadata: &pb.Metadata{Name: "Synthetic login", Note: "old note", ItemUuid: "uuid-synthetic"},
		Content:  &pb.Content{Content: &pb.Content_Login{Login: login}},
		ExtraFields: []*pb.ExtraField{
			hidden,
			{FieldName: "password", Content: &pb.ExtraField_Text{Text: &pb.ExtraTextField{Content: "text two"}}},
			{FieldName: "note", Content: &pb.ExtraField_Text{Text: &pb.ExtraTextField{Content: "custom note"}}},
			{FieldName: "totp", Content: &pb.ExtraField_Totp{Totp: &pb.ExtraTotp{TotpUri: "otpauth://totp/Extra?secret=KRSXG5CTMVRXEZLU"}}},
			{FieldName: "", Content: &pb.ExtraField_Text{Text: &pb.ExtraTextField{Content: ""}}},
		},
	}
	withUnknown(item, 5001, "unknown on the item")
	withUnknown(item.Metadata, 5002, "unknown on the metadata")
	withUnknown(item.Content, 5003, "unknown on the content")
	withUnknown(login, 5004, "unknown on the login")
	withUnknown(passkey, 5005, "unknown on the passkey")
	withUnknown(hidden, 5006, "unknown on a custom field")
	return item
}

func edited(t *testing.T, original *pb.Item, patch Patch) (*pb.Item, []byte, *editDoer, []byte) {
	t.Helper()
	s, d, itemKey := editFixture(t, original)
	if err := s.ItemEdit(context.Background(), editShare, editItem, patch); err != nil {
		t.Fatalf("ItemEdit: %v", err)
	}
	got, sealed := written(t, d, itemKey)
	return got, sealed, d, itemKey
}

// Editing the password and the note changes exactly those two values. Everything else the login holds - the other scalars, the
// addresses, the TOTP URI to the byte, the passkey, every custom field in its order with its kind, and every field this program
// cannot name - is written back as it was read.
func TestEditingThePasswordAndNoteLeavesEverythingElseOfALoginAlone(t *testing.T) {
	for name, tc := range map[string]struct{ password, note string }{
		"plain":              {"new-password", "new note"},
		"whitespace and NUL": {"  pad\x00ded\n", "\n\tmulti\r\nline\n  "},
		"unicode":            {"p\u00e4ssw\u00f6rd\u65e5\u672c\u8a9e\U0001F510", "za\u017c\u00f3\u0142\u0107\n\u202eRTL\u202c\ne\u0301 "},
		"only the password":  {"only-password", ""},
		"only the note":      {"", "only note"},
		"the old values":     {"old-password", "old note"},
	} {
		t.Run(name, func(t *testing.T) {
			original := richLogin()
			got, sealed, d, _ := edited(t, original, Patch{Scalars: Scalars{Password: tc.password, Note: tc.note}})

			want := proto.Clone(original).(*pb.Item)
			if tc.password != "" {
				want.GetContent().GetLogin().Password = tc.password
			}
			if tc.note != "" {
				want.GetMetadata().Note = tc.note
			}
			if !proto.Equal(want, got) {
				t.Errorf("the item written is not the item read with only the patched values replaced:\n got %v\nwant %v", got, want)
			}

			login, before := got.GetContent().GetLogin(), original.GetContent().GetLogin()
			if tc.password != "" && login.GetPassword() != tc.password {
				t.Errorf("password = %q, want %q", login.GetPassword(), tc.password)
			}
			if tc.note != "" && got.GetMetadata().GetNote() != tc.note {
				t.Errorf("note = %q, want %q", got.GetMetadata().GetNote(), tc.note)
			}
			if login.GetTotpUri() != before.GetTotpUri() {
				t.Errorf("the TOTP URI changed: %q", login.GetTotpUri())
			}
			if login.GetItemUsername() != before.GetItemUsername() || login.GetItemEmail() != before.GetItemEmail() ||
				got.GetMetadata().GetName() != "Synthetic login" || got.GetMetadata().GetItemUuid() != "uuid-synthetic" {
				t.Error("a scalar that was not patched changed")
			}
			if fmt.Sprint(login.GetUrls()) != fmt.Sprint(before.GetUrls()) {
				t.Errorf("the addresses changed: %v", login.GetUrls())
			}
			if len(login.GetPasskeys()) != 1 || !proto.Equal(login.GetPasskeys()[0], before.GetPasskeys()[0]) {
				t.Error("the passkey changed")
			}
			extras := got.GetExtraFields()
			if len(extras) != 5 {
				t.Fatalf("the item has %d custom fields, want the 5 it had", len(extras))
			}
			wantKinds := []string{"hidden:password", "text:password", "text:note", "totp:totp", "text:"}
			for i, f := range extras {
				kind := fmt.Sprintf("%T", f.GetContent())
				if !proto.Equal(f, original.GetExtraFields()[i]) {
					t.Errorf("custom field %d (%s, %s) changed", i, wantKinds[i], kind)
				}
			}
			// the fields this program has no name for, one level at a time
			for level, pair := range map[string][2]proto.Message{
				"item": {got, original}, "metadata": {got.GetMetadata(), original.GetMetadata()},
				"content": {got.GetContent(), original.GetContent()}, "login": {login, before},
				"passkey": {login.GetPasskeys()[0], before.GetPasskeys()[0]}, "custom field": {extras[0], original.GetExtraFields()[0]},
			} {
				if len(unknownOf(pair[1])) == 0 {
					t.Fatalf("the fixture has no unknown field on the %s", level)
				}
				if !bytes.Equal(unknownOf(pair[0]), unknownOf(pair[1])) {
					t.Errorf("the unknown fields on the %s were not preserved", level)
				}
			}

			// the write is sealed, names the revision it was based on, and carries no plaintext of what was set
			for _, secret := range []string{tc.password, tc.note} {
				if len(secret) > 3 && bytes.Contains(sealed, []byte(secret)) {
					t.Error("the written content holds a value in the clear")
				}
			}
			if d.put["LastRevision"] != 3 || d.put["KeyRotation"] != 1 || d.put["ContentFormatVersion"] != contentFormatVersion {
				t.Errorf("the write says %v, %v, %v", d.put["LastRevision"], d.put["KeyRotation"], d.put["ContentFormatVersion"])
			}
			if len(d.calls) != 3 {
				t.Errorf("an edit made %d requests (%v), want read, key, write", len(d.calls), d.calls)
			}
		})
	}
}

// An empty value is how a patch says "leave it": it changes nothing and sends nothing, which is why the command line refuses
// a frame that would only be ignored.
func TestAnEmptyPatchSendsNothing(t *testing.T) {
	s, d, _ := editFixture(t, richLogin())
	if err := s.ItemEdit(context.Background(), editShare, editItem, Patch{}); err != nil {
		t.Fatalf("ItemEdit: %v", err)
	}
	if len(d.calls) != 0 || d.put != nil {
		t.Errorf("an empty patch made requests: %v", d.calls)
	}
}

// A note item keeps its text in the metadata like every other item; the content is an empty message and anything a newer client
// put in it stays.
func TestEditingTheNoteOfANoteItemKeepsItsContent(t *testing.T) {
	content := &pb.Content{Content: &pb.Content_Note{Note: &pb.ItemNote{}}}
	withUnknown(content.GetNote(), 6001, "unknown on the note content")
	original := &pb.Item{Metadata: &pb.Metadata{Name: "Synthetic note", Note: "old"}, Content: content}
	got, _, _, _ := edited(t, original, Patch{Scalars: Scalars{Note: "  new\n text\x00 "}})
	if got.GetMetadata().GetNote() != "  new\n text\x00 " || got.GetMetadata().GetName() != "Synthetic note" {
		t.Errorf("name %q, note %q", got.GetMetadata().GetName(), got.GetMetadata().GetNote())
	}
	if !bytes.Equal(unknownOf(got.GetContent().GetNote()), unknownOf(content.GetNote())) {
		t.Error("the content of the note item was not preserved")
	}
}

// A password patch on an item that has no password is refused after the one read and before anything is sealed or sent. It used to
// be written back unchanged and reported as a success. The refusal is atomic: a note sent beside the password is not applied
// either, and it names the kind of item, never a value.
func TestAPasswordPatchOnAnItemWithoutAPasswordIsRefusedBeforeAnyWrite(t *testing.T) {
	const secret, note = "SYNTHETIC-PASSWORD-NEVER-ECHOED-41d2", "SYNTHETIC-NOTE-NEVER-ECHOED-8b07"
	unknownKind := &pb.Content{}
	withUnknown(unknownKind, 7001, "a kind from a newer client")
	for name, content := range map[string]*pb.Content{
		"note":        {Content: &pb.Content_Note{Note: &pb.ItemNote{}}},
		"credit-card": {Content: &pb.Content_CreditCard{CreditCard: &pb.ItemCreditCard{Number: "0000"}}},
		"ssh-key":     {Content: &pb.Content_SshKey{SshKey: &pb.ItemSSHKey{PublicKey: "pub"}}},
		"identity":    {Content: &pb.Content_Identity{Identity: &pb.ItemIdentity{}}},
		"custom":      {Content: &pb.Content_Custom{Custom: &pb.ItemCustom{}}},
		"alias":       {Content: &pb.Content_Alias{Alias: &pb.ItemAlias{}}},
		"unknown":     unknownKind,
		"no content":  nil,
	} {
		for patchName, scalars := range map[string]Scalars{
			"password alone":    {Password: secret},
			"password and note": {Password: secret, Note: note},
		} {
			t.Run(name+"/"+patchName, func(t *testing.T) {
				original := &pb.Item{Metadata: &pb.Metadata{Name: "Synthetic item", Note: "kept"}, Content: content}
				s, d, _ := editFixture(t, original)
				err := s.ItemEdit(context.Background(), editShare, editItem, Patch{Scalars: scalars})
				if err == nil {
					t.Fatal("a password patch on an item with no password was accepted")
				}
				if d.put != nil {
					t.Error("a write was sent")
				}
				if len(d.calls) != 1 || d.calls[0] != "GET "+fmt.Sprintf("/pass/v1/share/%s/item/%s", editShare, editItem) {
					t.Errorf("the refusal came after more than the one read: %v", d.calls)
				}
				if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), note) {
					t.Errorf("the refusal repeats a value: %v", err)
				}
			})
		}
	}
}

// The kinds that do have a password keep editing it: a login (the rich fixture above) and a Wi-Fi network, whose other fields
// stay as they were.
func TestAPasswordPatchOnAWifiItemStillWorks(t *testing.T) {
	wifi := &pb.ItemWifi{Ssid: "synthetic-net", Password: "old", Security: pb.WifiSecurity_WPA2}
	withUnknown(wifi, 7002, "unknown on the wifi content")
	original := &pb.Item{Metadata: &pb.Metadata{Name: "Synthetic wifi", Note: "kept"}, Content: &pb.Content{Content: &pb.Content_Wifi{Wifi: wifi}}}
	got, _, _, _ := edited(t, original, Patch{Scalars: Scalars{Password: " new wifi password "}})
	want := proto.Clone(original).(*pb.Item)
	want.GetContent().GetWifi().Password = " new wifi password "
	if !proto.Equal(want, got) {
		t.Errorf("the item written is not the item read with only the password replaced: %v", got)
	}
}

// A note-only edit of an item whose kind this program does not know (its content is only unknown fields) still works, and the
// content is written back byte for byte.
func TestAnItemOfAnUnknownKindKeepsItsContentWhenOnlyTheNoteChanges(t *testing.T) {
	content := &pb.Content{}
	withUnknown(content, 7001, "a kind from a newer client")
	original := &pb.Item{Metadata: &pb.Metadata{Name: "Synthetic future item", Note: "old"}, Content: content}
	got, _, _, _ := edited(t, original, Patch{Scalars: Scalars{Note: "new"}})
	if got.GetMetadata().GetNote() != "new" {
		t.Errorf("note = %q", got.GetMetadata().GetNote())
	}
	if !bytes.Equal(unknownOf(got.GetContent()), unknownOf(content)) || got.GetContent().GetContent() != nil {
		t.Error("the unknown content was not preserved as it was")
	}
}

// CHARACTERIZATION of the custom-field route, which this spike does not use: a patch that names a custom field replaces the FIRST
// field of that name (and its kind) and leaves a second field with the same name alone. Setting the login's own password does
// not go through this route.
func TestACustomFieldPatchReplacesOnlyTheFirstFieldOfThatName(t *testing.T) {
	original := richLogin()
	got, _, _, _ := edited(t, original, Patch{ExtraFields: ExtraFields{Text: []string{"password=replaced"}}})
	extras := got.GetExtraFields()
	if len(extras) != 5 || extras[0].GetText().GetContent() != "replaced" || extras[1].GetText().GetContent() != "text two" {
		t.Errorf("custom fields: %v", extras)
	}
	if got.GetContent().GetLogin().GetPassword() != "old-password" {
		t.Error("a custom field called password changed the login's password")
	}
}

// A username or an email patch on an item that has none is refused after the one read, like a password: it used to be sealed,
// sent and reported as an edit that changed nothing. An identity keeps an email (through its own section), so only the
// username is refused there; a login takes both, byte for byte, and keeps everything else.
func TestAUsernameOrEmailPatchOnAnItemThatHasNoneIsRefusedBeforeAnyWrite(t *testing.T) {
	const secret = "SYNTHETIC-VALUE-NEVER-ECHOED-93a1"
	contents := map[string]*pb.Content{
		"note":        {Content: &pb.Content_Note{Note: &pb.ItemNote{}}},
		"credit-card": {Content: &pb.Content_CreditCard{CreditCard: &pb.ItemCreditCard{Number: "0000"}}},
		"wifi":        {Content: &pb.Content_Wifi{Wifi: &pb.ItemWifi{Ssid: "synthetic-net"}}},
		"identity":    {Content: &pb.Content_Identity{Identity: &pb.ItemIdentity{}}},
	}
	for name, content := range contents {
		for field, scalars := range map[string]Scalars{"username": {Username: secret}, "email": {Email: secret}} {
			if name == "identity" && field == "email" {
				continue
			}
			t.Run(name+"/"+field, func(t *testing.T) {
				s, d, _ := editFixture(t, &pb.Item{Metadata: &pb.Metadata{Name: "Synthetic item"}, Content: content})
				err := s.ItemEdit(context.Background(), editShare, editItem, Patch{Scalars: scalars})
				if err == nil {
					t.Fatal("the patch was accepted")
				}
				if d.put != nil {
					t.Error("a write was sent")
				}
				if strings.Contains(err.Error(), secret) {
					t.Errorf("the refusal repeats a value: %v", err)
				}
			})
		}
	}
}

func TestEditingTheUsernameAndEmailOfALoginLeavesEverythingElseAlone(t *testing.T) {
	original := richLogin()
	username, email := "  api\x00key \t\n", "é日@example.invalid \r\n"
	got, _, _, _ := edited(t, original, Patch{Scalars: Scalars{Username: username, Email: email}})
	want := proto.Clone(original).(*pb.Item)
	want.GetContent().GetLogin().ItemUsername = username
	want.GetContent().GetLogin().ItemEmail = email
	if !proto.Equal(want, got) {
		t.Errorf("the item written is not the item read with only the username and email replaced: %v", got)
	}
}

// An identity's email is edited through its own section, and everything else the identity holds is written back as it was read. The
// command sends the email both as a scalar and as the identity field, so the patch here does the same.
func TestEditingTheEmailOfAnIdentityLeavesEverythingElseAlone(t *testing.T) {
	idn := buildIdentity(map[string]string{"first-name": "Jane", "email": "old@example.invalid", "city": "Vienna"})
	withUnknown(idn, 7003, "unknown on the identity content")
	original := &pb.Item{Metadata: &pb.Metadata{Name: "Synthetic identity", Note: "kept"}, Content: &pb.Content{Content: &pb.Content_Identity{Identity: idn}}}
	email := "é日@example.invalid \r\n"
	got, _, _, _ := edited(t, original, Patch{Scalars: Scalars{Email: email}, Identity: map[string]string{"email": email}})
	want := proto.Clone(original).(*pb.Item)
	for _, f := range IdentityFields {
		if f.Flag == "email" {
			f.Set(want.GetContent().GetIdentity(), email)
		}
	}
	if !proto.Equal(want, got) {
		t.Errorf("the item written is not the item read with only the email replaced: %v", got)
	}
	if read := readIdentity(got.GetContent().GetIdentity()); read["email"] != email || read["first-name"] != "Jane" || read["city"] != "Vienna" {
		t.Errorf("identity fields after the edit: %v", read)
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
