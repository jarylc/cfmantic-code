package splitter

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// SymbolContext describes a definition symbol and its enclosing line range.
type SymbolContext struct {
	Name      string
	Kind      string
	StartLine int
	EndLine   int
}

type symbolExtractionStep string

const (
	symbolExtractionAST            symbolExtractionStep = "tree-sitter AST"
	symbolExtractionGoStdlibParser symbolExtractionStep = "Go stdlib parser last resort"
)

var defaultSymbolExtractionPolicy = []symbolExtractionStep{
	symbolExtractionAST,
}

var symbolExtractionPolicyByExt = map[string][]symbolExtractionStep{
	".go": {
		symbolExtractionAST,
		symbolExtractionGoStdlibParser,
	},
}

var errUnknownSymbolExtractionStep = errors.New("unknown symbol extraction step")

type symbolExtractionState struct {
	source   []byte
	filePath string
	resolved *resolvedLanguage

	tree       *sitter.Tree
	root       *sitter.Node
	treeLoaded bool
	treeErr    error
}

func symbolExtractionPolicy(ext string) []symbolExtractionStep {
	if policy, ok := symbolExtractionPolicyByExt[strings.ToLower(ext)]; ok {
		return slices.Clone(policy)
	}

	return slices.Clone(defaultSymbolExtractionPolicy)
}

func (s *symbolExtractionState) close() {
	if s.tree != nil {
		s.tree.Close()
	}
}

func (s *symbolExtractionState) loadTree() (*sitter.Tree, *sitter.Node, error) {
	if s.treeLoaded {
		return s.tree, s.root, s.treeErr
	}

	s.treeLoaded = true

	s.tree, s.treeErr = parseTree(s.source, s.resolved)
	if s.tree != nil {
		s.root = s.tree.RootNode()
	}

	return s.tree, s.root, s.treeErr
}

func (s *symbolExtractionState) extract(step symbolExtractionStep) ([]SymbolContext, error) {
	switch step {
	case symbolExtractionAST:
		_, root, err := s.loadTree()
		if err != nil || root == nil {
			return nil, err
		}

		symbols := extractASTSymbolContexts(root, s.source, s.resolved)
		if len(symbols) == 0 {
			return nil, nil
		}

		return symbols, nil
	case symbolExtractionGoStdlibParser:
		return extractGoSymbolContexts(s.source, s.filePath)
	default:
		return nil, fmt.Errorf("%w: %q", errUnknownSymbolExtractionStep, step)
	}
}

// ExtractSymbolContexts returns definition symbols for a parseable source file.
func ExtractSymbolContexts(source []byte, filePath string) ([]SymbolContext, error) {
	resolved := resolveLanguageForContent(filePath, source)
	if !supportsSymbolExtraction(resolved) {
		return nil, nil
	}

	state := &symbolExtractionState{
		source:   source,
		filePath: filePath,
		resolved: resolved,
	}
	defer state.close()

	var collected []SymbolContext

	for _, step := range symbolExtractionPolicy(resolved.ext) {
		symbols, err := state.extract(step)
		if err != nil {
			continue
		}

		if len(symbols) > 0 {
			collected = mergeSymbolContexts(collected, symbols)
		}
	}

	if len(collected) == 0 {
		return nil, nil
	}

	return collected, nil
}

func supportsSymbolExtraction(resolved *resolvedLanguage) bool {
	if resolved == nil || resolved.grammar.Support != grammarSupportSupportedNow {
		return false
	}

	return len(symbolDeclarationKinds(resolved)) > 0 || resolved.ext == ".go"
}

func symbolDeclarationKinds(resolved *resolvedLanguage) map[string]bool {
	if resolved == nil {
		return nil
	}

	return resolved.grammar.SymbolDeclarationKinds
}

func extractASTSymbolContexts(root *sitter.Node, source []byte, resolved *resolvedLanguage) []SymbolContext {
	if root == nil || resolved == nil {
		return nil
	}

	declKinds := symbolDeclarationKinds(resolved)
	if len(declKinds) == 0 {
		return nil
	}

	symbols := make([]SymbolContext, 0, root.NamedChildCount())
	for i := range root.ChildCount() {
		child := root.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}

		if !declKinds[nodeKind(child)] {
			continue
		}

		symbols = append(symbols, extractASTDeclarationSymbolContexts(child, child, source, resolved)...)
	}

	return dedupeSymbolContexts(symbols)
}

func extractASTDeclarationSymbolContexts(node, spanNode *sitter.Node, source []byte, resolved *resolvedLanguage) []SymbolContext {
	if node == nil || spanNode == nil || resolved == nil || !node.IsNamed() {
		return nil
	}

	if child := declarationWrapperChild(node); child != nil {
		return extractASTDeclarationSymbolContexts(child, spanNode, source, resolved)
	}

	symbols := make([]SymbolContext, 0, 1)
	if symbol, ok := symbolContextForNode(node, spanNode, source); ok {
		symbols = append(symbols, symbol)
	}

	for _, child := range declarationChildren(node, resolved) {
		symbols = append(symbols, extractASTDeclarationSymbolContexts(child, child, source, resolved)...)
	}

	return dedupeSymbolContexts(symbols)
}

