package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The record lookup answers one question: what does the live tip of ezop hold
// at fork/verification/<TAG>.md? It reads the tip through the GitHub API and
// walks the commit, its tree, the trees of fork and verification and, for a
// record that exists, its blob, each with a documented non-recursive endpoint.
// The contents endpoint is not used. It decides nothing about a release: the
// caller turns the state into a refusal or a status.

// The repository the lookup reads. They are constants on purpose: neither the
// environment nor an argument can point the lookup anywhere else.
const (
	ghRepo       = "Ezopek/proton-cli"
	ghAPIVersion = "2026-03-10"
	ezopBranch   = "ezop"
)

// recordState is the outcome of the lookup. The zero value is Unknown, so a
// result nobody filled in is the cautious one.
type recordState int

const (
	// recordUnknown is every doubt: a failed or malformed answer, a truncated
	// tree, an id that does not match, a missing directory.
	recordUnknown recordState = iota
	// recordPresent is a regular file whose blob was read and hashes to the
	// id the tree gave it.
	recordPresent
	// recordAbsent is no entry of that name, with README.md a regular file in
	// the same directory of the same commit.
	recordAbsent
	// recordNotFile is an entry of that name that is a directory, a symlink or
	// a submodule.
	recordNotFile
)

func (s recordState) String() string {
	switch s {
	case recordPresent:
		return "Present"
	case recordAbsent:
		return "Absent"
	case recordNotFile:
		return "NotFile"
	}
	return "Unknown"
}

// recordLookup is the answer: the state, the commit it was read at (empty when
// the tip was not resolved), the record bytes for Present, and for Unknown a
// reason built from a closed set of words. The reason never holds a response, a
// URL or a name from the answer.
type recordLookup struct {
	State  recordState
	Commit string
	Data   []byte
	Reason string
}

// ghGetter is the one seam to the GitHub API: a GET of an endpoint under the
// repository, answering the body. Tests replace it with a fake.
type ghGetter interface {
	Get(endpoint string) ([]byte, error)
}

// endpointPattern is every path the lookups may ask for: the tip of ezop, a
// commit, a tree or a blob by a full lowercase SHA, a page 1 to 50 of the
// releases list at 100 a page, and the four run reads (the workflow of
// fork-release.yml by file name, the runs of that workflow for one commit and
// the push event at 100 a page and a page 1 to 10, a run by id, and a run
// attempt by id and number), each in that one spelling. Digits are bounded: an
// id has at most 16, an attempt number at most 6, neither has a sign or a
// leading zero. Any other query string, a dot segment or any other name never
// matches.
var endpointPattern = regexp.MustCompile(`^(git/ref/heads/` + ezopBranch +
	`|git/(commits|trees|blobs)/[0-9a-f]{40}` +
	`|releases\?per_page=100&page=([1-9]|[1-4][0-9]|50)` +
	`|actions/workflows/` + regexp.QuoteMeta(forkRelease) +
	`|actions/workflows/` + regexp.QuoteMeta(forkRelease) + `/runs\?event=push&head_sha=[0-9a-f]{40}&per_page=100&page=([1-9]|10)` +
	`|actions/runs/[1-9][0-9]{0,15}` +
	`|actions/runs/[1-9][0-9]{0,15}/attempts/[1-9][0-9]{0,5})$`)

// ghArgs is the exact argument list of a gh call. A path that is not one of the
// validated shapes is refused here, before gh is run.
func ghArgs(endpoint string) ([]string, error) {
	if !endpointPattern.MatchString(endpoint) {
		return nil, errors.New("the API path is not one the lookup may read")
	}
	return []string{"api", "--method", "GET", "-H", "X-GitHub-Api-Version: " + ghAPIVersion, "repos/" + ghRepo + "/" + endpoint}, nil
}

// maxGHOutput bounds an answer; a tree or a record is far smaller.
const maxGHOutput = 8 << 20

// maxGHDownload bounds a downloaded release asset. A fork binary is tens of
// megabytes, so this is larger than the JSON limit, with room to grow, and still
// bounded.
const maxGHDownload = 256 << 20

// assetPattern is the one path a download may ask for: an asset of the release
// repository by its decimal id, without a sign or leading zero, in at most 16
// digits.
var assetPattern = regexp.MustCompile(`^releases/assets/[1-9][0-9]{0,15}$`)

// ghDownloader is the seam to the documented "get a release asset" endpoint:
// the content of one asset by its path. It is a separate interface so that a
// reader of JSON does not have to implement it.
type ghDownloader interface {
	Download(endpoint string) ([]byte, error)
}

// ghDownloadArgs is the exact argument list of a gh download. With the
// octet-stream Accept header the API answers the content (or redirects to it);
// without it the answer would be the asset's JSON description.
func ghDownloadArgs(endpoint string) ([]string, error) {
	if !assetPattern.MatchString(endpoint) {
		return nil, errors.New("the API path is not one the download may read")
	}
	return []string{"api", "--method", "GET", "-H", "X-GitHub-Api-Version: " + ghAPIVersion,
		"-H", "Accept: application/octet-stream", "repos/" + ghRepo + "/" + endpoint}, nil
}

