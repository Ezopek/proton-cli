package passfile

import (
	"testing"

	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// What is shown is what is taken back, rule and all.
func TestAURLRuleReadsBackAsItWasWritten(t *testing.T) {
	for _, written := range []string{
		"https://github.com",
		"exact-host:https://github.com",
		"exact-url:https://github.com/login",
		"never:https://gist.github.com",
		"pattern:*.github.com/*",
		"regex:^https://(www\\.)?github\\.com/",
		"starts-with:https://github.com/settings",
	} {
		if got := ParseURLRule(written).String(); got != written {
			t.Errorf("%q reads back as %q", written, got)
		}
	}
}

// A scheme or a port before the first colon is part of the address.
func TestAnAddressWithAColonIsNotARule(t *testing.T) {
	for _, written := range []string{"https://github.com", "localhost:8080", "never-mind.example:443"} {
		got := ParseURLRule(written)
		if got.Mode != pb.AutofillUrl_Default || got.Value != written {
			t.Errorf("%q reads as %v", written, got)
		}
	}
}

// A pattern and a regular expression name no site.
func TestOnlyAnAddressNamesASite(t *testing.T) {
	for written, want := range map[string]bool{
		"github.com":                    true,
		"never:https://gist.github.com": true,
		"pattern:*.github.com":          false,
		"regex:github":                  false,
	} {
		if got := ParseURLRule(written).Address(); got != want {
			t.Errorf("%q names a site: %v, want %v", written, got, want)
		}
	}
}

// An older app fills on every address it is shown, so it is shown only the
// ones under the default rule.
func TestOnlyTheDefaultRuleReachesAnOlderApp(t *testing.T) {
	login := &pb.ItemLogin{}
	SetLoginURLs(login, []URLRule{
		ParseURLRule("https://github.com"),
		ParseURLRule("never:https://gist.github.com"),
	})
	if urls := login.GetUrls(); len(urls) != 1 || urls[0] != "https://github.com" {
		t.Errorf("an older app would fill on %v", urls)
	}
	if len(login.GetAutofillUrls()) != 2 {
		t.Errorf("the rules came back as %v", login.GetAutofillUrls())
	}
}
