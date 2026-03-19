package splitter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func assertContainsSymbol(t *testing.T, symbols []SymbolContext, want SymbolContext) {
	t.Helper()

	assert.Contains(t, symbols, want)
}

func firstNodeByKind(node *sitter.Node, kind string) *sitter.Node {
	if node == nil {
		return nil
	}

	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}

		if nodeKind(child) == kind {
			return child
		}

		if nested := firstNodeByKind(child, kind); nested != nil {
			return nested
		}
	}

	return nil
}

func TestSymbolExtractionPolicy_DefaultUsesTreeSitterFallbacks(t *testing.T) {
	assert.Equal(t, []symbolExtractionStep{
		symbolExtractionAST,
	}, symbolExtractionPolicy(".ts"))
}

func TestSymbolExtractionPolicy_GoAddsStdlibLastResort(t *testing.T) {
	assert.Equal(t, []symbolExtractionStep{
		symbolExtractionAST,
		symbolExtractionGoStdlibParser,
	}, symbolExtractionPolicy(".go"))
}

func TestExtractSymbolContexts_MinimalGoFile(t *testing.T) {
	symbols, err := ExtractSymbolContexts([]byte("package main\n\nfunc helper() int {\n\treturn 1\n}\n"), "main.go")
	require.NoError(t, err)
	require.NotEmpty(t, symbols)
	assert.Equal(t, SymbolContext{
		Name:      "helper",
		Kind:      "function",
		StartLine: 3,
		EndLine:   5,
	}, symbols[0])
}

func TestExtractSymbolContexts_GoRepoFileProvidesSymbols(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "handler", "search_enrichment.go"))
	require.NoError(t, err)

	symbols, err := ExtractSymbolContexts(source, "search_enrichment.go")
	require.NoError(t, err)
	require.NotEmpty(t, symbols)

	names := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		names = append(names, symbol.Name)
	}

	assert.Contains(t, names, "newSearchResultEnricher")
	assert.Contains(t, names, "Enrich")
	assert.Contains(t, names, "loadFileSymbols")
}

func TestExtractSymbolContexts_TypeScriptVariableProvidesFallbackSymbol(t *testing.T) {
	source := []byte("export const answer = 42\n")

	symbols, err := ExtractSymbolContexts(source, "example.ts")
	require.NoError(t, err)
	require.Len(t, symbols, 1)
	assertContainsSymbol(t, symbols, SymbolContext{
		Name:      "answer",
		Kind:      "variable",
		StartLine: 1,
		EndLine:   1,
	})
}

func TestExtractSymbolContexts_JavaScriptVariableProvidesFallbackSymbol(t *testing.T) {
	source := []byte("export const answer = 42\n")

	symbols, err := ExtractSymbolContexts(source, "example.js")
	require.NoError(t, err)
	require.Len(t, symbols, 1)
	assertContainsSymbol(t, symbols, SymbolContext{
		Name:      "answer",
		Kind:      "variable",
		StartLine: 1,
		EndLine:   1,
	})
}

func TestExtractSymbolContexts_ShellNarrowsToFunctionDefinitions(t *testing.T) {
	source := []byte("echo hi\nif true; then\n  echo nope\nfi\n\nbuild() {\n  echo ok\n}\n")

	symbols, err := ExtractSymbolContexts(source, "build.sh")
	require.NoError(t, err)
	require.Len(t, symbols, 1)
	assert.Equal(t, SymbolContext{
		Name:      "build",
		Kind:      "function",
		StartLine: 6,
		EndLine:   8,
	}, symbols[0])
}

func TestExtractSymbolContexts_TypeScriptSymbolExtras(t *testing.T) {
	source := []byte("abstract class Greeter {\n  abstract speak(): string\n}\n\ninterface Speaker {\n  say(): void\n}\n")

	symbols, err := ExtractSymbolContexts(source, "types.ts")
	require.NoError(t, err)
	require.NotEmpty(t, symbols)

	for _, want := range []SymbolContext{
		{Name: "Greeter", Kind: "class", StartLine: 1, EndLine: 3},
		{Name: "speak", Kind: "method", StartLine: 2, EndLine: 2},
		{Name: "Speaker", Kind: "interface", StartLine: 5, EndLine: 7},
		{Name: "say", Kind: "method", StartLine: 6, EndLine: 6},
	} {
		assertContainsSymbol(t, symbols, want)
	}
}

