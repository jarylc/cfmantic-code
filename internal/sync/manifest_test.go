package filesync

import (
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"cfmantic-code/internal/walker"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/blake3"
)

// ─── NewFileHashMap ────────────────────────────────────────────────────────────

func TestNewFileHashMap_ReturnsEmptyMap(t *testing.T) {
	m := NewFileHashMap()
	require.NotNil(t, m)
	assert.NotNil(t, m.Files)
	assert.Empty(t, m.Files)
}

// ─── ComputeFileHashMap ────────────────────────────────────────────────────────

func TestComputeFileHashMap_ComputesBLAKE3AndMetadata(t *testing.T) {
	dir := t.TempDir()
	content := []byte("package main\nfunc main() {}")
	absPath := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(absPath, content, 0o644))
	info, err := os.Stat(absPath)
	require.NoError(t, err)

	files := []walker.CodeFile{{AbsPath: absPath, RelPath: "main.go"}}
	m, err := ComputeFileHashMap(files)
	require.NoError(t, err)

	sum := blake3.Sum256(content)
	expected := hex.EncodeToString(sum[:])

	require.Contains(t, m.Files, "main.go")
	assert.Equal(t, expected, m.Files["main.go"].Hash)
	assert.Equal(t, 0, m.Files["main.go"].ChunkCount)
	assert.Equal(t, info.Size(), m.Files["main.go"].Size)
	assert.Equal(t, info.ModTime().UnixNano(), m.Files["main.go"].ModTimeUnixNano)
}

func TestComputeFileHashMap_SkipsUnreadableFile(t *testing.T) {
	files := []walker.CodeFile{{AbsPath: "/nonexistent/path/nope.go", RelPath: "nope.go"}}
	m, err := ComputeFileHashMap(files)
	require.NoError(t, err)
	assert.Empty(t, m.Files)
}

func TestComputeFileHashMap_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	names := []string{"a.go", "b.go", "c.go"}

	files := make([]walker.CodeFile, 0, len(names))

	for _, name := range names {
		absPath := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(absPath, []byte(name), 0o644))
		files = append(files, walker.CodeFile{AbsPath: absPath, RelPath: name})
	}

	m, err := ComputeFileHashMap(files)
	require.NoError(t, err)
	assert.Len(t, m.Files, 3)

	for _, name := range names {
		assert.Contains(t, m.Files, name)
	}
}

func TestComputeFileHashMap_EmptyList(t *testing.T) {
	m, err := ComputeFileHashMap(nil)
	require.NoError(t, err)
	assert.Empty(t, m.Files)
}

func TestHashFile_ComputesBLAKE3(t *testing.T) {
	dir := t.TempDir()
	content := []byte("package main\nfunc main() {}\n")
	absPath := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(absPath, content, 0o644))

	hash, err := hashFile(absPath)
	require.NoError(t, err)

	sum := blake3.Sum256(content)
	assert.Equal(t, hex.EncodeToString(sum[:]), hash)
}

func TestComputeManifestDiff_ReusesEntryWhenMetadataMatches(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(absPath, []byte("package old"), 0o644))

	info, err := os.Stat(absPath)
	require.NoError(t, err)

	files := []walker.CodeFile{{
		AbsPath:         absPath,
		RelPath:         "main.go",
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}}

	old := NewFileHashMap()
	old.Files["main.go"] = FileEntry{
		Hash:            "carried-forward",
		ChunkCount:      7,
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}

	require.NoError(t, os.Remove(absPath))

	diff := ComputeManifestDiff(files, old)
	assert.Empty(t, diff.Changes)
	assert.Equal(t, old.Files["main.go"], diff.Manifest.Files["main.go"])
}

