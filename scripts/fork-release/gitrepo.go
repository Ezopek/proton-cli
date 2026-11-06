package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"time"
)

// treeEntry is one line of git ls-tree -r: the mode, the object type, the
// object id and the path.
type treeEntry struct {
	mode string
	typ  string
	oid  string
	path string
}

// gitRepo is everything the guard asks of git. It is the one seam between the
// pure checks and the process: tests replace it with a fake, or point the
// real adapter at a scratch repository.
type gitRepo interface {
	// OriginURLs returns every effective fetch URL of origin, or every
	// effective push URL with push set, after insteadOf rewriting.
	OriginURLs(push bool) ([]string, error)
	// FetchEzop fetches refs/heads/ezop of origin once and returns the full
	// SHA of the commit it fetched, read from FETCH_HEAD.
	FetchEzop() (string, error)
	// ResolveCommit peels rev to a commit and returns its full SHA.
	ResolveCommit(rev string) (string, error)
	// IsAncestor returns the exit status of git merge-base --is-ancestor. An
	// error means git could not be run at all.
	IsAncestor(ancestor, descendant string) (int, error)
	// Tree lists the entries of a commit's tree, recursively, limited to the
	// paths (the whole tree with none).
	Tree(commit string, paths []string) ([]treeEntry, error)
	// Blob returns the content of a blob.
	Blob(oid string) ([]byte, error)
	// OriginTags lists the tags of origin as the remote has them now, by name
	// without refs/tags/, each with the commit or object it names, peeled. It
	// writes no local ref.
	OriginTags() (map[string]string, error)
}

// execGit is the git adapter over the git program. It is a thin layer: it
// builds an argument list, runs git and parses the output. Every read that a
// SHA must keep its meaning for runs with git's own --no-replace-objects, so a
// replacement ref cannot change what a hash reads as. No error it returns
// carries git's output, which can hold a URL.
type execGit struct {
	// dir is the repository to work in.
	dir string
	// env is the environment of every git call; nil means the process's own.
	env []string
	// timeout bounds every git call; zero means 60 seconds.
	timeout time.Duration
}

const defaultGitTimeout = 60 * time.Second

// newExecGit is the adapter over the repository in the working directory, with
// terminal prompts off.
func newExecGit() execGit {
	return execGit{dir: ".", env: append(os.Environ(), "GIT_TERMINAL_PROMPT=0")}
}

// run runs git and returns its standard output and exit status. The error is
// non-nil only when git could not be run to completion (not found, killed by
// the timeout); a non-zero exit is a status.
func (g execGit) run(args ...string) ([]byte, int, error) {
	timeout := g.timeout
	if timeout == 0 {
		timeout = defaultGitTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.dir
	cmd.Env = g.env
	cmd.WaitDelay = time.Second
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Standard error and input are dropped: git's diagnostics can name a URL.
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		return stdout.Bytes(), exit.ExitCode(), nil
	}
	return nil, -1, errors.New("git could not be run to completion")
}

// bound runs a read whose result is bound to a hash, without replace refs.
func (g execGit) bound(args ...string) ([]byte, int, error) {
	return g.run(append([]string{"--no-replace-objects"}, args...)...)
}

func (g execGit) OriginURLs(push bool) ([]string, error) {
	args := []string{"remote", "get-url", "--all"}
	if push {
		args = append(args, "--push")
	}
	out, status, err := g.run(append(args, "origin")...)
	if err != nil {
		return nil, err
	}
	if status != 0 {
		return nil, errors.New("origin is not configured or cannot be read")
	}
	// Each URL is exactly one line of git's output, newline excluded. Nothing
	// else is trimmed: a URL with surrounding whitespace or a carriage return
	// is not the canonical remote and is refused by the origin check.
	lines := strings.Split(string(out), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

func (g execGit) FetchEzop() (string, error) {
	if _, status, err := g.run("fetch", "--no-tags", "origin", "refs/heads/ezop"); err != nil || status != 0 {
		return "", errors.New("could not fetch refs/heads/ezop from origin")
	}
	return g.revParseCommit("FETCH_HEAD")
}

func (g execGit) ResolveCommit(rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", errors.New("not a revision")
	}
	return g.revParseCommit(rev)
}

// revParseCommit peels rev to a commit and returns its full SHA. rev follows
// --end-of-options, so it can never be read as an option.
func (g execGit) revParseCommit(rev string) (string, error) {
	out, status, err := g.bound("rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil || status != 0 {
		return "", errors.New("does not name a commit")
	}
	sha := strings.TrimSpace(string(out))
	if !fullSHA.MatchString(sha) {
		return "", errors.New("did not resolve to a full commit SHA")
	}
	return sha, nil
}

func (g execGit) IsAncestor(ancestor, descendant string) (int, error) {
	if !fullSHA.MatchString(ancestor) || !fullSHA.MatchString(descendant) {
		return -1, errors.New("not a full commit SHA")
	}
	_, status, err := g.bound("merge-base", "--is-ancestor", ancestor, descendant)
	return status, err
}

func (g execGit) Tree(commit string, paths []string) ([]treeEntry, error) {
	if !fullSHA.MatchString(commit) {
		return nil, errors.New("not a full commit SHA")
	}
	args := []string{"ls-tree", "-r", "-z", "--full-tree", commit}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, status, err := g.bound(args...)
	if err != nil || status != 0 {
		return nil, errors.New("could not list the tree")
	}
	var entries []treeEntry
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, p, ok := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, errors.New("unexpected tree listing")
		}
		entries = append(entries, treeEntry{mode: fields[0], typ: fields[1], oid: fields[2], path: p})
	}
	return entries, nil
}

