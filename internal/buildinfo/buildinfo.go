// Package buildinfo holds the release identity injected by the release build.
package buildinfo

// Releases use Harness's 0.0.<epoch>-g<commit> format.
// Source builds use a distinct identity.
var Version = "dev"