func TestComputeManifestDiff_HashesMetadataCandidatesAndKeepsChunkCount(t *testing.T) {
	dir := t.TempDir()
	content := []byte("package main\n")
	absPath := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(absPath, content, 0o644))

	info, err := os.Stat(absPath)
	require.NoError(t, err)

	files := []walker.CodeFile{{
		AbsPath:         absPath,
		RelPath:         "main.go",
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}}

	sum := blake3.Sum256(content)
	old := NewFileHashMap()
	old.Files["main.go"] = FileEntry{
		Hash:            hex.EncodeToString(sum[:]),
		ChunkCount:      9,
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano() - 1,
	}

	diff := ComputeManifestDiff(files, old)
	assert.Empty(t, diff.Changes)
	assert.Equal(t, 9, diff.Manifest.Files["main.go"].ChunkCount)
	assert.Equal(t, info.Size(), diff.Manifest.Files["main.go"].Size)
	assert.Equal(t, info.ModTime().UnixNano(), diff.Manifest.Files["main.go"].ModTimeUnixNano)
}

func TestComputeManifestDiff_PreservesOldEntryWhenHashingFailsForExistingFile(t *testing.T) {
	dir := t.TempDir()
	brokenPath := filepath.Join(dir, "broken.go")
	require.NoError(t, os.Mkdir(brokenPath, 0o755))

	info, err := os.Stat(brokenPath)
	require.NoError(t, err)

	old := NewFileHashMap()
	old.Files["broken.go"] = FileEntry{
		Hash:            "stale-hash",
		ChunkCount:      4,
		Size:            info.Size() + 1,
		ModTimeUnixNano: info.ModTime().UnixNano() - 1,
	}

	diff := ComputeManifestDiff([]walker.CodeFile{{
		AbsPath:         brokenPath,
		RelPath:         "broken.go",
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
	}}, old)

	assert.Empty(t, diff.Changes)
	require.Contains(t, diff.Manifest.Files, "broken.go")
	assert.Equal(t, old.Files["broken.go"], diff.Manifest.Files["broken.go"])
}

func TestComputeManifestDiff_SkipsUnreadableEntriesAndSortsChanges(t *testing.T) {
	dir := t.TempDir()

	modifiedPath := filepath.Join(dir, "modified.go")
	addedPath := filepath.Join(dir, "added.go")
	skipPath := filepath.Join(dir, "skip.go")

	require.NoError(t, os.WriteFile(modifiedPath, []byte("package main\n// new"), 0o644))
	require.NoError(t, os.WriteFile(addedPath, []byte("package main\n"), 0o644))
	require.NoError(t, os.Mkdir(skipPath, 0o755))

	modifiedInfo, err := os.Stat(modifiedPath)
	require.NoError(t, err)
	addedInfo, err := os.Stat(addedPath)
	require.NoError(t, err)

	old := NewFileHashMap()
	old.Files["modified.go"] = FileEntry{Hash: "stale", ChunkCount: 2}
	old.Files["deleted.go"] = FileEntry{Hash: "gone", ChunkCount: 1}

	diff := ComputeManifestDiff([]walker.CodeFile{
		{AbsPath: modifiedPath, RelPath: "modified.go", Size: modifiedInfo.Size(), ModTimeUnixNano: modifiedInfo.ModTime().UnixNano()},
		{AbsPath: skipPath, RelPath: "skip.go"},
		{AbsPath: addedPath, RelPath: "added.go", Size: addedInfo.Size(), ModTimeUnixNano: addedInfo.ModTime().UnixNano()},
	}, old)

	assert.Equal(t, []FileChange{
		{RelPath: "added.go", Type: Added},
		{RelPath: "deleted.go", Type: Deleted},
		{RelPath: "modified.go", Type: Modified},
	}, diff.Changes)
	assert.NotContains(t, diff.Manifest.Files, "skip.go")
}

// ─── Diff ─────────────────────────────────────────────────────────────────────

