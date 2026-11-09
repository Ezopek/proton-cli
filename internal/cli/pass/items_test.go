package pass

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func historyValue(it passsvc.Item, label string) string {
	for _, f := range historyFields(it) {
		if f.Label == label {
			return f.Value
		}
	}
	return ""
}

// Only what a Pass app fills in has a last use, so a note shows none rather than
// "never" - and a login nobody has used yet says so.
func TestLastUsedIsShownForWhatPassFillsIn(t *testing.T) {
	for _, tc := range []struct {
		kind string
		used int64
		want string
	}{
		{"login", 0, "never"},
		{"credit-card", 0, "never"},
		{"identity", 0, "never"},
		{"note", 0, ""},
		{"alias", 0, ""},
	} {
		it := passsvc.Item{Type: tc.kind, LastUseTime: tc.used}
		if got := historyValue(it, "Last Used"); got != tc.want {
			t.Errorf("%s used at %d shows %q, want %q", tc.kind, tc.used, got, tc.want)
		}
	}
	if got := historyValue(passsvc.Item{Type: "login", LastUseTime: 1790323380}, "Last Used"); got == "" || got == "never" {
		t.Errorf("a login used once shows %q", got)
	}
}

func TestAnExcludedItemSaysSo(t *testing.T) {
	if got := historyValue(passsvc.Item{Type: "login", Excluded: true}, "Monitor"); got != "excluded" {
		t.Errorf("an excluded login shows Monitor %q", got)
	}
	if got := historyValue(passsvc.Item{Type: "login"}, "Monitor"); got != "" {
		t.Errorf("a login Pass Monitor checks shows Monitor %q", got)
	}
}

// oneVaultDoer holds a single vault whose keys cannot be fetched.
type oneVaultDoer struct{}

func (oneVaultDoer) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (oneVaultDoer) Decode(_ context.Context, r proton.Request, out any) error {
	if r.Method == "GET" && r.Path == "/pass/v1/share" {
		return json.Unmarshal([]byte(`{"Shares":[{"ShareID":"vault1","VaultID":"v1","TargetType":1,"Owner":true,"ShareRoleID":"1"}]}`), out)
	}
	return fmt.Errorf("nothing to answer %s %s with", r.Method, r.Path)
}

