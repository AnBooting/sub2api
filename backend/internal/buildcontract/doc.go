// Package buildcontract holds tests that lock the build inputs of this
// repository together.
//
// The Go toolchain version is pinned in several places at once: the go
// directive in backend/go.mod, the builder image in the root Dockerfile, the
// builder image in backend/Dockerfile and deploy/Dockerfile, and the version
// assertion in the three GitHub Actions workflows. The official Go images set
// GOTOOLCHAIN=local and refuse to download a newer toolchain on their own, so
// a builder that is older than the module minimum makes the build fail with
// "go.mod requires go >= ...". A missing entry is just as bad as a wrong one:
// the default build entry point of the repository is the root Dockerfile.
//
// The tests in this package therefore compare every one of those places
// against the version declared in backend/go.mod.
package buildcontract