func TestDiff(t *testing.T) {
	tests := []struct {
		name   string
		newMap map[string]FileEntry
		oldMap map[string]FileEntry
		want   []FileChange
	}{
		{
			name:   "no changes – identical maps",
			newMap: map[string]FileEntry{"a.go": {Hash: "abc"}},
			oldMap: map[string]FileEntry{"a.go": {Hash: "abc"}},
			want:   nil,
		},
		{
			name:   "added – file present only in new map",
			newMap: map[string]FileEntry{"new.go": {Hash: "xyz"}},
			oldMap: map[string]FileEntry{},
			want:   []FileChange{{RelPath: "new.go", Type: Added}},
		},
		{
			name:   "deleted – file present only in old map",
			newMap: map[string]FileEntry{},
			oldMap: map[string]FileEntry{"old.go": {Hash: "xyz"}},
			want:   []FileChange{{RelPath: "old.go", Type: Deleted}},
		},
		{
			name:   "modified – hash changed",
			newMap: map[string]FileEntry{"a.go": {Hash: "new-hash"}},
			oldMap: map[string]FileEntry{"a.go": {Hash: "old-hash"}},
			want:   []FileChange{{RelPath: "a.go", Type: Modified}},
		},
		{
			name: "mixed changes – sorted by RelPath",
			newMap: map[string]FileEntry{
				"added.go":     {Hash: "h1"},
				"modified.go":  {Hash: "h2-new"},
				"unchanged.go": {Hash: "h3"},
			},
			oldMap: map[string]FileEntry{
				"deleted.go":   {Hash: "h4"},
				"modified.go":  {Hash: "h2-old"},
				"unchanged.go": {Hash: "h3"},
			},
			want: []FileChange{
				{RelPath: "added.go", Type: Added},
				{RelPath: "deleted.go", Type: Deleted},
				{RelPath: "modified.go", Type: Modified},
			},
		},
		{
			name:   "both maps empty – no changes",
			newMap: map[string]FileEntry{},
			oldMap: map[string]FileEntry{},
			want:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newM := &FileHashMap{Files: tc.newMap}
			oldM := &FileHashMap{Files: tc.oldMap}
			got := newM.Diff(oldM)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ─── SetChunkCount ────────────────────────────────────────────────────────────

func TestSetChunkCount(t *testing.T) {
	m := NewFileHashMap()
	m.Files["a.go"] = FileEntry{Hash: "abc", ChunkCount: 0}

	m.SetChunkCount("a.go", 5)
	assert.Equal(t, 5, m.Files["a.go"].ChunkCount)
}

func TestSetChunkCount_NoopOnMissingKey(t *testing.T) {
	m := NewFileHashMap()
	m.SetChunkCount("missing.go", 99) // should not panic or add entry
	_, exists := m.Files["missing.go"]
	assert.False(t, exists)
}

// ─── ChunkCountForFiles ───────────────────────────────────────────────────────

func TestChunkCountForFiles_Sum(t *testing.T) {
	m := &FileHashMap{
		Files: map[string]FileEntry{
			"a.go": {Hash: "h1", ChunkCount: 3},
			"b.go": {Hash: "h2", ChunkCount: 7},
			"c.go": {Hash: "h3", ChunkCount: 2},
		},
	}

	assert.Equal(t, 10, m.ChunkCountForFiles([]string{"a.go", "b.go"}))
	assert.Equal(t, 12, m.ChunkCountForFiles([]string{"a.go", "b.go", "c.go"}))
}

func TestChunkCountForFiles_EmptySlice(t *testing.T) {
	m := NewFileHashMap()
	assert.Equal(t, 0, m.ChunkCountForFiles([]string{}))
}

func TestChunkCountForFiles_MissingKeys(t *testing.T) {
	m := NewFileHashMap()
	assert.Equal(t, 0, m.ChunkCountForFiles([]string{"does-not-exist.go"}))
}

// ─── Save / LoadFileHashMap ───────────────────────────────────────────────────

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "hashes.json")

	m := &FileHashMap{
		Files: map[string]FileEntry{
			"main.go":   {Hash: "abc123", ChunkCount: 5},
			"helper.go": {Hash: "def456", ChunkCount: 3},
		},
	}
	require.NoError(t, m.Save(fp))

	loaded, err := LoadFileHashMap(fp)
	require.NoError(t, err)
	assert.Equal(t, m.Files, loaded.Files)
}

func TestSave_OverwritesExistingFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "hashes.json")

	first := &FileHashMap{Files: map[string]FileEntry{
		"main.go": {Hash: "old", ChunkCount: 1},
	}}
	require.NoError(t, first.Save(fp))

	second := &FileHashMap{Files: map[string]FileEntry{
		"main.go": {Hash: "new", ChunkCount: 2},
	}}
	require.NoError(t, second.Save(fp))

	loaded, err := LoadFileHashMap(fp)
	require.NoError(t, err)
	assert.Equal(t, second.Files, loaded.Files)
}

