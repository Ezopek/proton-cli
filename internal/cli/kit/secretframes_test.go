package kit

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/ui"
)

// secretShape is synthetic: nothing in these tests is, or looks like, a real secret.
const secretShape = "SYNTHETIC-NEVER-ECHOED-5c2e"

// framed writes a frames input from NAME, VALUE pairs, the way a sender does:
// the length is the byte length, whatever the bytes are.
func framed(pairs ...string) string {
	var b strings.Builder
	b.WriteString(FramesHeader)
	for i := 0; i+1 < len(pairs); i += 2 {
		fmt.Fprintf(&b, "%s:%d\n%s\n", pairs[i], len(pairs[i+1]), pairs[i+1])
	}
	return b.String()
}

// A value arrives as it was written: every byte that a trimming reader, a line
// reader or a JSON round trip would have changed is in here.
func TestFramesCarryEveryByteOfAValueUntouched(t *testing.T) {
	values := map[string]string{
		"one byte":                "x",
		"leading and trailing":    "  padded  ",
		"ends in a newline":       "ends in a newline\n",
		"starts with a newline":   "\nstarts with a newline",
		"line breaks of both":     "one\r\ntwo\nthree\r",
		"blank lines":             "\n\nfirst\n\n\nlast\n\n",
		"tabs":                    "\t\ttabbed\t",
		"only whitespace":         " \t\n ",
		"NUL bytes":               "before\x00after\x00",
		"multiline unicode":       "zażółć gęślą jaźń\n日本語 🔐\ne\u0301 \u202eRTL\u202c\n\u00a0nbsp\u2003em",
		"a frame header inside":   "note:5\nhello\n" + FramesHeader + "password:1\nx\n",
		"the header on its own":   strings.TrimSuffix(FramesHeader, "\n"),
		"digits and colons":       "note:0\n:\n::\n0",
		"a lone surrogate escape": `\ud800 is only text here`,
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			got, err := ReadFrames(strings.NewReader(framed("password", value, "note", value+"|note")))
			if err != nil {
				t.Fatalf("ReadFrames: %v", err)
			}
			if got["password"] != value || got["note"] != value+"|note" || len(got) != 2 {
				t.Errorf("a value changed on the way: password %q, note %q", got["password"], got["note"])
			}
		})
	}
}

// A username or an email can itself be a secret (an API key used as a username), so both travel as frames, byte for byte, like the password
// and the note: the same values that must survive a trimming reader survive here, and the four fields come back in any order.
func TestFramesCarryTheUsernameAndTheEmailUntouchedBesideThePasswordAndTheNote(t *testing.T) {
	values := []string{"  padded  ", "ends in a newline\n", "\nstarts with a newline", "one\r\ntwo\r", "tab\there\t", " \t\n ", "before\x00after",
		"zażółć 日本語 🔐 e\u0301 \u202eRTL\u202c", "note:5\nhello\n" + FramesHeader, "x"}
	orders := [][]string{{"username", "email", "password", "note"}, {"note", "password", "email", "username"}, {"email", "username"}}
	for _, value := range values {
		for _, order := range orders {
			pairs := make([]string, 0, 2*len(order))
			for _, name := range order {
				pairs = append(pairs, name, value+"|"+name)
			}
			got, err := ReadFrames(strings.NewReader(framed(pairs...)))
			if err != nil {
				t.Fatalf("%q in order %v: %v", value, order, err)
			}
			if len(got) != len(order) {
				t.Fatalf("read %d fields, want %d", len(got), len(order))
			}
			for _, name := range order {
				if got[name] != value+"|"+name {
					t.Errorf("%s changed on the way: %q", name, got[name])
				}
			}
		}
	}
}

func TestFramesStillRefuseEveryOtherNameAndTheNewNamesAreExact(t *testing.T) {
	s := secretShape
	n := strconv.Itoa(len(s))
	for name, field := range map[string]string{"upper case username": "Username", "upper case email": "EMAIL", "a prefix": "user", "a suffix": "usernames",
		"a padded name": " username", "totp": "totp-uri", "name": "name", "url": "url"} {
		t.Run(name, func(t *testing.T) {
			got, err := ReadFrames(strings.NewReader(FramesHeader + field + ":" + n + "\n" + s + "\n"))
			if err == nil || got != nil {
				t.Fatalf("accepted %q: %v", field, got)
			}
			if strings.Contains(err.Error(), s) || strings.Contains(err.Error(), field+":") {
				t.Errorf("the refusal repeats the input: %v", err)
			}
		})
	}
	_, err := ReadFrames(strings.NewReader(FramesHeader + "totp-uri:1\nx\n"))
	if err == nil || !strings.Contains(err.Error(), "email, note, password or username") {
		t.Errorf("the refusal does not name the four frames: %v", err)
	}
}

