package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestFindingString(t *testing.T) {
	f := Finding{Path: ".github/workflows/new.yml", Rule: "workflow-set", Detail: "not in the permitted list"}
	want := ".github/workflows/new.yml: workflow-set: not in the permitted list"
	if got := f.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestSortFindings(t *testing.T) {
	a := Finding{"a.yml", "r1", "x"}
	b := Finding{"a.yml", "r2", "x"}
	c := Finding{"b.yml", "r1", "x"}
	d := Finding{"b.yml", "r1", "y"}

	t.Run("orders by path, rule, detail", func(t *testing.T) {
		got := sortFindings([]Finding{d, c, b, a})
		if want := []Finding{a, b, c, d}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("drops exact duplicates", func(t *testing.T) {
		got := sortFindings([]Finding{a, a, b})
		if want := []Finding{a, b}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("does not modify its input", func(t *testing.T) {
		in := []Finding{b, a}
		sortFindings(in)
		if in[0] != b {
			t.Fatalf("input reordered: %v", in)
		}
	})
	t.Run("empty stays empty", func(t *testing.T) {
		if got := sortFindings(nil); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
}

func TestVerdict(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		got := verdict("release", nil)
		if !strings.Contains(got, "release") || !strings.Contains(got, "pass") {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("held names every item", func(t *testing.T) {
		fs := []Finding{{"a.yml", "r1", "one"}, {"b.yml", "r2", "two"}}
		got := verdict("release", fs)
		if !strings.Contains(got, "held") || !strings.Contains(got, "2") {
			t.Fatalf("no count of held items in %q", got)
		}
		for _, f := range fs {
			if !strings.Contains(got, f.String()) {
				t.Fatalf("%q does not name %q", got, f)
			}
		}
		if strings.Contains(got, "pass") {
			t.Fatalf("held verdict says pass: %q", got)
		}
	})
}