func TestSave_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	// Deeply nested path – parent does not exist yet.
	fp := filepath.Join(dir, "nested", "deep", "hashes.json")

	m := NewFileHashMap()
	require.NoError(t, m.Save(fp))
	assert.FileExists(t, fp)
}

func TestSave_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	// A regular FILE at the path where Save tries to MkdirAll a directory.
	// os.MkdirAll("file/sub") fails because "file" is not a directory.
	blockingFile := filepath.Join(dir, "notadir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("x"), 0o644))

	m := NewFileHashMap()
	err := m.Save(filepath.Join(blockingFile, "sub", "hashes.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create hash dir")
}

func TestSave_WriteFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test write permission errors when running as root")
	}

	dir := t.TempDir()
	// Parent directory already exists but is not writable → WriteFile(.tmp) fails.
	roDir := filepath.Join(dir, "readonly")
	require.NoError(t, os.Mkdir(roDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	m := NewFileHashMap()
	err := m.Save(filepath.Join(roDir, "hashes.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write tmp hash file")
}

func TestSave_RenameError(t *testing.T) {
	dir := t.TempDir()
	// Put a DIRECTORY at the target path; rename(file → dir) returns EISDIR on Linux.
	hashPath := filepath.Join(dir, "hashes.json")
	require.NoError(t, os.Mkdir(hashPath, 0o755))

	m := NewFileHashMap()
	err := m.Save(hashPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename hash file")
}

func TestSave_EmptyHashMap(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "empty.json")

	m := NewFileHashMap()
	require.NoError(t, m.Save(fp))

	loaded, err := LoadFileHashMap(fp)
	require.NoError(t, err)
	assert.NotNil(t, loaded.Files)
	assert.Empty(t, loaded.Files)
}

func TestSave_JSONContentMatchesMap(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "hashes.json")

	m := &FileHashMap{
		Files: map[string]FileEntry{
			"src/main.go": {Hash: "deadbeef", ChunkCount: 12},
		},
	}
	require.NoError(t, m.Save(fp))

	data, err := os.ReadFile(fp)
	require.NoError(t, err)

	// File must be valid, indented JSON.
	var parsed FileHashMap
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "deadbeef", parsed.Files["src/main.go"].Hash)
	assert.Equal(t, 12, parsed.Files["src/main.go"].ChunkCount)
}

func TestLoadFileHashMap_NonExistent(t *testing.T) {
	m, err := LoadFileHashMap("/tmp/definitely/does-not-exist/hashes.json")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Empty(t, m.Files)
}

func TestLoadFileHashMap_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "corrupt.json")
	require.NoError(t, os.WriteFile(fp, []byte("{not valid json"), 0o644))

	_, err := LoadFileHashMap(fp)
	assert.Error(t, err)
}

func TestLoadFileHashMap_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test file permission errors when running as root")
	}

	dir := t.TempDir()
	fp := filepath.Join(dir, "noperm.json")
	require.NoError(t, os.WriteFile(fp, []byte(`{"files":{}}`), 0o644))
	require.NoError(t, os.Chmod(fp, 0o000))
	t.Cleanup(func() { _ = os.Chmod(fp, 0o644) })

	_, err := LoadFileHashMap(fp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read hash file")
}