const defaultGHTimeout = 60 * time.Second

// execGH is the adapter over the gh program. It builds the argument list, runs
// gh without a shell and returns the standard output. No error it returns
// carries gh's output, which can hold a URL or a token hint.
type execGH struct {
	// program is the gh program; empty means gh.
	program string
	// env is the environment of the call; nil means the process's own.
	env []string
	// timeout bounds the call; zero means 60 seconds.
	timeout time.Duration
	// downloadLimit bounds a downloaded asset; zero means maxGHDownload.
	downloadLimit int
}

// ghEnv drops the variables that could aim gh at another repository or host.
func ghEnv(base []string) []string {
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "GH_REPO=") || strings.HasPrefix(kv, "GH_HOST=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GH_PROMPT_DISABLED=1")
}

// limitWriter fails once more than max bytes were written.
type limitWriter struct {
	buf bytes.Buffer
	max int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.max {
		return 0, errors.New("the answer is too large")
	}
	return w.buf.Write(p)
}

func (g execGH) Get(endpoint string) ([]byte, error) {
	args, err := ghArgs(endpoint)
	if err != nil {
		return nil, err
	}
	return g.run(args, maxGHOutput)
}

// Download reads one release asset, up to downloadLimit bytes (maxGHDownload
// when it is zero).
func (g execGH) Download(endpoint string) ([]byte, error) {
	args, err := ghDownloadArgs(endpoint)
	if err != nil {
		return nil, err
	}
	limit := g.downloadLimit
	if limit == 0 {
		limit = maxGHDownload
	}
	return g.run(args, limit)
}

