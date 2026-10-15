package passfile

import (
	"fmt"
	"strings"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// URLRule is one address a login carries, and the rule that decides where Pass
// fills the login in.
type URLRule struct {
	Value string
	Mode  pb.AutofillUrl_Mode
}

// ruleWords are how each rule is written in front of its value. The default -
// the address, its parent domain and their subdomains - has no word, so a plain
// address means what it always has.
var ruleWords = map[pb.AutofillUrl_Mode]string{
	pb.AutofillUrl_Exact:             "exact-host",
	pb.AutofillUrl_ExactPath:         "exact-url",
	pb.AutofillUrl_Never:             "never",
	pb.AutofillUrl_Pattern:           "pattern",
	pb.AutofillUrl_RegularExpression: "regex",
	pb.AutofillUrl_StartWith:         "starts-with",
}

// String writes the rule the way `items get` shows it and --url takes it.
func (r URLRule) String() string {
	if r.Mode == pb.AutofillUrl_Default {
		return r.Value
	}
	word, ok := ruleWords[r.Mode]
	if !ok {
		word = fmt.Sprintf("rule-%d", int32(r.Mode))
	}
	return word + ":" + r.Value
}

// ParseURLRule reads a rule the way --url takes it.
//
// What stands before the first colon is a rule only when it is one of the rule
// words. Anything else - a scheme, a host with a port - is part of an address
// under the default rule.
func ParseURLRule(s string) URLRule {
	word, value, found := strings.Cut(s, ":")
	if found {
		for mode, w := range ruleWords {
			if w == word {
				return URLRule{Value: value, Mode: mode}
			}
		}
	}
	return URLRule{Value: s, Mode: pb.AutofillUrl_Default}
}

// Address reports whether the value is an address rather than a pattern or a
// regular expression, which is what decides whether it names a site.
func (r URLRule) Address() bool {
	return r.Mode != pb.AutofillUrl_Pattern && r.Mode != pb.AutofillUrl_RegularExpression
}

// LoginURLs are the rules a login item carries.
//
// The app keeps them in autofill_urls, and repeats the default ones in urls for
// clients too old to know the field. An item written by such a client has only
// urls, so that is what is read when the newer field is empty.
func LoginURLs(l *pb.ItemLogin) []URLRule {
	autofill := l.GetAutofillUrls()
	if len(autofill) == 0 {
		return defaultRules(l.GetUrls())
	}
	out := make([]URLRule, 0, len(autofill))
	for _, u := range autofill {
		out = append(out, URLRule{Value: u.GetUrl(), Mode: u.GetMode()})
	}
	return out
}

// SetLoginURLs puts rules on a login item, writing both fields the app writes.
//
// urls is what an older client reads, and it fills wherever an address in it
// matches, so it carries only the addresses under the default rule.
func SetLoginURLs(l *pb.ItemLogin, rules []URLRule) {
	autofill := make([]*pb.AutofillUrl, 0, len(rules))
	urls := make([]string, 0, len(rules))
	for _, r := range rules {
		autofill = append(autofill, &pb.AutofillUrl{Url: r.Value, Mode: r.Mode})
		if r.Mode == pb.AutofillUrl_Default {
			urls = append(urls, r.Value)
		}
	}
	l.AutofillUrls = autofill
	l.Urls = urls
}

// defaultRules are addresses under the default rule.
func defaultRules(urls []string) []URLRule {
	out := make([]URLRule, 0, len(urls))
	for _, u := range urls {
		out = append(out, URLRule{Value: u})
	}
	return out
}

// defaultURLs are the addresses among rules that fill under the default rule.
func defaultURLs(rules []URLRule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		if r.Mode == pb.AutofillUrl_Default {
			out = append(out, r.Value)
		}
	}
	return out
}
