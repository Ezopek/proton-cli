package offline

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --secret-frames carries the password and the note of a stored item on standard input, byte for byte. Everything about the
// frames that is judgeable from the command line and the stream alone is judged there, before the network and before anything
// is written; and nothing that arrives on the stream is ever repeated, in the answer or in the files a run leaves.

const (
	framePassword = "SYNTHETIC-PASSWORD-SENTINEL-3f9a"
	frameNote     = "SYNTHETIC-NOTE-SENTINEL-77d1"
)

var createWithFrames = []string{"pass", "items", "create", "--name", "Synthetic", "--secret-frames", "-"}

// framesInput writes the header and then NAME:LENGTH, the bytes and a newline for each pair.
func framesInput(pairs ...string) string {
	var b strings.Builder
	b.WriteString("secret-frames/1\n")
	for i := 0; i+1 < len(pairs); i += 2 {
		fmt.Fprintf(&b, "%s:%d\n%s\n", pairs[i], len(pairs[i+1]), pairs[i+1])
	}
	return b.String()
}

// refusesInput is refuses with something on standard input: the same exit code, the same promise that the answer needed no
// network, and the added promise that no byte of the input is in what was said.
func refusesInput(t *testing.T, stdin string, args []string, phrases ...string) {
	t.Helper()
	stdout, stderr, code := runWithStdin(t, stdin, args...)
	if code != 1 {
		t.Errorf("%v: exit %d, want 1\nstderr: %s", args, code, truncate(stderr))
	}
	if strings.Contains(stderr, "request failed") || strings.Contains(stderr, "connection refused") {
		t.Errorf("%v: reached the network, so this answer was not judged from the command line\nstderr: %s", args, truncate(stderr))
	}
	if stdout != "" {
		t.Errorf("%v: wrote to stdout while refusing: %q", args, truncate(stdout))
	}
	for _, secret := range []string{framePassword, frameNote} {
		if strings.Contains(stderr, secret) || strings.Contains(stdout, secret) {
			t.Errorf("%v: the answer repeats what arrived on stdin", args)
		}
	}
	for _, want := range phrases {
		if !strings.Contains(stderr, want) {
			t.Errorf("%v: stderr does not mention %q\nstderr: %s", args, want, truncate(stderr))
		}
	}
}

func TestMalformedFramesAreRefusedBeforeTheNetwork(t *testing.T) {
	pw := fmt.Sprint(len(framePassword))
	for name, tc := range map[string]struct {
		input string
		want  string
	}{
		"no header":          {"password:" + pw + "\n" + framePassword + "\n", "does not begin with"},
		"nothing at all":     {"", "does not begin with"},
		"only the header":    {"secret-frames/1\n", "no frame"},
		"truncated":          {"secret-frames/1\npassword:" + pw + "\n" + framePassword[:5], "ends before"},
		"no closing newline": {"secret-frames/1\npassword:" + pw + "\n" + framePassword, "ends before"},
		"too long a length":  {"secret-frames/1\npassword:99\n" + framePassword + "\n", "ends before"},
		"too short a length": {"secret-frames/1\npassword:5\n" + framePassword + "\n", "newline after its 5 bytes"},
		"another field":      {"secret-frames/1\ntotp-uri:" + pw + "\n" + framePassword + "\n", "email, note, password or username"},
		"the same one twice": {framesInput("password", framePassword, "password", framePassword), "given twice"},
		"an empty one":       {"secret-frames/1\nnote:0\n\n", "is empty"},
		"not UTF-8":          {framesInput("note", frameNote+"\xff"), "not valid UTF-8"},
		"text after":         {framesInput("note", frameNote) + "more", "no end"},
	} {
		t.Run(name, func(t *testing.T) {
			refusesInput(t, tc.input, createWithFrames, tc.want)
		})
	}
}

func TestFramesAreRefusedWhereAnotherSourceSetsTheSameField(t *testing.T) {
	both := framesInput("password", framePassword, "note", frameNote)
	refusesInput(t, both, append(append([]string{}, createWithFrames...), "--note", "from argv"), "--note and --secret-frames both set the note")
	refusesInput(t, both, append(append([]string{}, createWithFrames...), "--generate-password"), "both set the password")
	refusesInput(t, both, append(append([]string{}, createWithFrames...), "--secret-file", "password=/nonexistent/path"),
		"both set the password")
	// a secret file for a different field is not a conflict: this one is refused for what it is, not for the frames
	refusesInput(t, both, append(append([]string{}, createWithFrames...), "--secret-file", "token=/nonexistent/path"), "Could not read")
}

// The process-level form of the same promise: a note create with a password frame is refused before the network, and one without it is not.
func TestAPasswordFrameOnANoteCreateIsRefusedBeforeTheNetwork(t *testing.T) {
	note := []string{"pass", "items", "create", "--name", "Synthetic", "--type", "note", "--secret-frames", "-"}
	refusesInput(t, framesInput("password", framePassword), note, "has no password", "note")
	refusesInput(t, framesInput("note", frameNote, "password", framePassword), note, "has no password")
	stdout, stderr, code := runWithStdin(t, framesInput("note", frameNote), note...)
	if code == 0 || strings.Contains(stderr, "secret-frames") || strings.Contains(stdout+stderr, frameNote) {
		t.Errorf("a note frame for a note create was refused as frames, or repeated: exit %d: %s", code, truncate(stderr))
	}
}

