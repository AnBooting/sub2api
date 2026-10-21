//go:build unit

package buildcontract

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	goDirectivePattern = regexp.MustCompile(`(?m)^go (\d+\.\d+\.\d+)`)
	rootBuilderPattern = regexp.MustCompile(`(?m)^ARG GOLANG_IMAGE=golang:(\d+\.\d+\.\d+)-alpine`)
	backendBuilderPattern = regexp.MustCompile(`(?m)^FROM golang:(\d+\.\d+\.\d+)-alpine`)
	workflowAssertionPattern = regexp.MustCompile(`go version \| grep -q 'go(\d+\.\d+\.\d+)'`)
)

// repoRoot returns the repository root, which is three levels above this
// package (backend/internal/buildcontract).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	require.FileExists(t, filepath.Join(root, "backend", "go.mod"))
	require.FileExists(t, filepath.Join(root, "Dockerfile"))
	return root
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	return string(raw)
}

func requiredGoVersion(t *testing.T, root string) string {
	t.Helper()
	match := goDirectivePattern.FindStringSubmatch(readFile(t, filepath.Join(root, "backend", "go.mod")))
	require.NotNil(t, match, "backend/go.mod must declare a go directive with a full version")
	return match[1]
}

// The root Dockerfile is the default build entry point of the repository
// (docker build .). Its builder image must be at least as new as the module
// minimum, because the official Go images run with GOTOOLCHAIN=local and will
// not fetch a newer toolchain by themselves.
func TestRootDockerfileBuilderMeetsModuleMinimum(t *testing.T) {
	root := repoRoot(t)
	required := requiredGoVersion(t, root)
	match := rootBuilderPattern.FindStringSubmatch(readFile(t, filepath.Join(root, "Dockerfile")))
	require.NotNil(t, match, "the root Dockerfile must pin the Go builder image")
	require.Equal(t, required, match[1],
		"the root Dockerfile builder image and the go directive in backend/go.mod must be the same version")
}

// Every builder image and every workflow assertion has to name the same
// version as backend/go.mod, so that a local build, the container build and
// the CI run all use one toolchain.
func TestBuildInputsShareTheModuleGoVersion(t *testing.T) {
	root := repoRoot(t)
	required := requiredGoVersion(t, root)

	backendMatch := backendBuilderPattern.FindStringSubmatch(readFile(t, filepath.Join(root, "backend", "Dockerfile")))
	require.NotNil(t, backendMatch, "backend/Dockerfile must pin the Go builder image")
	require.Equal(t, required, backendMatch[1], "backend/Dockerfile builder image must match backend/go.mod")

	deployMatch := rootBuilderPattern.FindStringSubmatch(readFile(t, filepath.Join(root, "deploy", "Dockerfile")))
	require.NotNil(t, deployMatch, "deploy/Dockerfile must pin the Go builder image")
	require.Equal(t, required, deployMatch[1], "deploy/Dockerfile builder image must match backend/go.mod")

	workflows := []string{
		".github/workflows/backend-ci.yml",
		".github/workflows/release.yml",
		".github/workflows/security-scan.yml",
	}
	for _, workflow := range workflows {
		content := readFile(t, filepath.Join(root, filepath.FromSlash(workflow)))
		match := workflowAssertionPattern.FindStringSubmatch(content)
		require.NotNil(t, match, "%s must assert the installed Go version", workflow)
		require.Equal(t, required, match[1], "%s must assert the version declared in backend/go.mod", workflow)
	}
}
