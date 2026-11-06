package main

// allowedActions is the closed list of actions an enabled workflow may use, as
// full owner/repo[/path]@sha strings. It is seeded from the actions the tree
// already uses. A new action, or a new SHA for an old one, is held until a
// reviewed fork commit adds it here.
var allowedActions = map[string]bool{
	"goreleaser/goreleaser-action@f06c13b6b1a9625abc9e6e439d9c05a8f2190e94":     true, // v7.2.3
	"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1":                 true, // v7.0.1
	"actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e":                 true, // v7.0.0
	"jetify-com/devbox-install-action@8c6a66ed6273138b1915457069de78cb52fe3bd7": true, // v0.15.0
	"cachix/install-nix-action@13d8dd58da0234aa297dedd986986ccb8e7f3e24":        true, // v31.11.1
}

// goreleaserAction is the repository of the one action that writes a release.
const goreleaserAction = "goreleaser/goreleaser-action"