func TestExtractSymbolContexts_MigrationRepresentativeLanguages(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		source   []byte
		want     []SymbolContext
	}{
		{
			name:     "Go methods and functions",
			filePath: "main.go",
			source:   []byte("package main\n\ntype Greeter struct{}\nfunc (Greeter) Speak() string { return \"hi\" }\nfunc helper() string { return \"ok\" }\n"),
			want: []SymbolContext{
				{Name: "Greeter", Kind: "type", StartLine: 3, EndLine: 3},
				{Name: "Speak", Kind: "method", StartLine: 4, EndLine: 4},
				{Name: "helper", Kind: "function", StartLine: 5, EndLine: 5},
			},
		},
		{
			name:     "Rust functions",
			filePath: "math.rs",
			source:   []byte("struct Greeter;\n\nfn greet() -> &'static str {\n    \"hi\"\n}\n\nfn parting() -> &'static str {\n    \"bye\"\n}\n"),
			want: []SymbolContext{
				{Name: "greet", Kind: "function", StartLine: 3, EndLine: 5},
				{Name: "parting", Kind: "function", StartLine: 7, EndLine: 9},
			},
		},
		{
			name:     "React TSX exports",
			filePath: "component.tsx",
			source:   []byte("import React from \"react\";\n\ntype Props = { title: string }\n\nexport function Title(props: Props) { return <h1>{props.title}</h1> }\n\nexport const Footer = () => <footer>Bye</footer>;\n"),
			want: []SymbolContext{
				{Name: "Title", Kind: "function", StartLine: 5, EndLine: 5},
				{Name: "Footer", Kind: "variable", StartLine: 7, EndLine: 7},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			symbols, err := ExtractSymbolContexts(tt.source, tt.filePath)
			require.NoError(t, err)
			require.NotEmpty(t, symbols)

			for _, want := range tt.want {
				assertContainsSymbol(t, symbols, want)
			}
		})
	}
}

func TestExtractSymbolContexts_RustContainerWrappers(t *testing.T) {
	source := []byte("struct Greeter;\n\nimpl Greeter {\n    fn speak(&self) -> &'static str {\n        \"hi\"\n    }\n}\n\nmod nested {\n    pub fn helper() -> &'static str {\n        \"ok\"\n    }\n}\n\ntrait Speaker {\n    fn say(&self);\n}\n")

	symbols, err := ExtractSymbolContexts(source, "lib.rs")
	require.NoError(t, err)
	require.NotEmpty(t, symbols)

	for _, want := range []SymbolContext{
		{Name: "Greeter", Kind: "struct", StartLine: 1, EndLine: 1},
		{Name: "speak", Kind: "method", StartLine: 4, EndLine: 6},
		{Name: "nested", Kind: "module", StartLine: 9, EndLine: 13},
		{Name: "helper", Kind: "function", StartLine: 10, EndLine: 12},
		{Name: "Speaker", Kind: "trait", StartLine: 15, EndLine: 17},
		{Name: "say", Kind: "method", StartLine: 16, EndLine: 16},
	} {
		assertContainsSymbol(t, symbols, want)
	}
}

func TestExtractSymbolContexts_ReactTSXExportedDeclarations(t *testing.T) {
	source := []byte("import React, { memo } from \"react\";\n\nexport default function Title() {\n  return <h1>Hello</h1>;\n}\n\nexport const Footer = memo(function Footer() {\n  return <footer>Bye</footer>;\n});\n")

	symbols, err := ExtractSymbolContexts(source, "component.tsx")
	require.NoError(t, err)
	require.NotEmpty(t, symbols)

	for _, want := range []SymbolContext{
		{Name: "Title", Kind: "function", StartLine: 3, EndLine: 5},
		{Name: "Footer", Kind: "variable", StartLine: 7, EndLine: 9},
	} {
		assertContainsSymbol(t, symbols, want)
	}
}

func TestFindEnclosingSymbol_PrefersSmallestContainingSymbol(t *testing.T) {
	symbols := []SymbolContext{
		{Name: "Greeter", Kind: "type", StartLine: 3, EndLine: 7},
		{Name: "Speak", Kind: "method", StartLine: 4, EndLine: 6},
	}

	symbol := FindEnclosingSymbol(symbols, 5, 5)
	if assert.NotNil(t, symbol) {
		assert.Equal(t, "Speak", symbol.Name)
	}
}

