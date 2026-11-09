package kit

import (
	"bytes"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/spf13/cobra"
)

// Frames declares the other way a stored item's secret parts may arrive: as
// length-prefixed frames on standard input. The parts are the password and the
// note and, for a login, its username and email: either of those two can itself
// be a secret (an API key used as a username, a mailbox address that identifies
// a person), and a flag value is visible in the process list.
//
// --secret-file reads one value per stream and trims it, which is right for a
// password and wrong for a note: a note may begin or end in a space or a line
// break that is part of it, and only one field can own standard input. A frame
// states how many bytes it holds, so nothing in it is looked at, trimmed or
// mistaken for the next frame - a note that contains a frame header is a note -
// and one stream carries every field an edit sets.
//
// The input is the header line "secret-frames/1", then for each field
//
//	NAME:LENGTH LF, LENGTH bytes, LF
//
// and nothing after the last. NAME is email, note, password or username, so a
// frame can only ever reach the item's own email, note, password and username,
// never a custom field that happens to be called by the same word. LENGTH is a
// plain decimal of at least 1: an
// empty value means "leave as it is" to an edit, and a frame that would do
// nothing is refused rather than ignored. The bytes are a UTF-8 string, NUL
// included, because that is what an item field holds.
type Frames struct {
	path   string
	values map[string]string
}

// FramesUsage is the one thing the flag says, wherever it appears.
const FramesUsage = "Read the password, the note, the username and the email from standard input as length-prefixed frames, byte for byte; - is stdin"

// FramesHeader is the first line of a frames input.
const FramesHeader = "secret-frames/1\n"

// MaxFramesBytes bounds everything read from standard input.
const MaxFramesBytes = 1 << 20

// maxFrameDigits is as many digits as a length may have; more than the bound above can never fit.
const maxFrameDigits = 7

// frameFields are the only names a frame may carry.
var frameFields = []string{"email", "note", "password", "username"}

// Declare adds the flag to a command. Call Supply from its steps.
func (f *Frames) Declare(c *cobra.Command) {
	c.Flags().StringVar(&f.path, "secret-frames", "", FramesUsage)
}

// Supply claims standard input if the flag asks for it and reads every frame at
// once, so a malformed input is refused before the network and before anything
// is written. Standard input has one owner: another reader of it is refused by
// App.Stdin in terms of the two flags that collided.
func (f *Frames) Supply(c *Invocation) error {
	if f.path == "" {
		return nil
	}
	if f.path != app.Stdin {
		return errs.Problemf("--secret-frames reads standard input only, and %q is not -.", f.path).
			Hint("--secret-frames -")
	}
	r, err := c.App.Stdin("--secret-frames " + app.Stdin)
	if err != nil {
		return err
	}
	values, err := ReadFrames(r)
	if err != nil {
		return err
	}
	f.values = values
	return nil
}

// Value is the field a frame carried, and whether one did.
func (f *Frames) Value(name string) (string, bool) {
	v, ok := f.values[name]
	return v, ok
}

// ReadFrames reads and checks a whole frames input. No error carries a byte of
// a frame's content: a refusal names the field and the lengths, never the data.
func ReadFrames(r io.Reader) (map[string]string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFramesBytes+1))
	if err != nil {
		// The reader's error is whatever the reader says, so the person is told a fixed sentence; the log keeps the cause.
		slog.Debug("reading the frames from stdin failed", "error", err.Error())
		return nil, errs.Problemf("Could not read the frames from stdin.")
	}
	if len(data) > MaxFramesBytes {
		return nil, errs.Problemf("The frames on stdin are longer than %d bytes.", MaxFramesBytes)
	}
	rest, ok := bytes.CutPrefix(data, []byte(FramesHeader))
	if !ok {
		return nil, errs.Problemf("Standard input does not begin with the %q line --secret-frames reads.",
			strings.TrimSuffix(FramesHeader, "\n"))
	}
	out := make(map[string]string, len(frameFields))
	for len(rest) > 0 {
		end := bytes.IndexByte(rest, '\n')
		if end < 0 {
			return nil, errs.Problemf("A frame header on stdin has no end.")
		}
		name, size, err := frameHeader(string(rest[:end]))
		if err != nil {
			return nil, err
		}
		if _, dup := out[name]; dup {
			return nil, errs.Problemf("Frame %s is given twice.", name)
		}
		rest = rest[end+1:]
		if size > len(rest)-1 {
			return nil, errs.Problemf("Frame %s declares %d bytes and the input ends before them.", name, size)
		}
		if rest[size] != '\n' {
			return nil, errs.Problemf("Frame %s is not followed by a newline after its %d bytes.", name, size)
		}
		payload := rest[:size]
		if !utf8.Valid(payload) {
			return nil, errs.Problemf("Frame %s is not valid UTF-8, which an item field has to be.", name)
		}
		out[name] = string(payload)
		rest = rest[size+1:]
	}
	if len(out) == 0 {
		return nil, errs.Problemf("--secret-frames was given no frame.")
	}
	return out, nil
}

// frameHeader reads one NAME:LENGTH line. A refusal never repeats what it was
// given: a header is untrusted bytes, and the accepted names are the whole answer.
func frameHeader(line string) (name string, size int, err error) {
	name, digits, ok := strings.Cut(line, ":")
	known := false
	for _, f := range frameFields {
		known = known || f == name
	}
	if !ok || !known {
		return "", 0, errs.Problemf("A frame is named %s or %s.", strings.Join(frameFields[:len(frameFields)-1], ", "),
			frameFields[len(frameFields)-1])
	}
	if digits == "0" {
		return "", 0, errs.Problemf("Frame %s is empty, and an empty value means leave it as it is, so it is refused.", name)
	}
	plain := digits != "" && len(digits) <= maxFrameDigits && digits[0] != '0'
	for i := 0; plain && i < len(digits); i++ {
		plain = digits[i] >= '0' && digits[i] <= '9'
	}
	if !plain {
		return "", 0, errs.Problemf("The length of frame %s is not a plain decimal number.", name)
	}
	size, err = strconv.Atoi(digits)
	if err != nil {
		return "", 0, errs.Problemf("The length of frame %s is not a plain decimal number.", name)
	}
	return name, size, nil
}
