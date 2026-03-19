package splitter

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"maps"
	"math"
	"strings"
	"unicode/utf8"
)

// ASTSplitter splits source code using tree-sitter AST parsing, falling back
// to TextSplitter for unsupported languages or parse failures.
type ASTSplitter struct {
	ChunkSize int
	Overlap   int
	fallback  *TextSplitter
}

// NewASTSplitter creates an ASTSplitter with the given chunk size and overlap.
func NewASTSplitter(chunkSize, overlap int) *ASTSplitter {
	return &ASTSplitter{
		ChunkSize: chunkSize,
		Overlap:   overlap,
		fallback:  NewTextSplitter(chunkSize, overlap),
	}
}

type resolvedLanguage struct {
	ext     string
	grammar grammarBundle
}

type astUnit struct {
	text      string
	startLine int
	endLine   int
	size      int
}

const astLanguageDetectionSniffBytes = 8 * 1024

// Split splits content into chunks using AST-aware parsing when possible.
func (s *ASTSplitter) Split(reader io.Reader, filePath string, emit EmitChunkFunc) error {
	bufferedReader := bufio.NewReaderSize(reader, astLanguageDetectionSniffBytes)

	resolved := resolveLanguageForASTSplit(bufferedReader, filePath)

	if resolved == nil || resolved.grammar.Support != grammarSupportSupportedNow {
		return s.fallback.Split(bufferedReader, filePath, emit)
	}

	var err error

	contentBytes, err := io.ReadAll(bufferedReader)
	if err != nil {
		return fmt.Errorf("read splitter input: %w", err)
	}

	return s.splitResolvedContent(contentBytes, resolved, filePath, emit)
}

func resolveLanguageForASTSplit(reader *bufio.Reader, filePath string) *resolvedLanguage {
	if resolved := resolveLanguage(filePath); resolved != nil {
		return resolved
	}

	sniff, err := reader.Peek(astLanguageDetectionSniffBytes)
	if err != nil && len(sniff) == 0 {
		return nil
	}

	return resolveLanguageForContent(filePath, sniff)
}

