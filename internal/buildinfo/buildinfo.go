// Package buildinfo holds the release identity injected by the release build.
package buildinfo

// Version follows Harness's 0.0.<epoch>-g<commit> format for releases.
// Source builds are deliberately distinguishable from published releases.
var Version = "dev"