func TestFindEnclosingSymbol_BroadRangeUsesMidpointFallback(t *testing.T) {
	symbols := []SymbolContext{
		{Name: "newSearchResultEnricher", Kind: "function", StartLine: 30, EndLine: 36},
		{Name: "Enrich", Kind: "method", StartLine: 48, EndLine: 64},
		{Name: "loadFileSymbols", Kind: "method", StartLine: 66, EndLine: 115},
	}

	symbol := FindEnclosingSymbol(symbols, 1, 117)
	if assert.NotNil(t, symbol) {
		assert.Equal(t, "Enrich", symbol.Name)
	}
}

func TestFindEnclosingSymbol_UsesExtractedRustNesting(t *testing.T) {
	source := []byte("struct Greeter;\n\nimpl Greeter {\n    fn speak(&self) -> &'static str {\n        \"hi\"\n    }\n}\n\nmod nested {\n    pub fn helper() -> &'static str {\n        \"ok\"\n    }\n}\n\ntrait Speaker {\n    fn say(&self);\n}\n")

	symbols, err := ExtractSymbolContexts(source, "lib.rs")
	require.NoError(t, err)

	t.Run("prefers nested method", func(t *testing.T) {
		symbol := FindEnclosingSymbol(symbols, 5, 5)
		if assert.NotNil(t, symbol) {
			assert.Equal(t, "speak", symbol.Name)
		}
	})

	t.Run("falls back to enclosing module", func(t *testing.T) {
		symbol := FindEnclosingSymbol(symbols, 9, 9)
		if assert.NotNil(t, symbol) {
			assert.Equal(t, "nested", symbol.Name)
		}
	})

	t.Run("matches trait method signature", func(t *testing.T) {
		symbol := FindEnclosingSymbol(symbols, 16, 16)
		if assert.NotNil(t, symbol) {
			assert.Equal(t, "say", symbol.Name)
		}
	})
}

func TestSymbolExtractionState_LoadTreeCachesResult(t *testing.T) {
	resolved := resolveLanguage("main.go")
	require.NotNil(t, resolved)

	state := &symbolExtractionState{
		source:   []byte("package main\nfunc helper() {}\n"),
		filePath: "main.go",
		resolved: resolved,
	}
	defer state.close()

	tree1, root1, err1 := state.loadTree()
	tree2, root2, err2 := state.loadTree()

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Same(t, tree1, tree2)
	assert.Same(t, root1, root2)
}

func TestSymbolExtractionState_ExtractBranches(t *testing.T) {
	t.Run("ast returns parse error", func(t *testing.T) {
		state := &symbolExtractionState{source: []byte("x"), filePath: "README.md", resolved: resolveLanguage("README.md")}
		defer state.close()

		symbols, err := state.extract(symbolExtractionAST)
		assert.Nil(t, symbols)
		require.Error(t, err)
	})

	t.Run("ast returns nil when no symbols", func(t *testing.T) {
		resolved := resolveLanguage("main.go")
		require.NotNil(t, resolved)

		state := &symbolExtractionState{source: []byte("package main\n"), filePath: "main.go", resolved: resolved}
		defer state.close()

		symbols, err := state.extract(symbolExtractionAST)
		require.NoError(t, err)
		assert.Nil(t, symbols)
	})

	t.Run("unknown extraction step", func(t *testing.T) {
		state := &symbolExtractionState{}
		symbols, err := state.extract(symbolExtractionStep("mystery"))
		assert.Nil(t, symbols)
		require.ErrorIs(t, err, errUnknownSymbolExtractionStep)
	})
}

func TestExtractSymbolContexts_UnsupportedAndErrorOnlyPolicies(t *testing.T) {
	symbols, err := ExtractSymbolContexts([]byte("plain text"), "README.md")
	require.NoError(t, err)
	assert.Nil(t, symbols)

	original, existed := symbolExtractionPolicyByExt[".go"]
	symbolExtractionPolicyByExt[".go"] = []symbolExtractionStep{symbolExtractionStep("unknown")}

	t.Cleanup(func() {
		if existed {
			symbolExtractionPolicyByExt[".go"] = original
		} else {
			delete(symbolExtractionPolicyByExt, ".go")
		}
	})

	symbols, err = ExtractSymbolContexts([]byte("package main\nfunc helper() {}\n"), "main.go")
	require.NoError(t, err)
	assert.Nil(t, symbols)
}