func TestLoadFileHashMap_NullFilesField(t *testing.T) {
	// A JSON object with an explicit null "files" key should produce an empty map.
	dir := t.TempDir()
	fp := filepath.Join(dir, "null_files.json")
	require.NoError(t, os.WriteFile(fp, []byte(`{"files": null}`), 0o644))

	m, err := LoadFileHashMap(fp)
	require.NoError(t, err)
	assert.NotNil(t, m.Files)
	assert.Empty(t, m.Files)
}

func TestManifestHelpers_HandleNilInputs(t *testing.T) {
	var hashes *FileHashMap

	clone := hashes.Clone()
	require.NotNil(t, clone)
	assert.Empty(t, clone.Files)
	assert.Zero(t, hashes.ChunkCountForFiles([]string{"missing.go"}))

	changes := (&FileHashMap{Files: map[string]FileEntry{"main.go": {Hash: "hash"}}}).Diff(nil)
	assert.Equal(t, []FileChange{{RelPath: "main.go", Type: Added}}, changes)

	var diff *ManifestDiff
	assert.Nil(t, diff.FilesToProcess(nil))
	assert.Nil(t, diff.RemovedPaths())

	progress := diff.ProgressManifest()
	require.NotNil(t, progress)
	assert.Empty(t, progress.Files)

	added, modified, deleted := diff.ChangeCounts()
	assert.Zero(t, added)
	assert.Zero(t, modified)
	assert.Zero(t, deleted)
}

func TestManifestDiff_HelperViews(t *testing.T) {
	diff := &ManifestDiff{
		Manifest: &FileHashMap{Files: map[string]FileEntry{
			"added.go":     {Hash: "a", ChunkCount: 1},
			"modified.go":  {Hash: "b", ChunkCount: 2},
			"unchanged.go": {Hash: "c", ChunkCount: 3},
		}},
		Changes: []FileChange{
			{RelPath: "added.go", Type: Added},
			{RelPath: "deleted.go", Type: Deleted},
			{RelPath: "modified.go", Type: Modified},
		},
	}

	assert.Equal(t, []string{"deleted.go", "modified.go"}, diff.RemovedPaths())
	assert.Equal(t, []walker.CodeFile{{RelPath: "added.go"}, {RelPath: "modified.go"}}, diff.FilesToProcess([]walker.CodeFile{{RelPath: "deleted.go"}, {RelPath: "modified.go"}, {RelPath: "added.go"}}))
	assert.Equal(t, []string{"unchanged.go"}, mapsKeys(diff.ProgressManifest().Files))

	added, modified, deleted := diff.ChangeCounts()
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, modified)
	assert.Equal(t, 1, deleted)
}

func TestSaveManifest_NilManifestPersistsEmptyMap(t *testing.T) {
	path := t.TempDir()

	require.NoError(t, SaveManifest(path, nil, map[string]int{"ignored.go": 9}))

	loaded, err := LoadFileHashMap(HashFilePath(path))
	require.NoError(t, err)
	assert.Empty(t, loaded.Files)
}

func TestHashFile_ErrorPaths(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := hashFile(filepath.Join(t.TempDir(), "missing.go"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "open file for hashing")
	})

	t.Run("directory read", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "dir")
		require.NoError(t, os.Mkdir(dir, 0o755))

		_, err := hashFile(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stream file for hashing")
	})
}