func declarationWrapperChild(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}

	switch nodeKind(node) {
	case "decorated_definition", "export_statement":
		for _, field := range []string{"definition", "declaration"} {
			if child := node.ChildByFieldName(field); child != nil && child.IsNamed() {
				return child
			}
		}
	}

	return nil
}

func declarationChildren(node *sitter.Node, resolved *resolvedLanguage) []*sitter.Node {
	if node == nil || resolved == nil {
		return nil
	}

	switch nodeKind(node) {
	case "declaration_list", "lexical_declaration", "type_declaration":
		return allNamedChildren(node)
	case "abstract_class_declaration", "class_declaration", "class_definition", "impl_item", "interface_declaration", "internal_module", "mod_item", "trait_item":
		body := node.ChildByFieldName("body")
		if body == nil || !body.IsNamed() {
			return nil
		}

		return namedChildrenMatchingKinds(body, symbolDeclarationKinds(resolved))
	default:
		return nil
	}
}

func allNamedChildren(node *sitter.Node) []*sitter.Node {
	if node == nil {
		return nil
	}

	children := make([]*sitter.Node, 0, node.NamedChildCount())
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}

		children = append(children, child)
	}

	return children
}

func namedChildrenMatchingKinds(node *sitter.Node, kinds map[string]bool) []*sitter.Node {
	if node == nil || len(kinds) == 0 {
		return nil
	}

	children := make([]*sitter.Node, 0, node.NamedChildCount())
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}

		if !kinds[nodeKind(child)] {
			continue
		}

		children = append(children, child)
	}

	return children
}

func symbolContextForNode(node, spanNode *sitter.Node, source []byte) (SymbolContext, bool) {
	if node == nil || spanNode == nil || symbolWrapperOnlyNodeKind(nodeKind(node)) {
		return SymbolContext{}, false
	}

	name := symbolNameForNode(node, source)
	if name == "" {
		return SymbolContext{}, false
	}

	// Defensive: parser rows should already fit in int for real parse trees.
	startLine, ok := treeLineNumber(spanNode.StartPosition().Row)
	if !ok {
		return SymbolContext{}, false
	}

	endLine, ok := treeLineNumber(spanNode.EndPosition().Row)
	if !ok {
		return SymbolContext{}, false
	}

	endLine = max(endLine, startLine)

	return SymbolContext{
		Name:      name,
		Kind:      symbolKindForNode(node),
		StartLine: startLine,
		EndLine:   endLine,
	}, true
}

func symbolWrapperOnlyNodeKind(nodeType string) bool {
	switch nodeType {
	case "declaration_list", "decorated_definition", "export_statement", "impl_item", "lexical_declaration", "type_declaration":
		return true
	default:
		return false
	}
}

func dedupeSymbolContexts(symbols []SymbolContext) []SymbolContext {
	if len(symbols) < 2 {
		return symbols
	}

	unique := make([]SymbolContext, 0, len(symbols))

	seen := make(map[SymbolContext]struct{}, len(symbols))
	for _, symbol := range symbols {
		if _, ok := seen[symbol]; ok {
			continue
		}

		seen[symbol] = struct{}{}
		unique = append(unique, symbol)
	}

	return unique
}

func mergeSymbolContexts(existing, incoming []SymbolContext) []SymbolContext {
	if len(existing) == 0 {
		return dedupeSymbolContexts(slices.Clone(incoming))
	}

	merged := slices.Clone(existing)
	for _, symbol := range incoming {
		if hasMatchingSymbol(merged, symbol) {
			continue
		}

		merged = append(merged, symbol)
	}

	return dedupeSymbolContexts(merged)
}

func hasMatchingSymbol(symbols []SymbolContext, want SymbolContext) bool {
	for _, symbol := range symbols {
		if symbol.Name != want.Name || symbol.Kind != want.Kind {
			continue
		}

		if symbol.StartLine <= want.EndLine && want.StartLine <= symbol.EndLine {
			return true
		}
	}

	return false
}

func extractGoSymbolContexts(source []byte, filePath string) ([]SymbolContext, error) {
	if len(source) == 0 {
		return nil, nil
	}

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, filePath, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse go file: %w", err)
	}

	symbols := make([]SymbolContext, 0, len(file.Decls))
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			kind := "function"
			if decl.Recv != nil {
				kind = "method"
			}

			startLine := fset.PositionFor(decl.Pos(), false).Line
			endLine := max(fset.PositionFor(decl.End(), false).Line, startLine)

			symbols = append(symbols, SymbolContext{
				Name:      decl.Name.Name,
				Kind:      kind,
				StartLine: startLine,
				EndLine:   endLine,
			})
		case *ast.GenDecl:
			if decl.Tok != token.TYPE {
				continue
			}

			for _, spec := range decl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok { // Defensive: go/parser emits only *ast.TypeSpec entries inside a token.TYPE declaration.
					continue
				}

				startLine := fset.PositionFor(typeSpec.Pos(), false).Line
				endLine := max(fset.PositionFor(typeSpec.End(), false).Line, startLine)

				symbols = append(symbols, SymbolContext{
					Name:      typeSpec.Name.Name,
					Kind:      "type",
					StartLine: startLine,
					EndLine:   endLine,
				})
			}
		}
	}

	return symbols, nil
}