func TestFramesMayCarryOneFieldOrBothInEitherOrder(t *testing.T) {
	cases := map[string]struct {
		input string
		want  map[string]string
	}{
		"password only":  {framed("password", "p"), map[string]string{"password": "p"}},
		"note only":      {framed("note", "n"), map[string]string{"note": "n"}},
		"note first":     {framed("note", "n", "password", "p"), map[string]string{"note": "n", "password": "p"}},
		"password first": {framed("password", "p", "note", "n"), map[string]string{"note": "n", "password": "p"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ReadFrames(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("ReadFrames: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("read %d fields, want %d", len(got), len(tc.want))
			}
			for field, want := range tc.want {
				if got[field] != want {
					t.Errorf("%s = %q, want %q", field, got[field], want)
				}
			}
		})
	}
}

// The bound is on everything read, and exactly at it is still accepted.
func TestFramesAreBoundedAndTheBoundIsExact(t *testing.T) {
	size := MaxFramesBytes - len(FramesHeader) - len("note:") - len(strconv.Itoa(MaxFramesBytes-100)) - len("\n") - len("\n")
	atBound := framed("note", strings.Repeat("a", size))
	if len(atBound) != MaxFramesBytes {
		t.Fatalf("the test input is %d bytes, want exactly %d", len(atBound), MaxFramesBytes)
	}
	got, err := ReadFrames(strings.NewReader(atBound))
	if err != nil || len(got["note"]) != size {
		t.Fatalf("an input of exactly the bound was refused: %v", err)
	}
	if _, err := ReadFrames(strings.NewReader(framed("note", strings.Repeat("a", size+1)))); err == nil {
		t.Error("an input one byte past the bound was accepted")
	}
}

type endless struct{ read int }

func (e *endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	e.read += len(p)
	return len(p), nil
}

// A stream that never ends is not read to its end.
func TestFramesNeverReadMoreThanTheBoundPlusOne(t *testing.T) {
	src := &endless{}
	if _, err := ReadFrames(src); err == nil {
		t.Fatal("an endless stream was accepted")
	}
	if src.read > MaxFramesBytes+1+32*1024 {
		t.Errorf("read %d bytes of an endless stream", src.read)
	}
}

// Every malformed input is refused whole - nothing partial comes back, so no
// edit can be applied from half an input - and no refusal repeats a byte of what
// it was given.
func TestMalformedFramesAreRefusedWholeAndNeverEchoed(t *testing.T) {
	s := secretShape
	n := strconv.Itoa(len(s))
	bad := map[string]string{
		"empty input":                 "",
		"no header":                   "password:" + n + "\n" + s + "\n",
		"another version":             "secret-frames/2\npassword:" + n + "\n" + s + "\n",
		"header without its newline":  strings.TrimSuffix(FramesHeader, "\n"),
		"header only":                 FramesHeader,
		"a header with no end":        FramesHeader + "password:" + n,
		"payload shorter than said":   FramesHeader + "password:" + strconv.Itoa(len(s)+50) + "\n" + s + "\n",
		"payload longer than said":    FramesHeader + "password:" + strconv.Itoa(len(s)-3) + "\n" + s + "\n",
		"no closing newline":          FramesHeader + "password:" + n + "\n" + s,
		"closing byte is not a LF":    FramesHeader + "password:" + n + "\n" + s + "x",
		"closing CR LF":               FramesHeader + "password:" + n + "\n" + s + "\r\n",
		"text after the last frame":   framed("password", s) + "trailing",
		"blank line after last frame": framed("password", s) + "\n",
		"the same field twice":        framed("password", s, "password", s),
		"the note twice":              framed("note", s, "note", s),
		"the username twice":          framed("username", s, "username", s),
		"the email twice":             framed("email", s, "email", s),
		"an empty username":           FramesHeader + "username:0\n\n",
		"an empty email":              FramesHeader + "email:0\n\n",
		"a username not UTF-8":        framed("username", "\xff"+s),
		"an email not UTF-8":          framed("email", s+"\x80"),
		"another field":               FramesHeader + "totp-uri:" + n + "\n" + s + "\n",
		"a custom field name":         FramesHeader + "token:" + n + "\n" + s + "\n",
		"upper case name":             FramesHeader + "Password:" + n + "\n" + s + "\n",
		"empty name":                  FramesHeader + ":" + n + "\n" + s + "\n",
		"no length":                   FramesHeader + "password\n" + s + "\n",
		"empty length":                FramesHeader + "password:\n" + s + "\n",
		"zero length":                 FramesHeader + "note:0\n\n",
		"leading zero":                FramesHeader + "note:0" + n + "\n" + s + "\n",
		"plus sign":                   FramesHeader + "note:+" + n + "\n" + s + "\n",
		"minus sign":                  FramesHeader + "note:-" + n + "\n" + s + "\n",
		"hex":                         FramesHeader + "note:0x" + n + "\n" + s + "\n",
		"a space":                     FramesHeader + "note: " + n + "\n" + s + "\n",
		"a CR LF header":              FramesHeader + "note:" + n + "\r\n" + s + "\n",
		"too many digits":             FramesHeader + "note:12345678\n" + s + "\n",
		"a length past any int":       FramesHeader + "note:99999999999999999999999\n" + s + "\n",
		"not UTF-8":                   framed("note", "\xff"+s),
		"a lone continuation byte":    framed("password", s+"\x80"),
		"an overlong encoding":        framed("password", s+"\xc0\xaf"),
		"a surrogate in UTF-8":        framed("password", s+"\xed\xa0\x80"),
		"a good field then a bad one": framed("note", "fine") + "password:" + n + "\n" + s,
	}
	for name, input := range bad {
		t.Run(name, func(t *testing.T) {
			got, err := ReadFrames(strings.NewReader(input))
			if err == nil {
				t.Fatalf("accepted: %q", got)
			}
			if got != nil {
				t.Errorf("returned %d fields beside the refusal", len(got))
			}
			if strings.Contains(err.Error(), s) || strings.Contains(err.Error(), "fine") {
				t.Errorf("the refusal repeats the input: %v", err)
			}
		})
	}
}

func TestAStdinFailureIsRefusedAndNamesNoContent(t *testing.T) {
	// The reader's own error text is arbitrary, so it is not part of what the person is told either.
	_, err := ReadFrames(io.MultiReader(strings.NewReader(framed("password", secretShape)[:20]), iotest.ErrReader(errors.New("pipe broke "+secretShape))))
	if err == nil {
		t.Fatal("a read error was accepted")
	}
	if strings.Contains(err.Error(), secretShape) || strings.Contains(err.Error(), "pipe broke") {
		t.Errorf("the refusal repeats the input or the reader's error: %v", err)
	}
}

func suppliedBy(input string, flag string) (*Frames, *app.App, error) {
	a := &app.App{UI: ui.New(ui.Options{Out: io.Discard, Err: io.Discard, In: strings.NewReader(input), NoInput: true})}
	f := &Frames{path: flag}
	return f, a, f.Supply(&Invocation{Ctx: context.Background(), App: a})
}

// Supply claims standard input and reads it at once, so a malformed input is a
// refusal before the network and before anything is written.
func TestSupplyReadsStdinAtOnceAndOnlyWhenAsked(t *testing.T) {
	f, _, err := suppliedBy(framed("password", " pw ", "note", "n\n"), "-")
	if err != nil {
		t.Fatalf("Supply: %v", err)
	}
	if v, ok := f.Value("password"); !ok || v != " pw " {
		t.Errorf("password = %q, %v", v, ok)
	}
	if v, ok := f.Value("note"); !ok || v != "n\n" {
		t.Errorf("note = %q, %v", v, ok)
	}
	if _, ok := f.Value("totp-uri"); ok {
		t.Error("a field nobody sent has a value")
	}
	// not asked for: stdin is left alone for whoever wants it
	f, a, err := suppliedBy("not frames", "")
	if err != nil {
		t.Fatalf("Supply without the flag: %v", err)
	}
	if _, ok := f.Value("note"); ok {
		t.Error("a value appeared without the flag")
	}
	if _, err := a.Stdin("another reader -"); err != nil {
		t.Errorf("stdin was claimed although the flag was not given: %v", err)
	}
}

func TestSupplyRefusesAMalformedInputAndAPathThatIsNotStdin(t *testing.T) {
	if _, _, err := suppliedBy("password:3\nabc\n", "-"); err == nil {
		t.Error("an input without the header was accepted")
	}
	if _, _, err := suppliedBy(framed("password", "abc"), "/tmp/frames"); err == nil {
		t.Error("a path was accepted; only - is")
	}
}

// The vectors are byte-exact inputs with the verdict the format gives, so a writer of the format can be held to the same file as
// this reader.
func TestTheSharedVectorsAreReadAsTheFormatSays(t *testing.T) {
	raw, err := os.ReadFile("testdata/secret-frames-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name      string            `json:"name"`
		OK        bool              `json:"ok"`
		InputHex  string            `json:"input_hex"`
		FieldsHex map[string]string `json:"fields_hex"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 30 {
		t.Fatalf("only %d vectors", len(vectors))
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			input, err := hex.DecodeString(v.InputHex)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadFrames(strings.NewReader(string(input)))
			if !v.OK {
				if err == nil {
					t.Fatalf("accepted %d fields", len(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if len(got) != len(v.FieldsHex) {
				t.Fatalf("read %d fields, want %d", len(got), len(v.FieldsHex))
			}
			for name, want := range v.FieldsHex {
				if hex.EncodeToString([]byte(got[name])) != want {
					t.Errorf("field %s differs", name)
				}
			}
		})
	}
}

func TestSupplyRefusesASecondReaderOfStdin(t *testing.T) {
	_, a, err := suppliedBy(framed("password", "abc"), "-")
	if err != nil {
		t.Fatalf("Supply: %v", err)
	}
	_, err = a.Stdin("--secret-file password=-")
	if err == nil || !strings.Contains(err.Error(), "both read standard input") {
		t.Errorf("a second reader of stdin was not refused: %v", err)
	}
}
