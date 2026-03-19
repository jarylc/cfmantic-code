package walker

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createFile writes content to path, creating parent dirs as needed.
func createFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// relPaths extracts RelPath fields from a []CodeFile for easy assertion.
func relPaths(files []CodeFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.RelPath
	}

	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – input validation
// ─────────────────────────────────────────────────────────────────────────────

func TestWalk_RelativePath_ReturnsError(t *testing.T) {
	_, err := Walk(context.Background(), "relative/path", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute path")
}

func TestWalk_RelativePath_SentinelError(t *testing.T) {
	_, err := Walk(context.Background(), "relative/path", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRootNotAbsolute, "error must wrap ErrRootNotAbsolute for programmatic inspection")
}

func TestWalk_NonExistentAbsolutePath_ReturnsError(t *testing.T) {
	_, err := Walk(context.Background(), "/this/path/does/not/exist/at/all", nil)
	require.Error(t, err)
}

func TestWalk_CanceledContext_StopsTraversalPromptly(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "a.go"), "package a")
	createFile(t, filepath.Join(root, "b.go"), "package b")

	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	ctx, cancel := context.WithCancel(context.Background())
	visited := make([]string, 0, 2)

	old := matchIgnoreFn
	matchIgnoreFn = func(path string, rules []ignoreRule, isDir bool) (bool, error) {
		if !isDir {
			visited = append(visited, filepath.Base(path))
			if len(visited) == 1 {
				cancel()
			}
		}

		return old(path, rules, isDir)
	}

	t.Cleanup(func() { matchIgnoreFn = old })

	files, err := Walk(ctx, root, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "walking directory")
	assert.Nil(t, files)
	assert.Equal(t, []string{"a.go"}, visited)
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – text and binary detection
// ─────────────────────────────────────────────────────────────────────────────

func TestWalk_IncludesAllTextFiles(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.go"), "package main")
	createFile(t, filepath.Join(root, "notes.txt"), "hello")
	createFile(t, filepath.Join(root, "template.hbs"), "{{title}}")
	createFile(t, filepath.Join(root, "README"), "docs")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"README", "main.go", "notes.txt", "template.hbs"}, relPaths(files))
}

func TestWalk_ExcludesBinaryFiles(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.go"), "package main")
	require.NoError(t, os.WriteFile(filepath.Join(root, "generated.go"), []byte{0x00, 0x01, 0x02, 0x03}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "image.bin"), []byte{0x89, 0x50, 0x4e, 0x47, 0x00}, 0o644))

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

func TestWalk_ExtensionlessTextFilesIncluded(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "Makefile"), "all:\n\t@true\n")
	createFile(t, filepath.Join(root, "README"), "docs")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, []string{"Makefile", "README"}, relPaths(files))

	for _, file := range files {
		assert.Empty(t, file.Extension)
	}
}

func TestWalk_ExtensionFieldNormalized(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.GO"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, ".go", files[0].Extension, "Extension field should be lowercase")
}

func TestWalk_AbsPathSet(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, filepath.Join(root, "main.go"), files[0].AbsPath)
	assert.Equal(t, "main.go", files[0].RelPath)
}