func (s *ASTSplitter) splitResolvedContent(contentBytes []byte, resolved *resolvedLanguage, filePath string, emit EmitChunkFunc) error {
	fallback := func() error {
		return s.fallback.Split(bytes.NewReader(contentBytes), filePath, emit)
	}

	content := string(contentBytes)

	// Fast path: entire file fits in one chunk.
	if utf8.RuneCountInString(content) <= s.effectiveChunkSize() {
		lines := splitLines(content)
		if len(lines) == 0 {
			return nil
		}

		return emit(Chunk{Content: content, StartLine: 1, EndLine: len(lines)})
	}

	src := contentBytes

	tree, err := parseTree(src, resolved)
	if err != nil {
		return fallback()
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil { // Defensive: tree-sitter returns a root for every non-nil tree.
		return fallback()
	}

	declTypes := declarationTypes(resolved.ext)

	childCount := root.ChildCount()
	if childCount == 0 && strings.TrimSpace(content) != "" {
		// Defensive: supported grammars should yield at least one top-level child for non-blank input.
		return fallback()
	}

	type nodeInfo struct {
		text      string
		startLine int // 1-indexed
		endLine   int // 1-indexed, inclusive
		isDecl    bool
		isTrivia  bool
	}

	nodes := make([]nodeInfo, 0)

	for i := range childCount {
		child := root.Child(i)
		if child == nil { // Defensive: Child(i) should be non-nil for i < ChildCount().
			continue
		}

		// Defensive: parser-reported coordinates should already be within src/int bounds.
		startByte, ok := treeByteOffset(child.StartByte(), len(src))
		if !ok {
			return fallback()
		}

		endByte, ok := treeByteOffset(child.EndByte(), len(src))
		if !ok {
			return fallback()
		}

		if next := root.Child(i + 1); next != nil {
			nextStartByte, ok := treeByteOffset(next.StartByte(), len(src))
			if !ok {
				return fallback()
			}

			if nextStartByte > endByte {
				endByte = nextStartByte
			}
		}

		if endByte < startByte {
			return fallback()
		}

		text := string(src[startByte:endByte])
		if i == childCount-1 && uint64(len(src)) > uint64(endByte) {
			text = string(src[startByte:])
		}

		startLine, ok := treeLineNumber(child.StartPosition().Row)
		if !ok {
			return fallback()
		}

		endLine, ok := treeLineNumber(child.EndPosition().Row)
		if !ok {
			return fallback()
		}

		isDecl := declTypes[nodeKind(child)]
		isTrivia := child.IsExtra() || strings.TrimSpace(text) == ""
		nodes = append(nodes, nodeInfo{text, startLine, endLine, isDecl, isTrivia})
	}

	if len(nodes) == 0 && strings.TrimSpace(content) != "" {
		return fallback()
	}

	units := make([]astUnit, 0)

	appendUnit := func(start, end int) {
		if start >= end {
			return
		}

		var sb strings.Builder
		for k := start; k < end; k++ {
			sb.WriteString(nodes[k].text)
		}

		text := sb.String()
		units = append(units, astUnit{
			text:      text,
			startLine: nodes[start].startLine,
			endLine:   nodes[end-1].endLine,
			size:      utf8.RuneCountInString(text),
		})
	}

	i := 0
	for i < len(nodes) {
		if nodes[i].isDecl {
			appendUnit(i, i+1)

			i++
		} else {
			// Group consecutive non-declaration nodes, but keep a trailing
			// comment/trivia-only suffix with the declaration it prefixes.
			j := i
			for j < len(nodes) && !nodes[j].isDecl {
				j++
			}

			triviaStart := j
			for triviaStart > i && nodes[triviaStart-1].isTrivia {
				triviaStart--
			}

			if triviaStart < j && j < len(nodes) && nodes[j].isDecl {
				appendUnit(i, triviaStart)

				appendUnit(triviaStart, j+1)

				i = j + 1

				continue
			}

			appendUnit(i, j)

			i = j
		}
	}

	return s.emitPackedUnits(units, filePath, emit)
}

func treeByteOffset(offset uint, maxLen int) (uint, bool) {
	if offset > uint(math.MaxInt) {
		return 0, false
	}

	if int(offset) > maxLen {
		return 0, false
	}

	return offset, true
}

func treeLineNumber(row uint) (int, bool) {
	if uint64(row) > uint64(math.MaxInt-1) {
		return 0, false
	}

	return int(row) + 1, true
}

// effectiveChunkSize returns the configured ChunkSize with a sensible default.
func (s *ASTSplitter) effectiveChunkSize() int {
	if s.ChunkSize <= 0 {
		return 4000
	}

	return s.ChunkSize
}

// splitNode emits one Chunk for text that fits, or delegates to the fallback
// splitter and adjusts returned line numbers to be relative to startLine.
func (s *ASTSplitter) splitNode(text string, startLine, endLine int, filePath string) []Chunk {
	var chunks []Chunk

	err := s.emitNodeChunks(text, startLine, endLine, filePath, func(chunk Chunk) error {
		chunks = append(chunks, chunk)

		return nil
	})
	if err != nil { // Defensive: splitNode only appends chunks from in-memory text, so this needs an artificial seam.
		return nil
	}

	return chunks
}

func (s *ASTSplitter) emitNodeChunks(text string, startLine, endLine int, filePath string, emit EmitChunkFunc) error {
	if utf8.RuneCountInString(text) <= s.effectiveChunkSize() {
		return emit(Chunk{Content: text, StartLine: startLine, EndLine: endLine})
	}

	offset := startLine - 1

	return s.fallback.Split(strings.NewReader(text), filePath, func(chunk Chunk) error {
		chunk.StartLine += offset
		chunk.EndLine += offset

		return emit(chunk)
	})
}

func (s *ASTSplitter) emitPackedUnits(units []astUnit, filePath string, emit EmitChunkFunc) error {
	limit := s.effectiveChunkSize()

	var pack strings.Builder

	packStart := 0
	packEnd := 0
	packSize := 0
	hasPack := false

	flush := func() error {
		if !hasPack {
			return nil
		}

		err := s.emitNodeChunks(pack.String(), packStart, packEnd, filePath, emit)
		pack.Reset()

		packStart = 0
		packEnd = 0
		packSize = 0
		hasPack = false

		return err
	}

	for _, unit := range units {
		if unit.size > limit {
			if err := flush(); err != nil {
				return err
			}

			if err := s.emitNodeChunks(unit.text, unit.startLine, unit.endLine, filePath, emit); err != nil {
				return err
			}

			continue
		}

		if hasPack && packSize+unit.size > limit {
			if err := flush(); err != nil {
				return err
			}
		}

		if !hasPack {
			packStart = unit.startLine
			hasPack = true
		}

		pack.WriteString(unit.text)
		packEnd = unit.endLine
		packSize += unit.size
	}

	return flush()
}

// declarationTypes returns the set of tree-sitter node kinds considered
// top-level declarations for the given file extension.
func declarationTypes(ext string) map[string]bool {
	if bundle := grammarForExt(ext); bundle.Support != grammarSupportUnmapped && len(bundle.DeclarationKinds) > 0 {
		return maps.Clone(bundle.DeclarationKinds)
	}

	switch ext {
	case ".java":
		return map[string]bool{
			"method_declaration":      true,
			"class_declaration":       true,
			"interface_declaration":   true,
			"enum_declaration":        true,
			"constructor_declaration": true,
		}
	case ".c", ".h":
		return map[string]bool{
			"function_definition": true,
			"struct_specifier":    true,
		}
	case ".cpp", ".cc", ".cxx", ".hpp":
		return map[string]bool{
			"function_definition":  true,
			"class_specifier":      true,
			"struct_specifier":     true,
			"namespace_definition": true,
		}
	case ".cs":
		return map[string]bool{
			"method_declaration":    true,
			"class_declaration":     true,
			"interface_declaration": true,
			"struct_declaration":    true,
			"enum_declaration":      true,
			"namespace_declaration": true,
		}
	case ".rb":
		return map[string]bool{
			"method":           true,
			"singleton_method": true,
			"class":            true,
			"module":           true,
		}
	case ".scala", ".sc":
		return map[string]bool{
			"function_definition": true,
			"class_definition":    true,
			"object_definition":   true,
			"trait_definition":    true,
		}
	// Other grammars
	case ".html", ".htm":
		return map[string]bool{
			"element":        true,
			"script_element": true,
			"style_element":  true,
		}
	case ".css":
		return map[string]bool{
			"rule_set":            true,
			"at_rule":             true,
			"media_statement":     true,
			"keyframes_statement": true,
			"import_statement":    true,
		}
	case ".php":
		return map[string]bool{
			"function_definition":   true,
			"class_declaration":     true,
			"method_declaration":    true,
			"interface_declaration": true,
			"trait_declaration":     true,
		}
	case ".hs":
		return map[string]bool{
			"function":                 true,
			"class_decl":               true,
			"instance_decl":            true,
			"data_declaration":         true,
			"newtype_declaration":      true,
			"type_synonym_declaration": true,
			"signature":                true,
		}
	case ".jl":
		return map[string]bool{
			"function_definition": true,
			"macro_definition":    true,
			"struct_definition":   true,
			"module_definition":   true,
			"abstract_definition": true,
		}
	case ".ml", ".mli":
		return map[string]bool{
			"value_definition":  true,
			"type_definition":   true,
			"module_definition": true,
			"class_definition":  true,
			"external":          true,
		}
	case ".ejs", ".erb":
		return map[string]bool{
			"template":    true,
			"content":     true,
			"code":        true,
			"output_code": true,
		}
	case ".toml":
		return map[string]bool{
			"table":       true,
			"array_table": true,
			"pair":        true,
		}
	case ".xml", ".svg":
		return map[string]bool{
			"element":                true,
			"self_closing_element":   true,
			"processing_instruction": true,
		}
	case ".lua":
		return map[string]bool{
			"function_declaration": true,
			"local_function":       true,
			"function_definition":  true,
		}
	case ".zig":
		return map[string]bool{
			"function_declaration":        true,
			"container_declaration":       true,
			"test_declaration":            true,
			"global_variable_declaration": true,
		}
	case ".tf", ".hcl", ".tfvars":
		return map[string]bool{
			"block":     true,
			"attribute": true,
		}
	// Forest
	case ".vue":
		return map[string]bool{
			"element":          true,
			"script_element":   true,
			"style_element":    true,
			"template_element": true,
		}
	case ".nix":
		return map[string]bool{
			"function_expression": true,
			"binding":             true,
			"attrset_expression":  true,
			"let_expression":      true,
			"with_expression":     true,
		}
	case ".groovy", ".gradle":
		return map[string]bool{
			"method_declaration":      true,
			"class_declaration":       true,
			"closure":                 true,
			"constructor_declaration": true,
			"interface_declaration":   true,
		}
	case ".clj", ".cljs", ".cljc":
		return map[string]bool{
			"list_lit":    true,
			"map_lit":     true,
			"anon_fn_lit": true,
		}
	case ".erl", ".hrl":
		return map[string]bool{
			"function_clause":  true,
			"attribute":        true,
			"export_attribute": true,
		}
	case ".graphql", ".gql":
		return map[string]bool{
			"definition":                   true,
			"object_type_definition":       true,
			"interface_type_definition":    true,
			"field_definition":             true,
			"enum_type_definition":         true,
			"input_object_type_definition": true,
			"directive_definition":         true,
		}
	case ".angular":
		return map[string]bool{
			"element":              true,
			"text_interpolation":   true,
			"structural_directive": true,
		}
	case ".j2", ".jinja", ".jinja2":
		return map[string]bool{
			"statement":   true,
			"expression":  true,
			"comment":     true,
			"block_start": true,
		}
	}

	return map[string]bool{}
}