// FindEnclosingSymbol returns the smallest symbol that best matches a line range.
func FindEnclosingSymbol(symbols []SymbolContext, startLine, endLine int) *SymbolContext {
	if len(symbols) == 0 {
		return nil
	}

	startLine = max(startLine, 1)
	endLine = max(endLine, startLine)

	if idx := bestSymbolIndex(symbols, func(symbol SymbolContext) bool {
		return symbol.StartLine <= startLine && symbol.EndLine >= endLine
	}); idx >= 0 {
		return &symbols[idx]
	}

	midLine := startLine + (endLine-startLine)/2

	if idx := bestSymbolIndex(symbols, func(symbol SymbolContext) bool {
		return symbol.StartLine <= midLine && symbol.EndLine >= midLine
	}); idx >= 0 {
		return &symbols[idx]
	}

	return nil
}

func bestSymbolIndex(symbols []SymbolContext, match func(SymbolContext) bool) int {
	best := -1
	bestSpan := 0

	for i, symbol := range symbols {
		if !match(symbol) {
			continue
		}

		span := symbol.EndLine - symbol.StartLine
		if best == -1 || span < bestSpan || (span == bestSpan && symbol.StartLine >= symbols[best].StartLine) {
			best = i
			bestSpan = span
		}
	}

	return best
}

func symbolKindForNode(node *sitter.Node) string {
	if node == nil {
		return "definition"
	}

	if isRustMethodNode(node) {
		return "method"
	}

	nodeType := nodeKind(node)
	if nodeType == "function_signature_item" {
		return "function"
	}

	return astSymbolKind(nodeType)
}

func isRustMethodNode(node *sitter.Node) bool {
	if node == nil {
		return false
	}

	switch nodeKind(node) {
	case "function_item", "function_signature_item":
	default:
		return false
	}

	parent := node.Parent()
	if parent == nil || nodeKind(parent) != "declaration_list" {
		return false
	}

	switch nodeKind(parent.Parent()) {
	case "impl_item", "trait_item":
		return true
	default:
		return false
	}
}

func astSymbolKind(nodeType string) string {
	switch {
	case nodeTypeHasWord(nodeType, "method"):
		return "method"
	case nodeTypeHasWord(nodeType, "function"):
		return "function"
	case nodeTypeHasWord(nodeType, "class"):
		return "class"
	case nodeTypeHasWord(nodeType, "interface"):
		return "interface"
	case nodeTypeHasWord(nodeType, "enum"):
		return "enum"
	case nodeTypeHasWord(nodeType, "trait"):
		return "trait"
	case nodeTypeHasWord(nodeType, "struct"):
		return "struct"
	case nodeTypeHasWord(nodeType, "type"):
		return "type"
	case nodeTypeHasWord(nodeType, "namespace"):
		return "namespace"
	case nodeTypeHasWord(nodeType, "module") || strings.HasPrefix(nodeType, "mod_"):
		return "module"
	case nodeTypeHasWord(nodeType, "variable") || nodeTypeHasWord(nodeType, "lexical") || nodeTypeHasWord(nodeType, "binding") || nodeType == "pair" || nodeType == "attribute":
		return "variable"
	case nodeTypeHasWord(nodeType, "rule"):
		return "rule"
	case nodeTypeHasWord(nodeType, "table"):
		return "table"
	case nodeTypeHasWord(nodeType, "element"):
		return "element"
	}

	kind := nodeType
	for _, suffix := range []string{"_declaration", "_definition", "_statement", "_instruction", "_item", "_spec", "_expression", "_clause"} {
		kind = strings.TrimSuffix(kind, suffix)
	}

	kind = strings.ReplaceAll(kind, "_", " ")

	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "definition"
	}

	return kind
}

func nodeTypeHasWord(nodeType, word string) bool {
	parts := strings.FieldsFunc(nodeType, func(r rune) bool {
		return r == '_' || r == '-'
	})

	return slices.Contains(parts, word)
}

func symbolNameForNode(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}

	for _, field := range []string{"name", "key"} {
		if nameNode := node.ChildByFieldName(field); nameNode != nil {
			if name := strings.TrimSpace(nameNode.Utf8Text(source)); name != "" {
				return name
			}
		}
	}

	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil || !child.IsNamed() {
			continue
		}

		if name := symbolNameText(nodeKind(child), child.Utf8Text(source)); name != "" {
			return name
		}
	}

	return ""
}

func symbolNameText(nodeType, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	if isIdentifierNodeType(nodeType) {
		return text
	}

	if !strings.ContainsRune(text, '\n') {
		return text
	}

	return ""
}

func isIdentifierNodeType(nodeType string) bool {
	switch nodeType {
	case "field_identifier", "identifier", "interpreted_string_literal", "package_identifier", "private_property_identifier", "property_identifier", "sh_variable_name", "string", "type_identifier", "word":
		return true
	}

	return strings.HasSuffix(nodeType, "identifier")
}