func TestSymbolHelperBranches(t *testing.T) {
	assert.False(t, supportsSymbolExtraction(nil))
	assert.Nil(t, symbolDeclarationKinds(nil))
	assert.Nil(t, extractASTSymbolContexts(nil, nil, nil))
	assert.Nil(t, extractASTDeclarationSymbolContexts(nil, nil, nil, nil))
	assert.Nil(t, declarationWrapperChild(nil))
	assert.Nil(t, declarationChildren(nil, nil))
	assert.Nil(t, allNamedChildren(nil))
	assert.Nil(t, namedChildrenMatchingKinds(nil, nil))
	assert.False(t, hasMatchingSymbol([]SymbolContext{{Name: "one", Kind: "function", StartLine: 1, EndLine: 1}}, SymbolContext{Name: "two", Kind: "function", StartLine: 3, EndLine: 3}))
	assert.Nil(t, FindEnclosingSymbol(nil, 1, 1))
	assert.Equal(t, "definition", symbolKindForNode(nil))
	assert.False(t, isRustMethodNode(nil))
	assert.Empty(t, symbolNameForNode(nil, nil))

	assert.Equal(t, []SymbolContext{{Name: "dup", Kind: "function", StartLine: 1, EndLine: 1}}, dedupeSymbolContexts([]SymbolContext{{Name: "dup", Kind: "function", StartLine: 1, EndLine: 1}, {Name: "dup", Kind: "function", StartLine: 1, EndLine: 1}}))
	assert.Equal(t, []SymbolContext{{Name: "one", Kind: "function", StartLine: 1, EndLine: 1}, {Name: "two", Kind: "function", StartLine: 3, EndLine: 3}}, mergeSymbolContexts([]SymbolContext{{Name: "one", Kind: "function", StartLine: 1, EndLine: 1}}, []SymbolContext{{Name: "two", Kind: "function", StartLine: 3, EndLine: 3}}))
	assert.Nil(t, FindEnclosingSymbol([]SymbolContext{{Name: "one", Kind: "function", StartLine: 1, EndLine: 2}}, 5, 5))
}

func TestExtractGoSymbolContexts_ErrorAndEmptyCases(t *testing.T) {
	symbols, err := extractGoSymbolContexts(nil, "main.go")
	require.NoError(t, err)
	assert.Nil(t, symbols)

	symbols, err = extractGoSymbolContexts([]byte("package main\nfunc ("), "main.go")
	assert.Nil(t, symbols)
	require.Error(t, err)
}

func TestSymbolNameAndKindHelpers(t *testing.T) {
	assert.Equal(t, "name", symbolNameText("identifier", " name "))
	assert.Equal(t, "value", symbolNameText("literal", "value"))
	assert.Empty(t, symbolNameText("literal", "line1\nline2"))
	assert.Empty(t, symbolNameText("literal", "   "))

	assert.True(t, isIdentifierNodeType("identifier"))
	assert.True(t, isIdentifierNodeType("custom_identifier"))
	assert.False(t, isIdentifierNodeType("literal"))

	for _, tt := range []struct {
		nodeType string
		want     string
	}{
		{nodeType: "enum_declaration", want: "enum"},
		{nodeType: "trait_item", want: "trait"},
		{nodeType: "struct_specifier", want: "struct"},
		{nodeType: "namespace_definition", want: "namespace"},
		{nodeType: "module_definition", want: "module"},
		{nodeType: "variable_assignment", want: "variable"},
		{nodeType: "rule_set", want: "rule"},
		{nodeType: "array_table", want: "table"},
		{nodeType: "style_element", want: "element"},
		{nodeType: "unknown_definition", want: "unknown"},
		{nodeType: "   ", want: "definition"},
	} {
		t.Run(tt.nodeType, func(t *testing.T) {
			assert.Equal(t, tt.want, astSymbolKind(tt.nodeType))
		})
	}
}