func TestWalk_FileMetadataSet(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	content := "package main\n"
	createFile(t, path, content)

	info, err := os.Stat(path)
	require.NoError(t, err)

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	require.Len(t, files, 1)

	assert.Equal(t, info.Size(), files[0].Size)
	assert.Equal(t, info.ModTime().UnixNano(), files[0].ModTimeUnixNano)
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – skipDirs
// ─────────────────────────────��───────────────────────────────────────────────

func TestWalk_SkipDirs_AllKnownDirsSkipped(t *testing.T) {
	skipDirNames := []string{
		"node_modules", "vendor", "__pycache__", ".git",
		"dist", "build", ".next", "target", "bin", "obj",
		".tox", ".venv", "venv", ".cfmantic",
	}

	for _, dirName := range skipDirNames {
		t.Run(dirName, func(t *testing.T) {
			root := t.TempDir()
			createFile(t, filepath.Join(root, dirName, "file.go"), "package x")
			createFile(t, filepath.Join(root, "main.go"), "package main")

			files, err := Walk(context.Background(), root, nil)
			require.NoError(t, err)

			rels := relPaths(files)
			assert.Equal(t, []string{"main.go"}, rels,
				"files inside %q should be skipped", dirName)
		})
	}
}

func TestWalk_DotDirectories_TraversedUnlessIgnored(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".hidden", "secret.go"), "package x")
	createFile(t, filepath.Join(root, ".config", "app.go"), "package x")
	createFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "name: ci\n")
	createFile(t, filepath.Join(root, ".devcontainer", "devcontainer.json"), "{}")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		filepath.Join(".config", "app.go"),
		filepath.Join(".devcontainer", "devcontainer.json"),
		filepath.Join(".github", "workflows", "ci.yml"),
		filepath.Join(".hidden", "secret.go"),
		"main.go",
	}, relPaths(files))
}

func TestWalk_DotDirectories_RespectGitignore(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), ".hidden/\n")
	createFile(t, filepath.Join(root, ".hidden", "secret.go"), "package x")
	createFile(t, filepath.Join(root, ".config", "app.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		filepath.Join(".config", "app.go"),
		".gitignore",
		"main.go",
	}, relPaths(files))
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – .gitignore integration
// ─────────────────────────────────────────────────────────────────────────────

func TestWalk_Gitignore_FilesExcluded(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "ignored.go\n")
	createFile(t, filepath.Join(root, "ignored.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore", "main.go"}, relPaths(files))
}

func TestWalk_Gitignore_IgnoredUnreadableFileSkippedBeforeTextSniff(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks are not enforced")
	}

	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "ignored.go\n")

	ignored := filepath.Join(root, "ignored.go")
	createFile(t, ignored, "package ignored")
	require.NoError(t, os.Chmod(ignored, 0o000))
	t.Cleanup(func() { _ = os.Chmod(ignored, 0o644) })

	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore", "main.go"}, relPaths(files))
}

func TestWalk_Gitignore_DirectoryExcluded(t *testing.T) {
	root := t.TempDir()
	// Pattern with trailing slash: go-gitignore matches file paths inside but
	// NOT the bare directory entry itself, so files get filtered at the file level.
	createFile(t, filepath.Join(root, ".gitignore"), "generated/\n")
	createFile(t, filepath.Join(root, "generated", "code.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore", "main.go"}, relPaths(files))
}

func TestWalk_Gitignore_DirectorySkippedViaMatchesPath(t *testing.T) {
	// Pattern without trailing slash: go-gitignore matches the bare dir name,
	// triggering the filepath.SkipDir branch inside the WalkDir callback.
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "generated\n")
	createFile(t, filepath.Join(root, "generated", "code.go"), "package x")
	createFile(t, filepath.Join(root, "generated", "more.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore", "main.go"}, relPaths(files))
}

func TestWalk_Gitignore_CommentsAndEmptyLinesIgnored(t *testing.T) {
	root := t.TempDir()
	// Comments (#) and blank lines must not be treated as patterns
	createFile(t, filepath.Join(root, ".gitignore"), "# this is a comment\n\n   \nignored.go\n")
	createFile(t, filepath.Join(root, "ignored.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore", "main.go"}, relPaths(files))
}

func TestWalk_Gitignore_WildcardPattern(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "*.gen.go\n")
	createFile(t, filepath.Join(root, "foo.gen.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore", "main.go"}, relPaths(files))
}

