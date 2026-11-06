package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// Rule names for the reasons a YAML file is held.
const (
	ruleYAMLParse     = "yaml-parse"
	ruleYAMLDuplicate = "yaml-duplicate-key"
	ruleYAMLDocuments = "yaml-documents"
	ruleYAMLMerge     = "yaml-merge"
)

// yamlIssue is a reason a YAML document cannot be trusted as parsed.
type yamlIssue struct {
	rule   string
	detail string
}

// parseYAML reads a file in two passes and returns the value tree of its first
// document, with anchors, aliases and merge keys resolved, together with every
// reason the file is held. The value is nil when the file could not be read.
//
// The first pass builds the syntax tree. It rejects a duplicate key by its
// written path and keeps what a value tree loses: the number of documents and
// the place of every merge key. The second pass decodes into plain values, so
// that scalars carry their YAML type and merges are applied.
func parseYAML(data []byte) (any, []yamlIssue) {
	var issues []yamlIssue

	file, err := parser.ParseBytes(data, 0)
	switch {
	case err != nil && isDuplicateKey(err):
		issues = append(issues, yamlIssue{ruleYAMLDuplicate, firstLine(err)})
	case err != nil:
		return nil, []yamlIssue{{ruleYAMLParse, firstLine(err)}}
	default:
		issues = append(issues, syntaxHolds(file)...)
	}

	// Resolution applies a merge and then the mapping's own keys, so a merge
	// override reads as a repeated key. The syntax pass has already judged
	// true duplicates, so the decoder is told to allow them.
	var value any
	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.AllowDuplicateMapKey())
	if err := dec.Decode(&value); err != nil {
		detail := firstLine(err)
		if errors.Is(err, io.EOF) {
			detail = "no document"
		}
		return nil, []yamlIssue{{ruleYAMLParse, detail}}
	}
	return value, issues
}

// syntaxHolds finds what only the syntax tree shows: more than one document, an
// anchor name given to two nodes in one document, a merge key that is not first
// in its mapping, and a sequence merge.
func syntaxHolds(file *ast.File) []yamlIssue {
	var issues []yamlIssue
	if len(file.Docs) > 1 {
		issues = append(issues, yamlIssue{ruleYAMLDocuments, "more than one YAML document"})
	}
	for _, doc := range file.Docs {
		if doc.Body == nil {
			continue
		}
		anchors := anchorIndex{nodes: map[string]ast.Node{}}
		ast.Walk(&anchors, doc.Body)
		issues = append(issues, anchors.reused...)
		// With a name reused, what an alias means depends on how the decoder
		// nests its bindings, so an alias is not classified; the file is held.
		if len(anchors.reused) > 0 {
			anchors.nodes = nil
		}
		ast.Walk(mergeVisitor{&issues, anchors.nodes}, doc.Body)
	}
	return issues
}

// anchorIndex collects the node each anchor name is defined on in one document
// and holds a name defined twice. A name that is only used again is ordinary.
type anchorIndex struct {
	nodes  map[string]ast.Node
	reused []yamlIssue
}

func (a *anchorIndex) Visit(n ast.Node) ast.Visitor {
	if n, ok := n.(*ast.AnchorNode); ok {
		name := n.Name.GetToken().Value
		if _, again := a.nodes[name]; again {
			a.reused = append(a.reused, yamlIssue{ruleYAMLParse, "anchor " + strconv.Quote(name) + " is defined more than once (line " + line(n) + "); a reused anchor name is not supported"})
		} else {
			a.nodes[name] = n.Value
		}
	}
	return a
}

// mergeVisitor walks one document. anchors maps each anchor name to its node,
// or is nil when a reused name makes that map untrustworthy.
type mergeVisitor struct {
	issues  *[]yamlIssue
	anchors map[string]ast.Node
}

func (v mergeVisitor) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.MappingNode:
		for i, mv := range n.Values {
			if !isMergeKey(mv) {
				continue
			}
			if i > 0 {
				*v.issues = append(*v.issues, yamlIssue{ruleYAMLMerge, "merge key is not first in its mapping (line " + line(mv) + ")"})
			}
		}
	case *ast.MappingValueNode:
		if isMergeKey(n) && v.isSequence(n.Value) {
			*v.issues = append(*v.issues, yamlIssue{ruleYAMLMerge, "sequence merge (line " + line(n) + ")"})
		}
	}
	return v
}

// isSequence reports whether a merge operand is a sequence once the anchor that
// wraps it, or the alias that names an anchored node, is looked through. The
// decoder looks through both, so a sequence merge is one whichever way it is
// written. A chain that does not settle within the bound is held as well,
// because an operand that cannot be classified is not accepted.
func (v mergeVisitor) isSequence(node ast.Node) bool {
	for range 32 {
		switch x := node.(type) {
		case *ast.SequenceNode:
			return true
		case *ast.AnchorNode:
			node = x.Value
		case *ast.AliasNode:
			target, ok := v.anchors[x.Value.GetToken().Value]
			if !ok {
				return false // an unknown alias is a parse failure, and a reused name is already held
			}
			node = target
		default:
			return false
		}
	}
	return true
}

func isMergeKey(mv *ast.MappingValueNode) bool {
	return mv != nil && mv.Key != nil && mv.Key.IsMergeKey()
}

func line(n ast.Node) string {
	if tk := n.GetToken(); tk != nil && tk.Position != nil {
		return strconv.Itoa(tk.Position.Line)
	}
	return "?"
}

// isDuplicateKey recognizes the parser's refusal of a repeated key. The library
// words it "mapping key ... already defined"; "duplicate" is accepted too, so a
// rewording that keeps either phrase does not turn a duplicate into a lesser
// parse failure.
func isDuplicateKey(err error) bool {
	s := err.Error()
	return strings.Contains(s, "already defined") || strings.Contains(s, "duplicate")
}

// firstLine keeps an error to one line, so that a verdict stays readable.
func firstLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// walkTree visits a decoded tree depth first and calls onKey for every mapping
// key and onString for every string value, each with the keys that lead to it.
// It returns the path of the first value of a kind the decoder should not
// produce, or "" when every value was understood, so that an unknown shape is
// held and never skipped.
func walkTree(v any, path []string, onKey func(path []string, key string), onString func(path []string, s string)) (unknown string) {
	switch x := v.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return ""
	case string:
		onString(path, x)
	case map[string]any:
		for _, k := range sortedKeys(x) {
			onKey(path, k)
			onString(append(path, k), k)
			if u := walkTree(x[k], append(path[:len(path):len(path)], k), onKey, onString); u != "" && unknown == "" {
				unknown = u
			}
		}
	case []any:
		for i, e := range x {
			if u := walkTree(e, append(path[:len(path):len(path)], "["+strconv.Itoa(i)+"]"), onKey, onString); u != "" && unknown == "" {
				unknown = u
			}
		}
	default:
		return dotted(path) + " (" + reflect.TypeOf(v).String() + ")"
	}
	return unknown
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// dotted renders a path for a finding.
func dotted(path []string) string {
	if len(path) == 0 {
		return "(root)"
	}
	var b strings.Builder
	for i, p := range path {
		if i > 0 && !strings.HasPrefix(p, "[") {
			b.WriteByte('.')
		}
		b.WriteString(p)
	}
	return b.String()
}

// show renders a decoded scalar with its type, so that a finding can tell the
// string "false" from the boolean false.
func show(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s) + " (string)"
	}
	return fmt.Sprintf("%v (%T)", v, v)
}