// A listing over a vault that cannot be read is not the listing of an empty one:
// the response says something is missing.
func TestItemsListSaysWhenAVaultCouldNotBeRead(t *testing.T) {
	var out, errb bytes.Buffer
	a := &app.App{
		Pass: passsvc.New(oneVaultDoer{}, func(context.Context) (*keys.Unlocked, error) { return &keys.Unlocked{}, nil }),
		UI:   ui.New(ui.Options{Format: ui.FormatJSON, Out: &out, Err: &errb}),
	}
	cmd := itemsListCmd()
	cmd.SetContext(app.WithApp(context.Background(), a))
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("items list: %v", err)
	}
	var got struct {
		Skipped int `json:"skipped"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if got.Skipped != 1 {
		t.Errorf("skipped = %d, want 1:\n%s", got.Skipped, out.String())
	}
}

// framedInput is what a sender writes to --secret-frames -: header, then NAME:LENGTH, the bytes, a newline.
func framedInput(pairs ...string) string {
	var b strings.Builder
	b.WriteString(kit.FramesHeader)
	for i := 0; i+1 < len(pairs); i += 2 {
		b.WriteString(pairs[i] + ":" + itoa(len(pairs[i+1])) + "\n" + pairs[i+1] + "\n")
	}
	return b.String()
}

func itoa(n int) string {
	digits := ""
	for ; n > 0; n /= 10 {
		digits = string(rune('0'+n%10)) + digits
	}
	return digits
}

// frameFields parses a command line the way the real command would and runs the steps that precede the network:
// the secrets, then the frames.
func frameFields(t *testing.T, stdin string, args ...string) (*fields, error) {
	t.Helper()
	d := &fields{}
	cmd := &cobra.Command{Use: "test"}
	d.register(cmd, "Set")
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("flags %v: %v", args, err)
	}
	a := &app.App{UI: ui.New(ui.Options{Out: io.Discard, Err: io.Discard, In: strings.NewReader(stdin), NoInput: true})}
	inv := &kit.Invocation{Ctx: context.Background(), App: a, Cmd: cmd}
	if err := d.secrets.Supply(inv); err != nil {
		return d, err
	}
	return d, d.frames.Supply(inv)
}

// Frames reach the item's own password and note exactly, and a custom field that happens to be called the same
// is neither written nor replaced by them.
func TestFramesSetThePasswordAndTheNoteExactlyAndLeaveCustomFieldsAlone(t *testing.T) {
	password := "  sp\x00ace \n"
	note := "\nline one\r\n  line two é日\n\n"
	d, err := frameFields(t, framedInput("password", password, "note", note),
		"--secret-frames", "-", "--field", "note=custom text", "--field", "password=custom text two")
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if err := d.checkFields("login"); err != nil {
		t.Fatalf("checkFields: %v", err)
	}
	if err := d.readSecrets(nil); err != nil {
		t.Fatalf("readSecrets: %v", err)
	}
	if d.nc.Password != password || d.nc.Note != note {
		t.Errorf("password %q, note %q", d.nc.Password, d.nc.Note)
	}
	if got := strings.Join(d.nc.ExtraFields.Text, "|"); got != "note=custom text|password=custom text two" {
		t.Errorf("custom fields changed: %q", got)
	}
	if len(d.nc.ExtraFields.Hidden) != 0 || len(d.nc.ExtraFields.TOTP) != 0 {
		t.Errorf("a frame became a custom field: %+v", d.nc.ExtraFields)
	}
	// the judgement is repeatable: the handler checks again after the fold and must not see its own note as a conflict
	if err := d.checkFields("login"); err != nil {
		t.Errorf("the second check refused: %v", err)
	}
}

// A username or an email can itself be a secret, so both arrive as frames too. They reach the item's own username and email exactly (every
// byte, whitespace included), and a custom field called the same is neither written nor replaced.
func TestFramesSetTheUsernameAndTheEmailExactlyAndLeaveCustomFieldsAlone(t *testing.T) {
	username := "  api\x00key \t\n"
	email := "é日@example.invalid \n"
	d, err := frameFields(t, framedInput("username", username, "email", email, "password", "p"),
		"--secret-frames", "-", "--field", "username=custom one", "--field", "email=custom two")
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if err := d.checkFields("login"); err != nil {
		t.Fatalf("checkFields: %v", err)
	}
	if err := d.readSecrets(nil); err != nil {
		t.Fatalf("readSecrets: %v", err)
	}
	if d.nc.Username != username || d.nc.Email != email || d.nc.Password != "p" {
		t.Errorf("username %q, email %q, password %q", d.nc.Username, d.nc.Email, d.nc.Password)
	}
	if got := strings.Join(d.nc.ExtraFields.Text, "|"); got != "username=custom one|email=custom two" {
		t.Errorf("custom fields changed: %q", got)
	}
	if len(d.nc.ExtraFields.Hidden) != 0 || len(d.nc.ExtraFields.TOTP) != 0 {
		t.Errorf("a frame became a custom field: %+v", d.nc.ExtraFields)
	}
	if err := d.checkFields("login"); err != nil { // repeatable: the fold must not look like a collision with itself
		t.Errorf("the second check refused: %v", err)
	}
}

// Only the fields that arrived are set, and the legacy flags still work when no frame carries the same field.
func TestAUsernameOrEmailFrameLeavesTheOtherFieldToItsFlag(t *testing.T) {
	d, err := frameFields(t, framedInput("username", " frame user "), "--secret-frames", "-", "--email", "flag@example.invalid")
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if err := d.checkFields("login"); err != nil {
		t.Fatalf("a username frame beside --email is not a conflict: %v", err)
	}
	if err := d.readSecrets(nil); err != nil {
		t.Fatalf("readSecrets: %v", err)
	}
	if d.nc.Username != " frame user " || d.nc.Email != "flag@example.invalid" {
		t.Errorf("username %q, email %q", d.nc.Username, d.nc.Email)
	}
	// without any frame the legacy flags behave exactly as before
	d, err = frameFields(t, "not frames at all", "--username", "u", "--email", "e@example.invalid")
	if err != nil || d.checkFields("login") != nil || d.readSecrets(nil) != nil {
		t.Fatalf("legacy flags: %v", err)
	}
	if d.nc.Username != "u" || d.nc.Email != "e@example.invalid" {
		t.Errorf("legacy flags changed: username %q, email %q", d.nc.Username, d.nc.Email)
	}
}

// A username or an email that two routes both carry is refused before anything is read or sent, and the refusal repeats neither value.
func TestTwoRoutesToTheUsernameOrTheEmailAreRefusedWithoutEchoingEither(t *testing.T) {
	const s = "SYNTHETIC-NEVER-ECHOED-6c0d"
	for name, tc := range map[string]struct {
		input string
		args  []string
		want  string
	}{
		"username and --username": {framedInput("username", s), []string{"--secret-frames", "-", "--username", s}, "--username and --secret-frames"},
		"email and --email":       {framedInput("email", s), []string{"--secret-frames", "-", "--email", s}, "--email and --secret-frames"},
		"all four and the flags":  {framedInput("username", s, "email", s, "note", s, "password", s), []string{"--secret-frames", "-", "--email", s}, "--email and --secret-frames"},
	} {
		t.Run(name, func(t *testing.T) {
			d, err := frameFields(t, tc.input, tc.args...)
			if err != nil {
				t.Fatalf("steps: %v", err)
			}
			err = d.checkFields("login")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("a field set twice was not refused as such: %v", err)
			}
			if strings.Contains(err.Error(), s) {
				t.Errorf("the refusal repeats a value: %v", err)
			}
			if d.nc.Username != "" && d.nc.Username != s || d.nc.Password != "" {
				t.Errorf("the item was filled in before the refusal: username %q, password %q", d.nc.Username, d.nc.Password)
			}
		})
	}
}

// The username is a login's, the email a login's or an identity's; a frame for any other kind would be accepted and dropped by the
// create, so it is refused where the kind is known (before the network). An edit's kind is not known yet and is not refused here.
func TestAUsernameOrEmailFrameForAKindThatHasNoneIsRefusedBeforeTheNetwork(t *testing.T) {
	const s = "SYNTHETIC-NEVER-ECHOED-1f88"
	for _, kind := range []string{"note", "credit-card", "ssh-key", "alias", "custom", "wifi"} {
		for _, field := range []string{"username", "email"} {
			t.Run(kind+"/"+field, func(t *testing.T) {
				d, err := frameFields(t, framedInput(field, s), "--secret-frames", "-")
				if err != nil {
					t.Fatalf("steps: %v", err)
				}
				err = d.checkFields(kind)
				if err == nil || strings.Contains(err.Error(), s) || !strings.Contains(err.Error(), kind) {
					t.Errorf("not refused as it should be (and by kind, without the value): %v", err)
				}
			})
		}
	}
	d, err := frameFields(t, framedInput("username", s), "--secret-frames", "-")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.checkFields("identity"); err == nil {
		t.Error("an identity has no username, yet the frame was accepted")
	}
	for kind, field := range map[string]string{"login": "username", "login ": "email", "identity": "email", "": "username"} {
		d, err := frameFields(t, framedInput(field, s), "--secret-frames", "-")
		if err != nil {
			t.Fatal(err)
		}
		if err := d.checkFields(strings.TrimSpace(kind)); err != nil {
			t.Errorf("%s/%s refused: %v", kind, field, err)
		}
	}
}

// Only the field that arrived is set: a note frame alone leaves the password to its other source, and the reverse.
func TestAFrameSetsOnlyTheFieldItCarries(t *testing.T) {
	d, err := frameFields(t, framedInput("note", " just a note "), "--secret-frames", "-")
	if err != nil || d.readSecrets(nil) != nil {
		t.Fatalf("steps: %v", err)
	}
	if d.nc.Password != "" || d.nc.Note != " just a note " {
		t.Errorf("password %q, note %q", d.nc.Password, d.nc.Note)
	}
	d, err = frameFields(t, framedInput("password", " just a password "), "--secret-frames", "-", "--note", "from the flag")
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if err := d.checkFields("login"); err != nil {
		t.Fatalf("a password frame beside --note is not a conflict: %v", err)
	}
	if err := d.readSecrets(nil); err != nil {
		t.Fatalf("readSecrets: %v", err)
	}
	if d.nc.Password != " just a password " || d.nc.Note != "from the flag" {
		t.Errorf("password %q, note %q", d.nc.Password, d.nc.Note)
	}
}

// A password or a note that two routes both carry is refused before anything is read or sent, and the refusal
// repeats neither value.
func TestTwoRoutesToTheSameFieldAreRefusedWithoutEchoingEither(t *testing.T) {
	const s = "SYNTHETIC-NEVER-ECHOED-9a41"
	for name, tc := range map[string]struct {
		input string
		args  []string
	}{
		"note and --note":                {framedInput("note", s), []string{"--secret-frames", "-", "--note", s}},
		"password and --secret-file":     {framedInput("password", s), []string{"--secret-frames", "-", "--secret-file", "password=/nonexistent/" + s}},
		"password and --generate":        {framedInput("password", s), []string{"--secret-frames", "-", "--generate-password"}},
		"both and the other two sources": {framedInput("password", s, "note", s), []string{"--secret-frames", "-", "--note", s, "--generate-password"}},
	} {
		t.Run(name, func(t *testing.T) {
			d, err := frameFields(t, tc.input, tc.args...)
			if err != nil {
				t.Fatalf("steps: %v", err)
			}
			err = d.checkFields("login")
			if err == nil {
				t.Fatal("a field set twice was accepted")
			}
			if strings.Contains(err.Error(), s) {
				t.Errorf("the refusal repeats a value: %v", err)
			}
			if d.nc.Password != "" || (d.nc.Note != "" && d.nc.Note != s) {
				t.Errorf("the item was filled in before the refusal: password %q, note %q", d.nc.Password, d.nc.Note)
			}
		})
	}
}

// A password frame for a kind of item that has no password would be accepted and dropped by the create, which then reports success.
// It is refused where the kind is known - before the network - and the refusal names the kind, never the value. Update does not know
// the kind until the item is read, and the service refuses it there.
func TestAPasswordFrameForAKindWithoutAPasswordIsRefusedBeforeTheNetwork(t *testing.T) {
	const s = "SYNTHETIC-NEVER-ECHOED-52b7"
	for _, kind := range []string{"note", "credit-card", "ssh-key", "identity", "alias", "custom"} {
		for name, input := range map[string]string{"password alone": framedInput("password", s), "password and note": framedInput("note", "n", "password", s)} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				d, err := frameFields(t, input, "--secret-frames", "-")
				if err != nil {
					t.Fatalf("steps: %v", err)
				}
				err = d.checkFields(kind)
				if err == nil {
					t.Fatal("a password frame for an item with no password was accepted")
				}
				if strings.Contains(err.Error(), s) || !strings.Contains(err.Error(), kind) {
					t.Errorf("the refusal repeats a value or does not name the kind: %v", err)
				}
			})
		}
	}
}

// The kinds that have a password, a note frame for any kind, and an edit (whose kind is not known yet) are not refused.
func TestFramesThatFitTheKindAreNotRefused(t *testing.T) {
	for kind, input := range map[string]string{
		"login": framedInput("password", "p", "note", "n"), "wifi": framedInput("password", "p"),
		"note": framedInput("note", "n"), "credit-card": framedInput("note", "n"), "": framedInput("password", "p", "note", "n"),
	} {
		t.Run("kind "+kind, func(t *testing.T) {
			d, err := frameFields(t, input, "--secret-frames", "-")
			if err != nil {
				t.Fatalf("steps: %v", err)
			}
			if err := d.checkFields(kind); err != nil {
				t.Errorf("refused: %v", err)
			}
		})
	}
}

// Standard input has one owner: frames and a secret file that both want it are refused in terms of the flags.
func TestFramesAndASecretFileCannotBothReadStdin(t *testing.T) {
	_, err := frameFields(t, framedInput("password", "x"), "--secret-frames", "-", "--secret-file", "token=-")
	if err == nil || !strings.Contains(err.Error(), "both read standard input") {
		t.Errorf("err = %v", err)
	}
}

// Without the flag nothing about the item changes: the frames code is inert and stdin is untouched.
func TestWithoutTheFlagNoFrameIsRead(t *testing.T) {
	d, err := frameFields(t, "not frames at all", "--note", "plain")
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if err := d.checkFields("login"); err != nil {
		t.Fatalf("checkFields: %v", err)
	}
	if err := d.readSecrets(nil); err != nil {
		t.Fatalf("readSecrets: %v", err)
	}
	if d.nc.Password != "" || d.nc.Note != "plain" {
		t.Errorf("password %q, note %q", d.nc.Password, d.nc.Note)
	}
}