func TestWalk_Indexignore_FilesExcluded(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".indexignore"), "ignored.go\n")
	createFile(t, filepath.Join(root, "ignored.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".indexignore", "main.go"}, relPaths(files))
}

func TestWalk_DefaultGlobalIgnoreFallback_FilesExcluded(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	createFile(t, filepath.Join(home, ".config", "git", "ignore"), "global.go\n")
	createFile(t, filepath.Join(root, "global.go"), "package ignored")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

func TestWalk_MissingGitBinary_FallsBackSafely(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	createFile(t, filepath.Join(home, ".config", "git", "ignore"), "global.go\n")
	createFile(t, filepath.Join(root, "global.go"), "package ignored")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

func TestWalk_ConfiguredCoreExcludesFile_FilesExcluded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	root := t.TempDir()
	home := t.TempDir()
	configDir := t.TempDir()
	globalIgnore := filepath.Join(configDir, "global-ignore")
	globalConfig := filepath.Join(configDir, "gitconfig")

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)

	createFile(t, globalIgnore, "configured.go\n")
	createFile(t, globalConfig, "[core]\n\texcludesFile = "+globalIgnore+"\n")
	createFile(t, filepath.Join(root, "configured.go"), "package ignored")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

func TestWalk_GitInfoExclude_FilesExcluded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	root := t.TempDir()

	cmd := exec.CommandContext(context.Background(), "git", "init", "--quiet", root)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	createFile(t, filepath.Join(root, ".git", "info", "exclude"), "local.go\n")
	createFile(t, filepath.Join(root, "local.go"), "package ignored")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

func TestWalk_GitCommandsReceiveProvidedContext(t *testing.T) {
	type ctxKey struct{}

	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.go"), "package main")

	ctx := context.WithValue(context.Background(), ctxKey{}, "walk-context")

	old := execCommandContext

	var captured []context.Context

	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		captured = append(captured, ctx)
		return &exec.Cmd{Path: "nonexistent-git", Args: append([]string{"nonexistent-git"}, args...)}
	}

	t.Cleanup(func() { execCommandContext = old })

	files, err := Walk(ctx, root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
	require.Len(t, captured, 2)

	for _, got := range captured {
		assert.Same(t, ctx, got)
		assert.Equal(t, "walk-context", got.Value(ctxKey{}))
	}
}

func TestWalk_Gitignore_NestedNegationOverridesRootIgnore(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "ignored.go\n")
	createFile(t, filepath.Join(root, "ignored.go"), "package root")
	createFile(t, filepath.Join(root, "pkg", ".gitignore"), "!ignored.go\n")
	createFile(t, filepath.Join(root, "pkg", "ignored.go"), "package pkg")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		".gitignore",
		filepath.Join("pkg", ".gitignore"),
		filepath.Join("pkg", "ignored.go"),
	}, relPaths(files))
}

func TestWalk_Gitignore_NestedNegationOverridesParentPathPattern(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "pkg/*\n")
	createFile(t, filepath.Join(root, "pkg", ".gitignore"), "!keep.go\n")
	createFile(t, filepath.Join(root, "pkg", "keep.go"), "package pkg")
	createFile(t, filepath.Join(root, "pkg", "drop.go"), "package pkg")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		".gitignore",
		filepath.Join("pkg", "keep.go"),
	}, relPaths(files))
}

func TestWalk_Indexignore_NestedRulesAppliedAfterGitignore(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "pkg/*\n")
	createFile(t, filepath.Join(root, "pkg", ".gitignore"), "keep.go\n")
	createFile(t, filepath.Join(root, "pkg", ".indexignore"), "!keep.go\n")
	createFile(t, filepath.Join(root, "pkg", "keep.go"), "package pkg")
	createFile(t, filepath.Join(root, "pkg", "drop.go"), "package pkg")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		".gitignore",
		filepath.Join("pkg", "keep.go"),
	}, relPaths(files))
}

func TestWalk_Gitignore_IgnoredParentDirectoryNotTraversedByNestedNegation(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "generated/\n")
	createFile(t, filepath.Join(root, "generated", ".gitignore"), "!keep.go\n")
	createFile(t, filepath.Join(root, "generated", "keep.go"), "package generated")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore"}, relPaths(files))
}