func TestIsFileFresh_MissingFileReturnsError(t *testing.T) {
	_, err := IsFileFresh(filepath.Join(t.TempDir(), "missing.go"), FileEntry{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stat file freshness")
}

func mapsKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func TestIsFileFresh(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	content := []byte("package main\n")
	require.NoError(t, os.WriteFile(filePath, content, 0o644))

	manifest, err := ComputeFileHashMap([]walker.CodeFile{{
		AbsPath:   filePath,
		RelPath:   "main.go",
		Extension: ".go",
	}})
	require.NoError(t, err)

	fresh, err := IsFileFresh(filePath, manifest.Files["main.go"])
	require.NoError(t, err)
	assert.True(t, fresh)

	require.NoError(t, os.WriteFile(filePath, append(content, []byte("// changed\n")...), 0o644))

	fresh, err = IsFileFresh(filePath, manifest.Files["main.go"])
	require.NoError(t, err)
	assert.False(t, fresh)
}

// ─── HashFilePath ─────────────────────────────────────────────────────────────

func TestHashFilePath_Format(t *testing.T) {
	codebasePath := "/some/codebase/path"
	fp := HashFilePath(codebasePath)

	expectedName := snapshot.CollectionName(codebasePath) + ".json"
	expected := filepath.Join(codebasePath, ".cfmantic", expectedName)

	assert.Equal(t, expected, fp)
	assert.Contains(t, fp, ".cfmantic")
	assert.Contains(t, fp, ".json")
}

func TestFileHashMapSave_CreatesMetadataGitignore(t *testing.T) {
	dir := t.TempDir()
	hashes := NewFileHashMap()

	require.NoError(t, hashes.Save(HashFilePath(dir)))

	gitignore, err := os.ReadFile(filepath.Join(snapshot.MetadataDirPath(dir), ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "*", strings.TrimSpace(string(gitignore)))
}

func TestFileHashMapSave_DoesNotOverwriteExistingGitignore(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(snapshot.MetadataDirPath(dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(snapshot.MetadataDirPath(dir), ".gitignore"), []byte("keep\n"), 0o644))

	hashes := NewFileHashMap()
	require.NoError(t, hashes.Save(HashFilePath(dir)))

	gitignore, err := os.ReadFile(filepath.Join(snapshot.MetadataDirPath(dir), ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "keep\n", string(gitignore))
}

// ─���─ BuildEntity ──────────────────────────────────────────────────────────────

func TestBuildEntity_FieldsAndDeterministicID(t *testing.T) {
	chunk := splitter.Chunk{
		Content:   "func main() {}",
		StartLine: 1,
		EndLine:   3,
	}
	e := BuildEntity("main.go", ".go", "/some/codebase", chunk)

	// ID is non-empty and prefixed correctly.
	assert.NotEmpty(t, e.ID)
	assert.Greater(t, len(e.ID), len("chunk_"), "ID should have a hash suffix")

	// Leading dot stripped from extension.
	assert.Equal(t, "go", e.FileExtension)

	// Content and line metadata match chunk.
	assert.Equal(t, chunk.Content, e.Content)
	assert.Equal(t, "main.go", e.RelativePath)
	assert.Equal(t, 1, e.StartLine)
	assert.Equal(t, 3, e.EndLine)

	// Metadata encodes codebasePath.
	assert.Contains(t, e.Metadata, "codebasePath")
	assert.Contains(t, e.Metadata, "/some/codebase")

	// ID is deterministic (same inputs → same ID).
	e2 := BuildEntity("main.go", ".go", "/some/codebase", chunk)
	assert.Equal(t, e.ID, e2.ID)

	// Different content → different ID.
	other := splitter.Chunk{Content: "different", StartLine: 1, EndLine: 1}
	e3 := BuildEntity("main.go", ".go", "/some/codebase", other)
	assert.NotEqual(t, e.ID, e3.ID)
}

func TestBuildEntity_ExtensionWithoutLeadingDot(t *testing.T) {
	chunk := splitter.Chunk{Content: "x", StartLine: 1, EndLine: 1}

	// Extension with dot → dot is stripped.
	e1 := BuildEntity("a.py", ".py", "/base", chunk)
	assert.Equal(t, "py", e1.FileExtension)

	// Extension without dot → unchanged.
	e2 := BuildEntity("a.py", "py", "/base", chunk)
	assert.Equal(t, "py", e2.FileExtension)
}