func (g execGit) Blob(oid string) ([]byte, error) {
	if !fullSHA.MatchString(oid) {
		return nil, errors.New("not a full object id")
	}
	out, status, err := g.bound("cat-file", "blob", oid)
	if err != nil || status != 0 {
		return nil, errors.New("could not read the blob")
	}
	return out, nil
}

// OriginTags asks origin for its tags with git ls-remote, which reads the
// remote and writes no local ref. origin must be a configured remote: without
// that, git would take the word as a path. Success with no line means the
// remote has no tags.
func (g execGit) OriginTags() (map[string]string, error) {
	if _, err := g.OriginURLs(false); err != nil {
		return nil, errors.New("origin is not configured or cannot be read")
	}
	out, status, err := g.run("ls-remote", "--tags", "origin")
	if err != nil || status != 0 {
		return nil, errors.New("could not list the tags of origin")
	}
	return parseOriginTags(out)
}

// parseOriginTags reads git ls-remote --tags output: one "<oid>\trefs/tags/<name>"
// line per tag, and for an annotated tag a second "<name>^{}" line that carries
// the peeled commit and wins. Anything else, a tag or a peel listed twice or a
// peel with no tag is an error, never a guess.
func parseOriginTags(out []byte) (map[string]string, error) {
	bad := errors.New("unexpected tag listing from origin")
	direct, peeled := map[string]string{}, map[string]string{}
	text := string(out)
	if text != "" {
		text = strings.TrimSuffix(text, "\n")
		for _, line := range strings.Split(text, "\n") {
			oid, ref, ok := strings.Cut(line, "\t")
			name, isTag := strings.CutPrefix(ref, "refs/tags/")
			if !ok || !isTag || !fullSHA.MatchString(oid) {
				return nil, bad
			}
			into := direct
			if base, isPeel := strings.CutSuffix(name, "^{}"); isPeel {
				name, into = base, peeled
			}
			if name == "" || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
				return nil, bad
			}
			if _, dup := into[name]; dup {
				return nil, bad
			}
			into[name] = oid
		}
	}
	for name, oid := range peeled {
		if _, ok := direct[name]; !ok {
			return nil, bad
		}
		direct[name] = oid
	}
	return direct, nil
}

// commitTreeFS is the view of a commit as a file system, limited to paths: its
// tree and its blobs, never the working tree.
func commitTreeFS(g gitRepo, commit string, paths []string) (fs.FS, error) {
	entries, err := g.Tree(commit, paths)
	if err != nil {
		return nil, err
	}
	return buildTreeFS(entries, paths, g.Blob)
}

// buildTreeFS turns tree entries into a file system. Only entries at or under
// one of paths are kept, and only the blob of a regular file inside them is
// read. A symlink, a submodule and anything else that is not a plain file
// becomes a non-regular entry that cannot be read, and is not followed.
func buildTreeFS(entries []treeEntry, paths []string, read func(oid string) ([]byte, error)) (fs.FS, error) {
	for _, e := range entries {
		if !fs.ValidPath(e.path) || e.path == "." {
			return nil, errors.New("the tree has an entry with an unusable path")
		}
	}
	view := &treeFS{files: map[string]treeNode{}, dirs: map[string][]string{".": nil}}
	for _, e := range entries {
		if !within(e.path, paths) {
			continue
		}
		node := treeNode{mode: fs.ModeIrregular}
		switch {
		case e.typ == "blob" && (e.mode == "100644" || e.mode == "100755"):
			data, err := read(e.oid)
			if err != nil {
				return nil, errors.New("could not read a blob of the tree")
			}
			node = treeNode{mode: 0o644, data: data}
			if e.mode == "100755" {
				node.mode = 0o755
			}
		case e.typ == "blob" && e.mode == "120000":
			node.mode = fs.ModeSymlink | 0o777
		}
		view.files[e.path] = node
		for p := e.path; p != "."; p = path.Dir(p) {
			parent := path.Dir(p)
			if !slices.Contains(view.dirs[parent], path.Base(p)) {
				view.dirs[parent] = append(view.dirs[parent], path.Base(p))
			}
		}
	}
	for _, names := range view.dirs {
		slices.Sort(names)
	}
	return view, nil
}