func TestWalk_NoGitignore_NoError(t *testing.T) {
	root := t.TempDir()
	// No .gitignore file present
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – extraIgnorePatterns
// ─────────────────────────────────────────────────────────────────────────────

func TestWalk_ExtraIgnorePatterns_FilesExcluded(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "generated.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, []string{"generated.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

func TestWalk_ExtraIgnorePatterns_DirectoryExcluded(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "proto", "types.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, []string{"proto/"})
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

func TestWalk_ExtraIgnorePatterns_CombinedWithGitignore(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, ".gitignore"), "gitignored.go\n")
	createFile(t, filepath.Join(root, "gitignored.go"), "package x")
	createFile(t, filepath.Join(root, "extra.go"), "package x")
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, []string{"extra.go"})
	require.NoError(t, err)
	assert.Equal(t, []string{".gitignore", "main.go"}, relPaths(files))
}

func TestWalk_NilExtraIgnorePatterns_WithNoGitignore_GiIsNil(t *testing.T) {
	// Ensures the gi==nil branch is exercised (no patterns at all).
	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – sorting and traversal
// ─────────────────────────────────────────────────────────────────────────────

func TestWalk_ResultsSortedByRelPath(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "z_last.go"), "")
	createFile(t, filepath.Join(root, "a_first.go"), "")
	createFile(t, filepath.Join(root, "m_middle.go"), "")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"a_first.go", "m_middle.go", "z_last.go"}, relPaths(files))
}

func TestWalk_NestedDirectories_Traversed(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "cmd", "main.go"), "package main")
	createFile(t, filepath.Join(root, "internal", "pkg", "lib.go"), "package pkg")
	createFile(t, filepath.Join(root, "README.md"), "# readme")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)

	rels := relPaths(files)
	assert.Contains(t, rels, filepath.Join("cmd", "main.go"))
	assert.Contains(t, rels, filepath.Join("internal", "pkg", "lib.go"))
	assert.Contains(t, rels, "README.md")
}

func TestWalk_NestedResultsSorted(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "b", "file.go"), "")
	createFile(t, filepath.Join(root, "a", "file.go"), "")
	createFile(t, filepath.Join(root, "root.go"), "")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		filepath.Join("a", "file.go"),
		filepath.Join("b", "file.go"),
		"root.go",
	}, relPaths(files))
}

func TestWalk_EmptyDirectory_ReturnsEmpty(t *testing.T) {
	root := t.TempDir()

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestWalk_DirectoryWithOnlySkippedFiles_ReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "data.bin"), []byte{0x00, 0x01, 0x02}, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "image.png"), []byte{0x89, 0x50, 0x4e, 0x47, 0x00}, 0o644))

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Empty(t, files)
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – symlink handling
// ─────────────────────────────────────────────────────────────────────────────

func TestWalk_SymlinkFile_Skipped(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create a real file outside the codebase root
	outsideFile := filepath.Join(outside, "secret.go")
	createFile(t, outsideFile, "package secret")

	// Create a symlink inside root pointing to the outside file
	symlinkPath := filepath.Join(root, "link.go")
	require.NoError(t, os.Symlink(outsideFile, symlinkPath))

	// Also create a regular file that should be found
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)

	rels := relPaths(files)
	assert.Equal(t, []string{"main.go"}, rels, "symlinked file must not appear in results")
}

func TestWalk_SymlinkDir_Skipped(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create a real file inside a directory outside root
	createFile(t, filepath.Join(outside, "secret.go"), "package secret")

	// Create a symlink directory inside root pointing to the outside directory
	symlinkDir := filepath.Join(root, "linked")
	require.NoError(t, os.Symlink(outside, symlinkDir))

	// Also create a regular file that should be found
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)

	rels := relPaths(files)
	assert.Equal(t, []string{"main.go"}, rels, "files inside symlinked directory must not appear in results")
}

func TestWalk_NamedPipe_SkippedAsNonRegular(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mkfifo is not supported on windows")
	}

	root := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(root, "events.pipe"), 0o644))
	createFile(t, filepath.Join(root, "main.go"), "package main")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, relPaths(files))
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – error propagation from WalkDir callback
// ─────────────────────────────────────────────────────────────────────────────