// A username or an email can itself be a secret: it is a frame too, refused before the network where another route carries the same field
// or the kind has no such field, and never repeated by the answer.
func TestUsernameAndEmailFramesAreJudgedBeforeTheNetworkAndNeverRepeated(t *testing.T) {
	const frameUser = "SYNTHETIC-USERNAME-SENTINEL-2b6e"
	both := framesInput("username", frameUser, "email", frameUser)
	refusesInput(t, both, append(append([]string{}, createWithFrames...), "--username", "from argv"), "--username and --secret-frames both set the username")
	refusesInput(t, both, append(append([]string{}, createWithFrames...), "--email", "from@argv.invalid"), "--email and --secret-frames both set the email")
	note := []string{"pass", "items", "create", "--name", "Synthetic", "--type", "note", "--secret-frames", "-"}
	refusesInput(t, framesInput("username", frameUser), note, "has no username", "note")
	refusesInput(t, framesInput("email", frameUser), note, "has no email", "note")
	refusesInput(t, framesInput("username", frameUser), []string{"pass", "items", "create", "--name", "Synthetic", "--type", "identity",
		"--secret-frames", "-"}, "has no username", "identity")
	// a login accepts both: the run then stops where every offline run stops (nobody is signed in), not as frames, and repeats nothing
	args := append(append([]string{}, createWithFrames...), "--type", "login")
	stdout, stderr, code := runWithStdin(t, both, args...)
	if code == 0 || strings.Contains(stderr, "secret-frames") || strings.Contains(stdout+stderr, frameUser) {
		t.Errorf("a login username/email frame was refused as frames, or repeated: exit %d: %s", code, truncate(stderr))
	}
	for _, arg := range args {
		if strings.Contains(arg, frameUser) {
			t.Errorf("a secret is in argv: %q", arg)
		}
	}
}

func TestFramesAndAnotherReaderOfStdinAreRefused(t *testing.T) {
	refusesInput(t, framesInput("password", framePassword),
		append(append([]string{}, createWithFrames...), "--secret-file", "token=-"), "both read standard input")
}

func TestFramesReadStdinOnlyAndNeverAPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "frames")
	if err := os.WriteFile(file, []byte(framesInput("password", framePassword)), 0o600); err != nil {
		t.Fatal(err)
	}
	refusesInput(t, "", []string{"pass", "items", "create", "--name", "Synthetic", "--secret-frames", file}, "standard input only")
}

// A well-formed input gets past the frames, and the run then stops where every offline run stops: nobody is signed in. What
// matters is what is left behind - neither the answer nor any file the run wrote (its log is one) holds a byte of the input.
func TestWellFormedFramesAreNeverRepeatedByTheRunOrItsFiles(t *testing.T) {
	odd := " lead\x00and trail \n"
	for name, input := range map[string]string{
		"both": framesInput("password", framePassword+odd, "note", frameNote+odd),
		"note": framesInput("note", frameNote+odd),
	} {
		t.Run(name, func(t *testing.T) {
			args := append(append([]string{}, createWithFrames...), "--type", "login")
			stdout, stderr, code := runWithStdin(t, input, args...)
			if code == 0 {
				t.Fatalf("a create with nobody signed in succeeded\nstdout: %s", truncate(stdout))
			}
			if strings.Contains(stderr, "secret-frames") {
				t.Errorf("a well-formed input was refused as frames: %s", truncate(stderr))
			}
			for _, secret := range []string{framePassword, frameNote} {
				if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
					t.Errorf("the answer repeats what arrived on stdin")
				}
			}
			for _, arg := range args {
				if strings.Contains(arg, framePassword) || strings.Contains(arg, frameNote) {
					t.Errorf("a secret is in argv: %q", arg)
				}
			}
			err := filepath.WalkDir(configDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				for _, secret := range []string{framePassword, frameNote} {
					if bytes.Contains(data, []byte(secret)) {
						t.Errorf("%s holds what arrived on stdin", path)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("scan the files the run left: %v", err)
			}
		})
	}
}

// update takes the same frames and judges them the same way before the network. Its item is named by a full SHARE/ITEM, which
// needs no lookup; the kind of the item is not known until it is read, so only conflicts and the stream can be refused here.
func TestUpdateJudgesFramesBeforeTheNetworkToo(t *testing.T) {
	update := []string{"pass", "items", "update", "SYNTHETICSHARE/SYNTHETICITEM", "--secret-frames", "-"}
	both := framesInput("password", framePassword, "note", frameNote, "username", framePassword, "email", frameNote)
	refusesInput(t, both, append(append([]string{}, update...), "--note", "from argv"), "--note and --secret-frames both set the note")
	refusesInput(t, both, append(append([]string{}, update...), "--username", "from argv"), "--username and --secret-frames both set the username")
	refusesInput(t, both, append(append([]string{}, update...), "--email", "from@argv.invalid"), "--email and --secret-frames both set the email")
	refusesInput(t, both, append(append([]string{}, update...), "--generate-password"), "both set the password")
	refusesInput(t, both, append(append([]string{}, update...), "--secret-file", "password=/nonexistent/path"), "both set the password")
	refusesInput(t, both, append(append([]string{}, update...), "--secret-file", "token=-"), "both read standard input")
	refusesInput(t, "password:3\nabc\n", update, "does not begin with")
	refusesInput(t, framesInput("note", frameNote+"\xff"), update, "not valid UTF-8")
}