func TestTreeSitterSymbolHelpers(t *testing.T) {
	resolvedTS := resolveLanguage("component.tsx")
	require.NotNil(t, resolvedTS)
	treeTS, err := parseTree([]byte("export const answer = 42\nclass Greeter { speak() {} }\n"), resolvedTS)
	require.NoError(t, err)
	t.Cleanup(func() { treeTS.Close() })

	exportNode := firstNodeByKind(treeTS.RootNode(), "export_statement")
	require.NotNil(t, exportNode)
	assert.Equal(t, "const answer = 42", symbolNameForNode(exportNode, []byte("export const answer = 42\nclass Greeter { speak() {} }\n")))
	assert.NotNil(t, declarationWrapperChild(exportNode))
	_, ok := symbolContextForNode(exportNode, exportNode, []byte("export const answer = 42\nclass Greeter { speak() {} }\n"))
	assert.False(t, ok)

	classNode := firstNodeByKind(treeTS.RootNode(), "class_declaration")
	require.NotNil(t, classNode)
	children := declarationChildren(classNode, resolvedTS)
	require.NotEmpty(t, children)
	assert.NotEmpty(t, namedChildrenMatchingKinds(classNode.ChildByFieldName("body"), symbolDeclarationKinds(resolvedTS)))

	treeTSInterface, err := parseTree([]byte("interface Speaker {\n  name: string\n  speak(): void\n}\n"), resolveLanguage("types.ts"))
	require.NoError(t, err)
	t.Cleanup(func() { treeTSInterface.Close() })

	interfaceNode := firstNodeByKind(treeTSInterface.RootNode(), "interface_declaration")
	require.NotNil(t, interfaceNode)
	interfaceBody := interfaceNode.ChildByFieldName("body")
	require.NotNil(t, interfaceBody)
	assert.Len(t, namedChildrenMatchingKinds(interfaceBody, map[string]bool{"method_signature": true}), 1)

	treeTSBroken, err := parseTree([]byte("class Greeter"), resolveLanguage("types.ts"))
	require.NoError(t, err)
	t.Cleanup(func() { treeTSBroken.Close() })

	brokenClass := firstNodeByKind(treeTSBroken.RootNode(), "class_declaration")
	if brokenClass != nil {
		assert.Nil(t, declarationChildren(brokenClass, resolveLanguage("types.ts")))
	}

	rootSymbols := extractASTSymbolContexts(treeTS.RootNode(), []byte("export const answer = 42\nclass Greeter { speak() {} }\n"), &resolvedLanguage{ext: ".dockerfile", grammar: grammarBundle{Support: grammarSupportSupportedNow}})
	assert.Nil(t, rootSymbols)

	resolvedGo := resolveLanguage("main.go")
	require.NotNil(t, resolvedGo)
	treeGo, err := parseTree([]byte("package main\nimport \"fmt\"\n"), resolvedGo)
	require.NoError(t, err)
	t.Cleanup(func() { treeGo.Close() })
	assert.Equal(t, "package main", symbolNameForNode(treeGo.RootNode(), []byte("package main\nimport \"fmt\"\n")))

	treeGoBlock, err := parseTree([]byte("package main\nfunc helper() {\n\tif true {\n\t}\n}\n"), resolvedGo)
	require.NoError(t, err)
	t.Cleanup(func() { treeGoBlock.Close() })

	blockNode := firstNodeByKind(treeGoBlock.RootNode(), "block")
	require.NotNil(t, blockNode)
	assert.Empty(t, symbolNameForNode(blockNode, []byte("package main\nfunc helper() {\n\tif true {\n\t}\n}\n")))
	assert.Empty(t, extractASTSymbolContexts(blockNode, []byte("package main\nfunc helper() {\n\tif true {\n\t}\n}\n"), &resolvedLanguage{ext: ".ts", grammar: grammarBundle{Support: grammarSupportSupportedNow, SymbolDeclarationKinds: map[string]bool{"if_statement": true}}}))

	_, ok = symbolContextForNode(blockNode, blockNode, []byte("package main\nfunc helper() {\n\tif true {\n\t}\n}\n"))
	assert.False(t, ok)

	resolvedRust := resolveLanguage("lib.rs")
	require.NotNil(t, resolvedRust)
	treeRust, err := parseTree([]byte("trait Speaker {\n    fn say(&self);\n}\n\nfn helper() {}\n"), resolvedRust)
	require.NoError(t, err)
	t.Cleanup(func() { treeRust.Close() })

	methodSig := firstNodeByKind(treeRust.RootNode(), "function_signature_item")
	require.NotNil(t, methodSig)
	assert.True(t, isRustMethodNode(methodSig))

	topLevelFn := firstNodeByKind(treeRust.RootNode(), "function_item")
	require.NotNil(t, topLevelFn)
	assert.False(t, isRustMethodNode(topLevelFn))

	treeRustSig, err := parseTree([]byte("fn declare(&self);\n"), resolvedRust)
	require.NoError(t, err)
	t.Cleanup(func() { treeRustSig.Close() })

	standaloneSig := firstNodeByKind(treeRustSig.RootNode(), "function_signature_item")
	require.NotNil(t, standaloneSig)
	assert.Equal(t, "function", symbolKindForNode(standaloneSig))
}