func TestWalk_PermissionDenied_SubDirectory_ReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks are not enforced")
	}

	root := t.TempDir()

	// Create a subdirectory that the walker cannot read.
	restricted := filepath.Join(root, "restricted")
	require.NoError(t, os.MkdirAll(restricted, 0o000))
	t.Cleanup(func() { _ = os.Chmod(restricted, 0o755) }) // restore so TempDir cleanup works

	// A regular file at root level that would otherwise be matched.
	createFile(t, filepath.Join(root, "main.go"), "package main")

	_, err := Walk(context.Background(), root, nil)
	// filepath.WalkDir propagates the permission error from the callback.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "walking directory")
}

func TestWalk_UnreadableFile_ReturnsSniffError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks are not enforced")
	}

	root := t.TempDir()
	blocked := filepath.Join(root, "blocked.go")
	createFile(t, blocked, "package blocked")
	require.NoError(t, os.Chmod(blocked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o644) })

	_, err := Walk(context.Background(), root, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sniffing file type")
}

func TestWalk_DirectoryIgnoreError_ReturnsWrappedError(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "pkg", "main.go"), "package main")

	old := matchIgnoreFn
	matchIgnoreFn = func(path string, rules []ignoreRule, isDir bool) (bool, error) {
		if isDir && filepath.Base(path) == "pkg" {
			return false, errors.New("boom")
		}

		return old(path, rules, isDir)
	}

	t.Cleanup(func() { matchIgnoreFn = old })

	_, err := Walk(context.Background(), root, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "computing relative path for directory")
	assert.ErrorContains(t, err, "boom")
}

func TestWalk_FileRelPathError_ReturnsWrappedError(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.go"), "package main")

	old := computeRelPath
	computeRelPath = func(base, target string) (string, error) {
		if filepath.Base(target) == "main.go" {
			return "", errors.New("boom")
		}

		return old(base, target)
	}

	t.Cleanup(func() { computeRelPath = old })

	_, err := Walk(context.Background(), root, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "computing relative path")
	assert.ErrorContains(t, err, "boom")
}

func TestWalk_FileIgnoreError_ReturnsWrappedError(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.go"), "package main")

	old := matchIgnoreFn
	matchIgnoreFn = func(path string, rules []ignoreRule, isDir bool) (bool, error) {
		if !isDir && filepath.Base(path) == "main.go" {
			return false, errors.New("boom")
		}

		return old(path, rules, isDir)
	}

	t.Cleanup(func() { matchIgnoreFn = old })

	_, err := Walk(context.Background(), root, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "computing relative path")
	assert.ErrorContains(t, err, "boom")
}

func TestWalk_FileInfoError_ReturnsWrappedError(t *testing.T) {
	root := t.TempDir()
	createFile(t, filepath.Join(root, "main.go"), "package main")

	old := dirEntryInfo
	dirEntryInfo = func(d fs.DirEntry) (fs.FileInfo, error) {
		if d.Name() == "main.go" {
			return nil, errors.New("boom")
		}

		return old(d)
	}

	t.Cleanup(func() { dirEntryInfo = old })

	_, err := Walk(context.Background(), root, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stat file")
	assert.ErrorContains(t, err, "boom")
}

func TestWalk_DeeplyNested_AllFilesFound(t *testing.T) {
	root := t.TempDir()

	// Create a 5-level deep directory hierarchy.
	createFile(t, filepath.Join(root, "a", "b", "c", "d", "e", "deep.go"), "package deep")
	createFile(t, filepath.Join(root, "a", "b", "c", "d", "mid.go"), "package mid")
	createFile(t, filepath.Join(root, "a", "b", "shallow.go"), "package shallow")
	createFile(t, filepath.Join(root, "root.go"), "package root")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)

	rels := relPaths(files)
	assert.Contains(t, rels, "root.go")
	assert.Contains(t, rels, filepath.Join("a", "b", "shallow.go"))
	assert.Contains(t, rels, filepath.Join("a", "b", "c", "d", "mid.go"))
	assert.Contains(t, rels, filepath.Join("a", "b", "c", "d", "e", "deep.go"))
	assert.Len(t, files, 4)
}