// run executes gh with args and returns at most limit bytes of its standard
// output.
func (g execGH) run(args []string, limit int) ([]byte, error) {
	program, timeout, env := g.program, g.timeout, g.env
	if program == "" {
		program = "gh"
	}
	if timeout == 0 {
		timeout = defaultGHTimeout
	}
	if env == nil {
		env = os.Environ()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = ghEnv(env)
	cmd.WaitDelay = time.Second
	out := &limitWriter{max: limit}
	cmd.Stdout = out
	// Standard error and input are dropped: gh's diagnostics can name a URL.
	if err := cmd.Run(); err != nil {
		return nil, errors.New("gh could not read the API path")
	}
	return out.buf.Bytes(), nil
}

// Closed vocabulary of the reasons an Unknown carries.
const (
	causeCall      = "call failed"
	causeMalformed = "malformed response"
	causeMismatch  = "id mismatch"
	causeNotCommit = "not a commit"
	causeTruncated = "truncated"
	causeRepeated  = "repeated entry"
	causeKind      = "unrecognized entry kind"
	causeEncoding  = "bad encoding"
	causeSize      = "size mismatch"
	causeHash      = "hash mismatch"
)

// getJSON reads one endpoint and decodes exactly one JSON value into v. Unknown
// fields are ignored; trailing data is not. The result is a cause, empty when
// the read and the decoding worked.
func getJSON(g ghGetter, endpoint string, v any) string {
	body, err := g.Get(endpoint)
	if err != nil {
		return causeCall
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		return causeMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return causeMalformed
	}
	return ""
}

type refAnswer struct {
	Ref    string `json:"ref"`
	Object struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"object"`
}

type commitAnswer struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

type treeItem struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type treeAnswer struct {
	SHA       string     `json:"sha"`
	Tree      []treeItem `json:"tree"`
	Truncated bool       `json:"truncated"`
}

type blobAnswer struct {
	SHA      string `json:"sha"`
	Size     *int   `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

// entryKind classifies a tree entry by its type and mode.
type entryKind int

const (
	kindOther entryKind = iota
	kindFile
	kindDir
	kindSymlink
	kindSubmodule
)

func classify(e treeItem) entryKind {
	switch {
	case e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755"):
		return kindFile
	case e.Type == "tree" && e.Mode == "040000":
		return kindDir
	case e.Type == "blob" && e.Mode == "120000":
		return kindSymlink
	case e.Type == "commit" && e.Mode == "160000":
		return kindSubmodule
	}
	return kindOther
}

// readTree reads one tree, non-recursively, and returns its entries by name.
// step names the level in a reason.
func readTree(g ghGetter, step, sha string) (map[string]treeItem, string) {
	var ans treeAnswer
	if cause := getJSON(g, "git/trees/"+sha, &ans); cause != "" {
		return nil, step + ": " + cause
	}
	if !fullSHA.MatchString(ans.SHA) {
		return nil, step + ": " + causeMalformed
	}
	entries := make(map[string]treeItem, len(ans.Tree))
	for _, e := range ans.Tree {
		if e.Path == "" || strings.Contains(e.Path, "/") || !fullSHA.MatchString(e.SHA) {
			return nil, step + ": " + causeMalformed
		}
	}
	if ans.SHA != sha {
		return nil, step + ": " + causeMismatch
	}
	if ans.Truncated {
		return nil, step + ": " + causeTruncated
	}
	for _, e := range ans.Tree {
		if _, dup := entries[e.Path]; dup {
			return nil, step + ": " + causeRepeated
		}
		entries[e.Path] = e
	}
	return entries, ""
}

// subTree finds the directory entry name in a tree.
func subTree(entries map[string]treeItem, step, name string) (string, string) {
	e, ok := entries[name]
	switch {
	case !ok:
		return "", step + ": " + name + " missing"
	case classify(e) != kindDir:
		return "", step + ": " + name + " is not a directory"
	}
	return e.SHA, ""
}

// gitBlobSHA is the git object id of a blob: SHA-1 of "blob <size>\0<data>".
func gitBlobSHA(data []byte) string {
	h := sha1.New()
	h.Write([]byte("blob " + strconv.Itoa(len(data)) + "\x00"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// lookupRecord reads the live tip of ezop and reports whether
// fork/verification/<tag>.md is a regular file there. It makes no refusal or
// status decision. Everything that is not one of Present, Absent and NotFile
// is Unknown.
func lookupRecord(g ghGetter, tag string) recordLookup {
	unknown := func(commit, reason string) recordLookup { return recordLookup{Commit: commit, Reason: reason} }
	if checkTagPattern(tag) != nil {
		return unknown("", "tag: not a Release Tag")
	}

	var ref refAnswer
	if cause := getJSON(g, "git/ref/heads/"+ezopBranch, &ref); cause != "" {
		return unknown("", "tip: "+cause)
	}
	if !fullSHA.MatchString(ref.Object.SHA) {
		return unknown("", "tip: "+causeMalformed)
	}
	if ref.Ref != "" && ref.Ref != "refs/heads/"+ezopBranch {
		return unknown("", "tip: "+causeMismatch)
	}
	if ref.Object.Type != "commit" {
		return unknown("", "tip: "+causeNotCommit)
	}
	commit := ref.Object.SHA

	var c commitAnswer
	if cause := getJSON(g, "git/commits/"+commit, &c); cause != "" {
		return unknown(commit, "commit: "+cause)
	}
	if !fullSHA.MatchString(c.SHA) || !fullSHA.MatchString(c.Tree.SHA) {
		return unknown(commit, "commit: "+causeMalformed)
	}
	if c.SHA != commit {
		return unknown(commit, "commit: "+causeMismatch)
	}

	root, reason := readTree(g, "root tree", c.Tree.SHA)
	if reason != "" {
		return unknown(commit, reason)
	}
	forkSHA, reason := subTree(root, "root tree", "fork")
	if reason != "" {
		return unknown(commit, reason)
	}
	fork, reason := readTree(g, "fork tree", forkSHA)
	if reason != "" {
		return unknown(commit, reason)
	}
	verSHA, reason := subTree(fork, "fork tree", "verification")
	if reason != "" {
		return unknown(commit, reason)
	}
	ver, reason := readTree(g, "verification tree", verSHA)
	if reason != "" {
		return unknown(commit, reason)
	}

	entry, found := ver[tag+".md"]
	if !found {
		readme, ok := ver["README.md"]
		switch {
		case !ok:
			return unknown(commit, "verification tree: README missing")
		case classify(readme) != kindFile:
			return unknown(commit, "verification tree: README is not a regular file")
		}
		return recordLookup{State: recordAbsent, Commit: commit}
	}
	switch classify(entry) {
	case kindDir, kindSymlink, kindSubmodule:
		return recordLookup{State: recordNotFile, Commit: commit}
	case kindOther:
		return unknown(commit, "verification tree: "+causeKind)
	}

	var blob blobAnswer
	if cause := getJSON(g, "git/blobs/"+entry.SHA, &blob); cause != "" {
		return unknown(commit, "record blob: "+cause)
	}
	if !fullSHA.MatchString(blob.SHA) || blob.Size == nil {
		return unknown(commit, "record blob: "+causeMalformed)
	}
	if blob.SHA != entry.SHA {
		return unknown(commit, "record blob: "+causeMismatch)
	}
	if blob.Encoding != "base64" {
		return unknown(commit, "record blob: "+causeEncoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return unknown(commit, "record blob: "+causeEncoding)
	}
	if *blob.Size != len(data) {
		return unknown(commit, "record blob: "+causeSize)
	}
	if gitBlobSHA(data) != entry.SHA {
		return unknown(commit, "record blob: "+causeHash)
	}
	return recordLookup{State: recordPresent, Commit: commit, Data: data}
}