// within reports whether p is one of paths or lies under one of them.
func within(p string, paths []string) bool {
	for _, q := range paths {
		if p == q || strings.HasPrefix(p, q+"/") {
			return true
		}
	}
	return false
}

// treeNode is a file of a treeFS.
type treeNode struct {
	mode fs.FileMode
	data []byte
}

// treeFS is a read-only file system over the entries of a git tree. Unlike
// fstest.MapFS it never follows a symlink: a symlink is an entry of its own
// that cannot be opened for reading.
type treeFS struct {
	files map[string]treeNode
	// dirs maps every directory, "." included, to its sorted child names.
	dirs map[string][]string
}

var errNotRegular = errors.New("not a regular file")

func (v *treeFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if names, ok := v.dirs[name]; ok {
		return &treeDir{view: v, name: name, names: names}, nil
	}
	if node, ok := v.files[name]; ok {
		return &treeFile{name: name, node: node, r: bytes.NewReader(node.data)}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

type treeInfo struct {
	name string
	size int64
	mode fs.FileMode
}

func (i treeInfo) Name() string       { return i.name }
func (i treeInfo) Size() int64        { return i.size }
func (i treeInfo) Mode() fs.FileMode  { return i.mode }
func (i treeInfo) ModTime() time.Time { return time.Time{} }
func (i treeInfo) IsDir() bool        { return i.mode.IsDir() }
func (i treeInfo) Sys() any           { return nil }

type treeFile struct {
	name string
	node treeNode
	r    *bytes.Reader
}

func (f *treeFile) Stat() (fs.FileInfo, error) {
	return treeInfo{path.Base(f.name), int64(len(f.node.data)), f.node.mode}, nil
}

func (f *treeFile) Read(b []byte) (int, error) {
	if !f.node.mode.IsRegular() {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: errNotRegular}
	}
	return f.r.Read(b)
}

func (f *treeFile) Close() error { return nil }

type treeDir struct {
	view   *treeFS
	name   string
	names  []string
	offset int
}

func (d *treeDir) Stat() (fs.FileInfo, error) {
	return treeInfo{path.Base(d.name), 0, fs.ModeDir | 0o555}, nil
}

func (d *treeDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: errors.New("is a directory")}
}

func (d *treeDir) Close() error { return nil }

func (d *treeDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.names[d.offset:]
	if n > 0 && len(rest) > n {
		rest = rest[:n]
	}
	if n > 0 && len(rest) == 0 {
		return nil, io.EOF
	}
	d.offset += len(rest)
	out := make([]fs.DirEntry, 0, len(rest))
	for _, name := range rest {
		full := path.Join(d.name, name)
		if _, isDir := d.view.dirs[full]; isDir {
			out = append(out, fs.FileInfoToDirEntry(treeInfo{name, 0, fs.ModeDir | 0o555}))
			continue
		}
		node := d.view.files[full]
		out = append(out, fs.FileInfoToDirEntry(treeInfo{name, int64(len(node.data)), node.mode}))
	}
	return out, nil
}

// tagFetcher is the optional capability of the real adapter that fetches the
// objects of Upstream release tags. It is a separate interface so that the
// gitRepo interface and every fake stay as they are; a git without it makes the
// pre-push guard refuse.
type tagFetcher interface {
	FetchTags(names []string) error
}

// fetchTagsArgs is the exact argument list of FetchTags: one explicit
// refs/tags/<name> refspec per name and no destination, so only FETCH_HEAD is
// written. A glob is not a valid refspec without a destination, and a name is
// never accepted unless it is an Upstream release tag, so nothing a remote
// lists can become an option, a destination or a force. An empty list is
// refused.
func fetchTagsArgs(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, errors.New("no tag names to fetch")
	}
	args := []string{"fetch", "--no-tags", "origin"}
	for _, name := range names {
		if !upstreamTagPattern.MatchString(name) {
			return nil, errors.New("not an Upstream release tag name")
		}
		args = append(args, "refs/tags/"+name)
	}
	return args, nil
}

// FetchTags fetches the named Upstream release tags from origin into FETCH_HEAD
// only, so that their objects are present for the ancestry test. It creates and
// updates no local ref or tag. One name that origin lacks fails the whole fetch.
func (g execGit) FetchTags(names []string) error {
	args, err := fetchTagsArgs(names)
	if err != nil {
		return err
	}
	if _, status, err := g.run(args...); err != nil || status != 0 {
		return errors.New("could not fetch the Upstream release tags from origin")
	}
	return nil
}