// ─────────────────────────────────────────────────────────────────────────────
// Walk – mixed scenarios
// ─────────────────────────────────────────────────────────────────────────────

func TestWalk_MixedTextFilesAndSkipsAndGitignore(t *testing.T) {
	root := t.TempDir()

	// Regular files to be found
	createFile(t, filepath.Join(root, "main.go"), "package main")
	createFile(t, filepath.Join(root, "lib", "util.py"), "def foo(): pass")

	// File that should be gitignored
	createFile(t, filepath.Join(root, ".gitignore"), "*.gen.go\ntmp/\n")
	createFile(t, filepath.Join(root, "auto.gen.go"), "package gen")
	createFile(t, filepath.Join(root, "tmp", "scratch.go"), "package tmp")

	// File inside a skipped dir
	createFile(t, filepath.Join(root, "vendor", "dep.go"), "package dep")

	// Text file with an otherwise arbitrary extension should still be included.
	createFile(t, filepath.Join(root, "style.css"), "body{}")

	files, err := Walk(context.Background(), root, nil)
	require.NoError(t, err)

	rels := relPaths(files)

	assert.Contains(t, rels, "main.go")
	assert.Contains(t, rels, filepath.Join("lib", "util.py"))
	assert.Contains(t, rels, "style.css")
	assert.NotContains(t, rels, "auto.gen.go")
	assert.NotContains(t, rels, filepath.Join("tmp", "scratch.go"))
	assert.NotContains(t, rels, filepath.Join("vendor", "dep.go"))
}

func TestResolveGitInfoExcludePath_AbsolutePathPreserved(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	root := t.TempDir()
	cmd := exec.CommandContext(context.Background(), "git", "init", "--quiet", root)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	absGitDir := filepath.Join(root, ".git")
	t.Setenv("GIT_DIR", absGitDir)

	assert.Equal(t, filepath.Join(absGitDir, "info", "exclude"), resolveGitInfoExcludePath(context.Background(), root))
}

func TestDefaultGlobalIgnorePath_PrefersXDGConfigHome(t *testing.T) {
	xdgConfigHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	t.Setenv("HOME", t.TempDir())

	assert.Equal(t, filepath.Join(xdgConfigHome, "git", "ignore"), defaultGlobalIgnorePath())
}

func TestDefaultGlobalIgnorePath_EmptyHomeReturnsEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	assert.Empty(t, defaultGlobalIgnorePath())
}

func TestMatchesIgnore_InvalidBaseReturnsError(t *testing.T) {
	rules := appendIgnoreRules(nil, "relative-base", []string{"ignored.go"})

	_, err := matchesIgnore(filepath.Join(t.TempDir(), "ignored.go"), rules, false)
	require.Error(t, err)
	assert.ErrorContains(t, err, "relative path")
}

func TestIsTextFile_OpenError(t *testing.T) {
	_, err := isTextFile(filepath.Join(t.TempDir(), "missing.txt"))
	require.Error(t, err)
	assert.ErrorContains(t, err, "open file")
}

func TestIsTextFile_ReadErrorOnDirectory(t *testing.T) {
	_, err := isTextFile(t.TempDir())
	require.Error(t, err)
	assert.ErrorContains(t, err, "read file prefix")
}

func TestIsTextFile_InvalidUTF8Rejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.dat")
	require.NoError(t, os.WriteFile(path, []byte{0x01, 0x80}, 0o644))

	isText, err := isTextFile(path)
	require.NoError(t, err)
	assert.False(t, isText)
}

func TestIsTextFile_ControlBytesRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.dat")
	require.NoError(t, os.WriteFile(path, []byte{0x01, 'A', 'B'}, 0o644))

	isText, err := isTextFile(path)
	require.NoError(t, err)
	assert.False(t, isText)
}

func TestIsTextFile_ValidUTF8FallbackAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bmp-signature.txt")
	require.NoError(t, os.WriteFile(path, []byte{'B', 'M'}, 0o644))

	isText, err := isTextFile(path)
	require.NoError(t, err)
	assert.True(t, isText)
}
