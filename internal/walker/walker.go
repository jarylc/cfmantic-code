package walker

import (
	"bytes"
	"cfmantic-code/internal/snapshot"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	gitignore "github.com/sabhiram/go-gitignore"
)

const sniffBytes = 512

// ErrRootNotAbsolute is returned by Walk when root is not an absolute path.
var ErrRootNotAbsolute = errors.New("walker: root must be an absolute path")

var errNilContext = errors.New("walker: nil context")

var (
	computeRelPath     = filepath.Rel
	matchIgnoreFn      = matchesIgnore
	dirEntryInfo       = func(d fs.DirEntry) (fs.FileInfo, error) { return d.Info() }
	execCommandContext = exec.CommandContext
)

// CodeFile represents a single source file discovered during a walk.
type CodeFile struct {
	AbsPath         string
	RelPath         string
	Extension       string
	Size            int64
	ModTimeUnixNano int64
}

var skipDirs = map[string]bool{
	"node_modules":           true,
	"vendor":                 true,
	"__pycache__":            true,
	".git":                   true,
	"dist":                   true,
	"build":                  true,
	".next":                  true,
	"target":                 true,
	"bin":                    true,
	"obj":                    true,
	".tox":                   true,
	".venv":                  true,
	"venv":                   true,
	snapshot.MetadataDirName: true,
}

type ignoreRule struct {
	base    string
	dirOnly bool
	gi      *gitignore.GitIgnore
	negate  bool
}

// Walk recursively walks root, returning all text files while respecting
// recursive .gitignore/.indexignore files, Git global excludes, and extraIgnorePatterns.
func Walk(ctx context.Context, root string, extraIgnorePatterns []string) ([]CodeFile, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%w: got %q", ErrRootNotAbsolute, root)
	}

	if ctx == nil {
		return nil, errNilContext
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	rootRules := loadRootIgnoreRules(ctx, root, extraIgnorePatterns)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	dirRules := map[string][]ignoreRule{root: rootRules}

	var files []CodeFile

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walk canceled: %w", err)
		}

		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		name := d.Name()

		if d.IsDir() {
			if path == root {
				return nil
			}

			if skipDirs[name] {
				return filepath.SkipDir
			}

			rules := dirRules[filepath.Dir(path)]

			ignored, err := matchIgnoreFn(path, rules, true)
			if err != nil {
				return fmt.Errorf("computing relative path for directory: %w", err)
			}

			if ignored {
				return filepath.SkipDir
			}

			if err := ctx.Err(); err != nil {
				return fmt.Errorf("walk canceled: %w", err)
			}

			dirRules[path] = appendLocalIgnoreRules(rules, path)

			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))

		rel, err := computeRelPath(root, path)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}

		ignored, err := matchIgnoreFn(path, dirRules[filepath.Dir(path)], false)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}

		if ignored {
			return nil
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walk canceled: %w", err)
		}

		isText, err := isTextFile(path)
		if err != nil {
			return fmt.Errorf("sniffing file type for %s: %w", path, err)
		}

		if !isText {
			return nil
		}

		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walk canceled: %w", err)
		}

		info, err := dirEntryInfo(d)
		if err != nil {
			return fmt.Errorf("stat file %s: %w", path, err)
		}

		files = append(files, CodeFile{
			AbsPath:         path,
			RelPath:         rel,
			Extension:       ext,
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})

	return files, nil
}

func loadRootIgnoreRules(ctx context.Context, root string, extraIgnorePatterns []string) []ignoreRule {
	rules := appendIgnoreRules(nil, root, readGlobalIgnorePatterns(ctx, root))
	rules = appendIgnoreRules(rules, root, readGitInfoExcludePatterns(ctx, root))

	if ctx != nil && ctx.Err() != nil {
		return rules
	}

	rules = appendLocalIgnoreRules(rules, root)

	return appendIgnoreRules(rules, root, extraIgnorePatterns)
}

func appendLocalIgnoreRules(rules []ignoreRule, dir string) []ignoreRule {
	rules = appendIgnoreRules(rules, dir, readIgnorePatterns(filepath.Join(dir, ".gitignore")))

	return appendIgnoreRules(rules, dir, readIgnorePatterns(filepath.Join(dir, ".indexignore")))
}

func readGlobalIgnorePatterns(ctx context.Context, root string) []string {
	return readIgnorePatterns(resolveGlobalIgnorePath(ctx, root))
}

func readGitInfoExcludePatterns(ctx context.Context, root string) []string {
	return readIgnorePatterns(resolveGitInfoExcludePath(ctx, root))
}

func resolveGlobalIgnorePath(ctx context.Context, root string) string {
	if path := resolveGitConfiguredExcludesFile(ctx, root); path != "" {
		return path
	}

	return defaultGlobalIgnorePath()
}

func resolveGitConfiguredExcludesFile(ctx context.Context, root string) string {
	return runGitCommand(ctx, root, "config", "--path", "core.excludesFile")
}

func resolveGitInfoExcludePath(ctx context.Context, root string) string {
	path := runGitCommand(ctx, root, "rev-parse", "--git-path", "info/exclude")
	if path == "" {
		return ""
	}

	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(root, path)
}

func runGitCommand(ctx context.Context, root string, args ...string) string {
	if ctx == nil {
		return ""
	}

	if err := ctx.Err(); err != nil {
		return ""
	}

	cmd := execCommandContext(ctx, "git", args...)
	cmd.Dir = root

	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func defaultGlobalIgnorePath() string {
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "git", "ignore")
	}

	home := os.Getenv("HOME")
	if home == "" {
		return ""
	}

	return filepath.Join(home, ".config", "git", "ignore")
}

func readIgnorePatterns(path string) []string {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var patterns []string

	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}

	return patterns
}

func appendIgnoreRules(rules []ignoreRule, base string, patterns []string) []ignoreRule {
	if len(patterns) == 0 {
		return rules
	}

	out := make([]ignoreRule, 0, len(rules)+len(patterns))
	out = append(out, rules...)

	for _, pattern := range patterns {
		rule := ignoreRule{base: base}

		if strings.HasPrefix(pattern, "!") {
			rule.negate = true
			pattern = pattern[1:]
		}

		rule.dirOnly = strings.HasSuffix(pattern, "/")

		rule.gi = gitignore.CompileIgnoreLines(pattern)
		out = append(out, rule)
	}

	return out
}

func matchesIgnore(path string, rules []ignoreRule, isDir bool) (bool, error) {
	ignored := false

	for _, rule := range rules {
		rel, err := filepath.Rel(rule.base, path)
		if err != nil {
			return false, fmt.Errorf("relative path from %s to %s: %w", rule.base, path, err)
		}

		if rule.gi.MatchesPath(rel) || (isDir && rule.dirOnly && rule.gi.MatchesPath(rel+string(filepath.Separator))) {
			ignored = !rule.negate
		}
	}

	return ignored, nil
}

func isTextFile(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	var buf [sniffBytes]byte

	n, err := file.Read(buf[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read file prefix: %w", err)
	}

	sample := buf[:n]
	if len(sample) == 0 {
		return true, nil
	}

	if bytes.IndexByte(sample, 0) >= 0 {
		return false, nil
	}

	contentType := http.DetectContentType(sample)
	if strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		contentType == "application/xml" ||
		contentType == "image/svg+xml" {
		return true, nil
	}

	if !utf8.Valid(sample) {
		return false, nil
	}

	for _, b := range sample {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' && b != '\f' {
			return false, nil
		}
	}

	return true, nil
}
