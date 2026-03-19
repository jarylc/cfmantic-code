// Package filesync provides incremental file-level change detection and
// background sync of indexed codebases into the vector store.
package filesync

import (
	"cfmantic-code/internal/fileutil"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/walker"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"github.com/zeebo/blake3"
)

// ChangeType classifies how a file changed between two snapshots.
type ChangeType int

const (
	Added ChangeType = iota
	Modified
	Deleted
)

// FileChange describes a single changed file.
type FileChange struct {
	RelPath string
	Type    ChangeType
}

// FileEntry stores per-file metadata for change detection and chunk accounting.
type FileEntry struct {
	Hash            string `json:"hash"`
	ChunkCount      int    `json:"chunkCount"`
	Size            int64  `json:"size"`
	ModTimeUnixNano int64  `json:"modTimeUnixNano"`
}

type ManifestDiff struct {
	Manifest *FileHashMap
	Changes  []FileChange
}

// FileHashMap is a flat map of file relative path to FileEntry,
// sufficient for file-level change detection and chunk count tracking.
type FileHashMap struct {
	Files map[string]FileEntry `json:"files"` // relPath → file info
}

// NewFileHashMap returns an empty FileHashMap.
func NewFileHashMap() *FileHashMap {
	return &FileHashMap{Files: make(map[string]FileEntry)}
}

// ComputeFileHashMap reads each file and computes its BLAKE3 hash.
// Files that cannot be read are skipped with a log message.
// ChunkCount is initialized to 0 and must be set later by the caller via SetChunkCount.
func ComputeFileHashMap(files []walker.CodeFile) (*FileHashMap, error) {
	m := NewFileHashMap()

	for _, f := range files {
		entry, ok := computeFileEntry(f)
		if !ok {
			continue
		}

		m.Files[f.RelPath] = entry
	}

	return m, nil
}

func ComputeManifestDiff(files []walker.CodeFile, old *FileHashMap) *ManifestDiff {
	if old == nil {
		old = NewFileHashMap()
	}

	manifest := NewFileHashMap()
	changes := make([]FileChange, 0)

	for _, f := range files {
		oldEntry, exists := old.Files[f.RelPath]
		if exists && sameFileMetadata(oldEntry, f.Size, f.ModTimeUnixNano) {
			manifest.Files[f.RelPath] = oldEntry
			continue
		}

		entry, ok := computeFileEntry(f)
		if !ok {
			if exists {
				manifest.Files[f.RelPath] = oldEntry
			}

			continue
		}

		if exists && entry.Hash == oldEntry.Hash {
			entry.ChunkCount = oldEntry.ChunkCount
		}

		manifest.Files[f.RelPath] = entry

		if !exists {
			changes = append(changes, FileChange{RelPath: f.RelPath, Type: Added})
		} else if entry.Hash != oldEntry.Hash {
			changes = append(changes, FileChange{RelPath: f.RelPath, Type: Modified})
		}
	}

	for relPath := range old.Files {
		if _, exists := manifest.Files[relPath]; !exists {
			changes = append(changes, FileChange{RelPath: relPath, Type: Deleted})
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		return changes[i].RelPath < changes[j].RelPath
	})

	return &ManifestDiff{Manifest: manifest, Changes: changes}
}

func computeFileEntry(file walker.CodeFile) (FileEntry, bool) {
	if file.Size == 0 || file.ModTimeUnixNano == 0 {
		info, err := os.Stat(file.AbsPath)
		if err != nil {
			log.Printf("filesync: skip stat for %s: %v", file.AbsPath, err)
			return FileEntry{}, false
		}

		file.Size = info.Size()
		file.ModTimeUnixNano = info.ModTime().UnixNano()
	}

	hash, err := hashFile(file.AbsPath)
	if err != nil {
		log.Printf("filesync: skip hash for %s: %v", file.AbsPath, err)
		return FileEntry{}, false
	}

	return FileEntry{
		Hash:            hash,
		Size:            file.Size,
		ModTimeUnixNano: file.ModTimeUnixNano,
	}, true
}

func sameFileMetadata(entry FileEntry, size, modTimeUnixNano int64) bool {
	return entry.Size == size && entry.ModTimeUnixNano == modTimeUnixNano
}

// IsFileFresh reports whether the file still matches the stored manifest entry.
func IsFileFresh(filePath string, entry FileEntry) (bool, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return false, fmt.Errorf("stat file freshness: %w", err)
	}

	return sameFileMetadata(entry, info.Size(), info.ModTime().UnixNano()), nil
}

func hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file for hashing: %w", err)
	}
	defer file.Close()

	hasher := blake3.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("stream file for hashing: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Diff compares the receiver (new state) against old and returns a
// sorted, deterministic list of changes.
func (m *FileHashMap) Diff(old *FileHashMap) []FileChange {
	if old == nil {
		old = NewFileHashMap()
	}

	var changes []FileChange

	for relPath, newEntry := range m.Files {
		oldEntry, exists := old.Files[relPath]
		if !exists {
			changes = append(changes, FileChange{RelPath: relPath, Type: Added})
		} else if newEntry.Hash != oldEntry.Hash {
			changes = append(changes, FileChange{RelPath: relPath, Type: Modified})
		}
	}

	for relPath := range old.Files {
		if _, exists := m.Files[relPath]; !exists {
			changes = append(changes, FileChange{RelPath: relPath, Type: Deleted})
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		return changes[i].RelPath < changes[j].RelPath
	})

	return changes
}

func (m *FileHashMap) Clone() *FileHashMap {
	clone := NewFileHashMap()
	if m == nil {
		return clone
	}

	maps.Copy(clone.Files, m.Files)

	return clone
}

func (m *FileHashMap) ApplyChunkCounts(chunkCounts map[string]int) {
	for relPath, count := range chunkCounts {
		m.SetChunkCount(relPath, count)
	}
}

func SaveManifest(codebasePath string, manifest *FileHashMap, chunkCounts map[string]int) error {
	if manifest == nil {
		manifest = NewFileHashMap()
	}

	persisted := manifest.Clone()
	persisted.ApplyChunkCounts(chunkCounts)

	return persisted.Save(HashFilePath(codebasePath))
}

// SetChunkCount updates the ChunkCount for an existing entry.
func (m *FileHashMap) SetChunkCount(relPath string, count int) {
	if entry, ok := m.Files[relPath]; ok {
		entry.ChunkCount = count
		m.Files[relPath] = entry
	}
}

// ChunkCountForFiles returns the sum of ChunkCount for the given paths.
func (m *FileHashMap) ChunkCountForFiles(relPaths []string) int {
	if m == nil {
		return 0
	}

	total := 0

	for _, relPath := range relPaths {
		if entry, ok := m.Files[relPath]; ok {
			total += entry.ChunkCount
		}
	}

	return total
}

func (d *ManifestDiff) FilesToProcess(files []walker.CodeFile) []walker.CodeFile {
	if d == nil || len(d.Changes) == 0 {
		return nil
	}

	fileMap := make(map[string]walker.CodeFile, len(files))
	for _, file := range files {
		fileMap[file.RelPath] = file
	}

	toProcess := make([]walker.CodeFile, 0, len(d.Changes))
	for _, change := range d.Changes {
		if change.Type != Added && change.Type != Modified {
			continue
		}

		if file, ok := fileMap[change.RelPath]; ok {
			toProcess = append(toProcess, file)
		}
	}

	return toProcess
}

func (d *ManifestDiff) RemovedPaths() []string {
	if d == nil {
		return nil
	}

	removed := make([]string, 0, len(d.Changes))
	for _, change := range d.Changes {
		if change.Type == Deleted || change.Type == Modified {
			removed = append(removed, change.RelPath)
		}
	}

	return removed
}

func (d *ManifestDiff) ProgressManifest() *FileHashMap {
	if d == nil {
		return NewFileHashMap()
	}

	progress := d.Manifest.Clone()
	for _, change := range d.Changes {
		if change.Type == Added || change.Type == Modified {
			delete(progress.Files, change.RelPath)
		}
	}

	return progress
}

func (d *ManifestDiff) ChangeCounts() (int, int, int) {
	if d == nil {
		return 0, 0, 0
	}

	var added, modified, deleted int

	for _, change := range d.Changes {
		switch change.Type {
		case Added:
			added++
		case Modified:
			modified++
		case Deleted:
			deleted++
		}
	}

	return added, modified, deleted
}

// Save marshals the FileHashMap to indented JSON and writes it via a
// tmp-then-rename strategy. The parent directory is created if needed.
// On Windows, overwriting an existing file falls back to remove-then-rename.
func (m *FileHashMap) Save(filePath string) error {
	dir := filepath.Dir(filePath)

	var err error
	if filepath.Base(dir) == snapshot.MetadataDirName {
		err = snapshot.EnsureMetadataDirPath(dir)
	} else {
		err = os.MkdirAll(dir, 0o750)
	}

	if err != nil {
		return fmt.Errorf("filesync: create hash dir: %w", err)
	}

	data, _ := json.MarshalIndent(m, "", "  ") //nolint:errcheck,errchkjson // FileHashMap only contains JSON-serializable fields.

	tmp := filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("filesync: write tmp hash file: %w", err)
	}

	if err := fileutil.ReplaceFile(tmp, filePath); err != nil {
		return fmt.Errorf("filesync: rename hash file: %w", err)
	}

	return nil
}

// LoadFileHashMap reads a FileHashMap from filePath.
// If the file does not exist, an empty FileHashMap is returned (not an error).
func LoadFileHashMap(filePath string) (*FileHashMap, error) {
	data, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return NewFileHashMap(), nil
	}

	if err != nil {
		return nil, fmt.Errorf("filesync: read hash file: %w", err)
	}

	var m FileHashMap
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("filesync: unmarshal hashes: %w", err)
	}

	if m.Files == nil {
		m.Files = make(map[string]FileEntry)
	}

	return &m, nil
}

// HashFilePath returns the path where hashes for codebasePath are persisted:
// <codebasePath>/.cfmantic/<collection_name>.json
func HashFilePath(codebasePath string) string {
	name := snapshot.CollectionName(codebasePath) + ".json"
	return filepath.Join(snapshot.MetadataDirPath(codebasePath), name)
}
