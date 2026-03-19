package splitter

import (
	"bufio"
	"errors"
	"io"
	"maps"
	"math"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── declarationTypes ─────────────────────────────────────────────────────────

func TestDeclarationTypes(t *testing.T) {
	type tc struct {
		ext   string
		check []string // keys that must be present and true
	}

	tests := []tc{
		{".go", []string{"function_declaration", "method_declaration", "type_declaration"}},
		{".py", []string{"function_definition", "async_function_definition", "class_definition", "decorated_definition"}},
		{".js", []string{"function_declaration", "class_declaration", "method_definition", "export_statement", "lexical_declaration"}},
		{".jsx", []string{"function_declaration", "class_declaration", "export_statement"}},
		{".ts", []string{"function_declaration", "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration"}},
		{".tsx", []string{"function_declaration", "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration"}},
		{".java", []string{"method_declaration", "class_declaration", "interface_declaration", "enum_declaration", "constructor_declaration"}},
		{".c", []string{"function_definition", "struct_specifier"}},
		{".h", []string{"function_definition", "struct_specifier"}},
		{".cpp", []string{"function_definition", "class_specifier", "struct_specifier", "namespace_definition"}},
		{".cc", []string{"function_definition", "class_specifier", "namespace_definition"}},
		{".cxx", []string{"function_definition", "struct_specifier"}},
		{".hpp", []string{"class_specifier", "namespace_definition"}},
		{".hx", []string{"class_specifier", "namespace_definition"}},
		{".ino", []string{"function_definition", "class_specifier", "namespace_definition"}},
		{".rs", []string{"function_item", "impl_item", "struct_item", "enum_item", "trait_item", "mod_item"}},
		{".cs", []string{"method_declaration", "class_declaration", "interface_declaration", "struct_declaration", "enum_declaration", "namespace_declaration"}},
		{".rb", []string{"method", "singleton_method", "class", "module"}},
		{".scala", []string{"function_definition", "class_definition", "object_definition", "trait_definition"}},
		{".sbt", []string{"function_definition", "class_definition", "object_definition", "trait_definition"}},
		{".sc", []string{"function_definition", "class_definition", "object_definition", "trait_definition"}},
		{".sh", []string{"function_definition", "command", "if_statement", "for_statement", "while_statement", "case_statement"}},
		{".bash", []string{"function_definition", "command", "if_statement"}},
		{".html", []string{"element", "script_element", "style_element"}},
		{".htm", []string{"element", "script_element", "style_element"}},
		{".css", []string{"rule_set", "at_rule", "media_statement", "keyframes_statement", "import_statement"}},
		{".json", []string{"pair", "object", "array"}},
		{".php", []string{"function_definition", "class_declaration", "method_declaration", "interface_declaration", "trait_declaration"}},
		{".hs", []string{"function", "class_decl", "instance_decl", "data_declaration", "newtype_declaration", "type_synonym_declaration", "signature"}},
		{".jl", []string{"function_definition", "macro_definition", "struct_definition", "module_definition", "abstract_definition"}},
		{".ml", []string{"value_definition", "type_definition", "module_definition", "class_definition", "external"}},
		{".mli", []string{"value_definition", "type_definition", "module_definition", "class_definition", "external"}},
		{".ejs", []string{"template", "content", "code", "output_code"}},
		{".erb", []string{"template", "content", "code", "output_code"}},
		{".yaml", []string{"block_mapping_pair", "block_sequence_item", "document"}},
		{".yml", []string{"block_mapping_pair", "block_sequence_item", "document"}},
		{".toml", []string{"table", "array_table", "pair"}},
		{".xml", []string{"element", "self_closing_element", "processing_instruction"}},
		{".svg", []string{"element", "self_closing_element", "processing_instruction"}},
		{".xsd", []string{"element", "self_closing_element", "processing_instruction"}},
		{".xslt", []string{"element", "self_closing_element", "processing_instruction"}},
		{".xsl", []string{"element", "self_closing_element", "processing_instruction"}},
		{".rng", []string{"element", "self_closing_element", "processing_instruction"}},
		{".lua", []string{"function_declaration", "local_function", "function_definition"}},
		{".zig", []string{"function_declaration", "container_declaration", "test_declaration", "global_variable_declaration"}},
		{".svelte", []string{"element", "script_element", "style_element", "if_statement", "each_statement"}},
		{".tf", []string{"block", "attribute"}},
		{".hcl", []string{"block", "attribute"}},
		{".tfvars", []string{"block", "attribute"}},
		{".tofu", []string{"block", "attribute"}},
		{".mk", []string{"rule", "variable_assignment", "include_directive"}},
		{".vue", []string{"element", "script_element", "style_element", "template_element"}},
		{".dockerfile", []string{"from_instruction", "run_instruction", "cmd_instruction", "entrypoint_instruction", "copy_instruction", "add_instruction", "env_instruction", "arg_instruction"}},
		{".nix", []string{"function_expression", "binding", "attrset_expression", "let_expression", "with_expression"}},
		{".groovy", []string{"method_declaration", "class_declaration", "closure", "constructor_declaration", "interface_declaration"}},
		{".gradle", []string{"method_declaration", "class_declaration", "closure", "constructor_declaration", "interface_declaration"}},
		{".clj", []string{"list_lit", "map_lit", "anon_fn_lit"}},
		{".cljs", []string{"list_lit", "map_lit", "anon_fn_lit"}},
		{".cljc", []string{"list_lit", "map_lit", "anon_fn_lit"}},
		{".erl", []string{"function_clause", "attribute", "export_attribute"}},
		{".hrl", []string{"function_clause", "attribute", "export_attribute"}},
		{".graphql", []string{"definition", "object_type_definition", "interface_type_definition", "field_definition", "enum_type_definition", "input_object_type_definition", "directive_definition"}},
		{".gql", []string{"definition", "object_type_definition", "interface_type_definition", "field_definition", "enum_type_definition", "input_object_type_definition", "directive_definition"}},
		{".astro", []string{"frontmatter", "element", "expression", "fragment"}},
		{".angular", []string{"element", "text_interpolation", "structural_directive"}},
		{".j2", []string{"statement", "expression", "comment", "block_start"}},
		{".jinja", []string{"statement", "expression", "comment", "block_start"}},
		{".jinja2", []string{"statement", "expression", "comment", "block_start"}},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			types := declarationTypes(tt.ext)
			require.NotNil(t, types)

			for _, k := range tt.check {
				assert.True(t, types[k], "expected key %q in declarationTypes(%q)", k, tt.ext)
			}
		})
	}

	t.Run("unknown returns empty map", func(t *testing.T) {
		assert.Empty(t, declarationTypes(".unknown"))
		assert.Empty(t, declarationTypes(""))
		assert.Empty(t, declarationTypes(".hbs"))
		assert.Empty(t, declarationTypes(".handlebars"))
	})
}

// ─── NewASTSplitter ───────────────────────────────────────────────────────────

func TestNewASTSplitter(t *testing.T) {
	s := NewASTSplitter(1500, 200)
	assert.Equal(t, 1500, s.ChunkSize)
	assert.Equal(t, 200, s.Overlap)
	assert.NotNil(t, s.fallback)
}

// ─── effectiveChunkSize ───────────────────────────────────────────────────────

func TestASTSplitter_EffectiveChunkSize(t *testing.T) {
	t.Run("zero uses default 4000", func(t *testing.T) {
		s := &ASTSplitter{ChunkSize: 0}
		assert.Equal(t, 4000, s.effectiveChunkSize())
	})
	t.Run("positive uses configured value", func(t *testing.T) {
		s := &ASTSplitter{ChunkSize: 800}
		assert.Equal(t, 800, s.effectiveChunkSize())
	})
}

// ─── splitNode ────────────────────────────────────────────────────────────────

func TestASTSplitter_SplitNode_FitsInChunk(t *testing.T) {
	s := NewASTSplitter(200, 20)
	text := "func Foo() {\n\treturn nil\n}\n"

	chunks := s.splitNode(text, 5, 7, "file.go")

	require.Len(t, chunks, 1)
	assert.Equal(t, text, chunks[0].Content)
	assert.Equal(t, 5, chunks[0].StartLine)
	assert.Equal(t, 7, chunks[0].EndLine)
}

func TestASTSplitter_SplitNode_FallbackWithLineOffset(t *testing.T) {
	// tiny chunk so the text must be split
	s := NewASTSplitter(20, 5)
	// 3 lines, each ~15 chars → total ~45 > 20
	line := strings.Repeat("x", 14) + "\n"
	text := strings.Repeat(line, 3)

	// pretend this node lives at absolute lines 10..12
	chunks := s.splitNode(text, 10, 12, "file.go")

	require.NotEmpty(t, chunks)
	// The TextSplitter counts a trailing "" when content ends with \n,
	// so the adjusted EndLine may be startLine + numNewlines (= 13 here).
	// The key invariant is that the offset is applied correctly.
	for i, c := range chunks {
		assert.GreaterOrEqual(t, c.StartLine, 10, "chunk[%d] StartLine below absolute start", i)
		assert.LessOrEqual(t, c.StartLine, c.EndLine, "chunk[%d] inverted range", i)
	}
	// First chunk must start at the node's absolute start line
	assert.Equal(t, 10, chunks[0].StartLine)
}

// ─── ASTSplitter.Split – fallback for unknown extension ──────────────────────

func TestASTSplitter_Split_UnknownExtensionFallsBack(t *testing.T) {
	s := NewASTSplitter(50, 5)
	line := strings.Repeat("w", 20) + "\n"
	content := strings.Repeat(line, 5)

	chunks := collectChunksFromString(t, s, content, "file.unknown")

	require.NotEmpty(t, chunks)
	assert.Equal(t, 1, chunks[0].StartLine, "fallback TextSplitter must start at line 1")
}

func TestASTSplitter_Split_PathFallbackStreamsWithoutFullRead(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
	}{
		{name: "unknown extension", filePath: "file.unknown"},
		{name: "deferred markdown", filePath: "README.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewASTSplitter(10, 2)
			stopErr := errors.New("stop after first chunk")
			wantReadErr := errors.New("reader exhausted")

			err := s.Split(
				io.MultiReader(strings.NewReader("alpha\nbeta\n"), iotest.ErrReader(wantReadErr)),
				tt.filePath,
				func(Chunk) error {
					return stopErr
				},
			)

			require.ErrorIs(t, err, stopErr)
			assert.NotErrorIs(t, err, wantReadErr, "fallback should emit before reading the entire stream")
		})
	}
}

func TestASTSplitter_Split_ContentSensitiveDetectionMatchesSupportedPath(t *testing.T) {
	tests := []struct {
		name          string
		filePath      string
		supportedPath string
		content       string
		chunkSize     int
		overlap       int
	}{
		{
			name:          "extensionless shebang shell",
			filePath:      "script",
			supportedPath: "script.sh",
			chunkSize:     20,
			overlap:       5,
			content: `#!/usr/bin/env bash
setup() {
	echo hi
}

teardown() {
	echo bye
}

setup
teardown
`,
		},
		{
			name:          "unknown extension modeline python",
			filePath:      "notes.txt",
			supportedPath: "notes.py",
			chunkSize:     80,
			overlap:       10,
			content:       "# -*- mode: python -*-\n\n" + pyMultiClass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewASTSplitter(tt.chunkSize, tt.overlap)

			detectedChunks := collectChunksFromString(t, s, tt.content, tt.filePath)
			supportedChunks := collectChunksFromString(t, s, tt.content, tt.supportedPath)

			require.NotEmpty(t, detectedChunks)
			assert.Equal(t, supportedChunks, detectedChunks)
		})
	}
}

// ─── ASTSplitter.Split – extensionless special-case files ────────────────────

func TestASTSplitter_Split_ExtensionlessFiles(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		known    bool
	}{
		{"Makefile", "Makefile", true},
		{"makefile lowercase", "makefile", true},
		{"GNUmakefile", "GNUmakefile", true},
		{"gnumakefile", "gnumakefile", true},
		{"Dockerfile", "Dockerfile", true},
		{"dockerfile lowercase", "dockerfile", true},
		{"unknown extensionless", "README", false},
	}

	// A short snippet that recognized files should still chunk without panicking.
	// Whether AST or text fallback is used depends on official grammar support.
	content := strings.Repeat("a", 10) + "\n"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewASTSplitter(5000, 100) // large chunkSize → fast path for AST
			chunks := collectChunksFromString(t, s, content, tt.filePath)

			if tt.known {
				require.NotEmpty(t, chunks)
				assert.Equal(t, 1, chunks[0].StartLine)
			} else {
				require.NotEmpty(t, chunks)
			}
		})
	}
}

// ─── ASTSplitter.Split – extension case normalisation ────────────────────────

func TestASTSplitter_Split_ExtensionCaseNormalised(t *testing.T) {
	s := NewASTSplitter(5000, 100)
	content := "package main\n"

	// .Go → normalised to .go → should use Go language (fast path)
	chunks := collectChunksFromString(t, s, content, "main.Go")

	require.NotEmpty(t, chunks)
	assert.Equal(t, 1, chunks[0].StartLine)
}

// ─── ASTSplitter.Split – fast path (content fits in one chunk) ───────────────

func TestASTSplitter_Split_FastPath_SingleChunk(t *testing.T) {
	s := NewASTSplitter(5000, 100)
	content := "package main\n\nfunc main() {}\n"

	chunks := collectChunksFromString(t, s, content, "main.go")

	require.Len(t, chunks, 1)
	assert.Equal(t, content, chunks[0].Content)
	assert.Equal(t, 1, chunks[0].StartLine)
	// "package main\n\nfunc main() {}\n" ends with \n →
	// splitLines returns 4 elements (3 lines + trailing "") → EndLine=4.
	assert.Equal(t, 4, chunks[0].EndLine)
}

// ─── ASTSplitter.Split – fast path with empty content ────────────────────────

func TestASTSplitter_Split_FastPath_EmptyContent(t *testing.T) {
	s := NewASTSplitter(5000, 100)

	chunks := collectChunksFromString(t, s, "", "main.go")

	assert.Nil(t, chunks)
}

// ─── ASTSplitter.Split – Go: declaration-based chunking ──────────────────────

// goMultiFunc is a valid Go source file with two top-level functions
// preceded by a package clause and import declaration.
const goMultiFunc = `package main

import "fmt"

func Hello() string {
	return fmt.Sprintf("Hello, World!")
}

func Goodbye() string {
	return fmt.Sprintf("Goodbye, World!")
}
`

const goDocCommentBeforeDecl = `package main

// Hello returns a greeting.
func Hello() string {
	return "hello"
}

func Goodbye() string {
	return "bye"
}
`

func TestASTSplitter_Split_GoDeclarations(t *testing.T) {
	// chunkSize=100: goMultiFunc (~157 chars) exceeds it → AST path; each
	// individual function (~60 chars) fits easily within the limit.
	s := NewASTSplitter(100, 10)

	chunks := collectChunksFromString(t, s, goMultiFunc, "main.go")

	require.Greater(t, len(chunks), 1, "multi-function Go file should produce multiple chunks")

	// Each chunk must have a valid, non-empty line range
	for i, c := range chunks {
		assert.NotEmpty(t, c.Content, "chunk[%d] must have content", i)
		assert.GreaterOrEqual(t, c.StartLine, 1, "chunk[%d] StartLine must be >= 1", i)
		assert.LessOrEqual(t, c.StartLine, c.EndLine, "chunk[%d] must have valid range", i)
	}

	// Line coverage: first chunk starts at 1, last ends at the final line
	assert.Equal(t, 1, chunks[0].StartLine)
	assert.Equal(t, strings.Count(goMultiFunc, "\n"), chunks[len(chunks)-1].EndLine)
	assert.Contains(t, chunks[0].Content, "package main")
	assert.Contains(t, chunks[0].Content, "func Hello()")
	assert.NotContains(t, chunks[0].Content, "func Goodbye()")

	// Both function bodies must appear somewhere in the output
	combined := combinedContent(chunks)
	assert.Contains(t, combined, "func Hello()")
	assert.Contains(t, combined, "func Goodbye()")
}

func TestASTSplitter_Split_GoDocCommentMergedIntoDeclaration(t *testing.T) {
	s := NewASTSplitter(90, 10)

	chunks := collectChunksFromString(t, s, goDocCommentBeforeDecl, "main.go")

	require.Len(t, chunks, 2)
	assert.Equal(t, goDocCommentBeforeDecl, combinedContent(chunks))
	assert.Contains(t, chunks[0].Content, "package main")
	assert.Contains(t, chunks[0].Content, "// Hello returns a greeting.\nfunc Hello()")
	assert.NotContains(t, chunks[0].Content, "func Goodbye()")

	helloChunk := chunkContaining(t, chunks, "func Hello()")
	assert.Contains(t, helloChunk.Content, "// Hello returns a greeting.\nfunc Hello()")
	assert.Equal(t, 1, helloChunk.StartLine)

	goodbyeChunk := chunkContaining(t, chunks, "func Goodbye()")
	assert.NotContains(t, goodbyeChunk.Content, "// Hello returns a greeting.")
}

// ─── ASTSplitter.Split – Python: class-based chunking ───────────────────────

const pyMultiClass = `class Animal:
    def __init__(self, name):
        self.name = name

    def speak(self):
        raise NotImplementedError


class Dog(Animal):
    def speak(self):
        return f"{self.name} says Woof!"


class Cat(Animal):
    def speak(self):
        return f"{self.name} says Meow!"
`

func TestASTSplitter_Split_PythonDeclarations(t *testing.T) {
	s := NewASTSplitter(100, 10)

	chunks := collectChunksFromString(t, s, pyMultiClass, "animals.py")

	require.Greater(t, len(chunks), 1, "multi-class Python file should produce multiple chunks")

	combined := combinedContent(chunks)
	assert.Contains(t, combined, "class Animal")
	assert.Contains(t, combined, "class Dog")
	assert.Contains(t, combined, "class Cat")
}

// ─── ASTSplitter.Split – Rust: function-based chunking ───────────────────────

const rustMultiFunc = `fn add(a: i32, b: i32) -> i32 {
    a + b
}

fn subtract(a: i32, b: i32) -> i32 {
    a - b
}

fn multiply(a: i32, b: i32) -> i32 {
    a * b
}
`

func TestASTSplitter_Split_RustDeclarations(t *testing.T) {
	s := NewASTSplitter(60, 10)

	chunks := collectChunksFromString(t, s, rustMultiFunc, "math.rs")

	require.Greater(t, len(chunks), 1, "multi-function Rust file should produce multiple chunks")

	combined := combinedContent(chunks)
	assert.Contains(t, combined, "fn add")
	assert.Contains(t, combined, "fn subtract")
	assert.Contains(t, combined, "fn multiply")
}

// ─── ASTSplitter.Split – TypeScript: interface + function ────────────────────

const tsMultiDecl = `interface User {
    name: string;
    age: number;
}

interface Admin {
    role: string;
    permissions: string[];
}

function greet(user: User): string {
    return "Hello, " + user.name;
}

function farewell(user: User): string {
    return "Goodbye, " + user.name;
}
`

const tsDocCommentBeforeFunction = `interface User {
    name: string;
}

/** Greets the user. */
function greet(user: User): string {
    return "Hello, " + user.name;
}

function farewell(user: User): string {
    return "Goodbye, " + user.name;
}
`

const makefileMultiRule = `build:
	@echo build

test:
	@echo test

deploy:
	@echo deploy
`

const dockerfileMultiInstruction = `FROM alpine:3.20
RUN apk add --no-cache bash
COPY . /app
CMD ["sh", "-c", "echo ok"]
`

const jsxMultiDecl = `import React from "react";

export function Title() {
	return <h1>Hello</h1>;
}

const Footer = () => <footer>Bye</footer>;
`

const bashMultiFunc = `setup() {
	echo setup
}

build() {
	echo build
}

deploy() {
	echo deploy
}
`

const yamlMultiDoc = `---
top:
  alpha: 1
  beta: 2
---
next:
  gamma: 3
  delta: 4
`

const cMultiDecl = `int add(int a, int b) {
  return a + b;
}

int subtract(int a, int b) {
  return a - b;
}
`

const csharpMultiDecl = `class Greeter {
    public string Hello() {
        return "hi";
    }
}

class Parting {
    public string Bye() {
        return "bye";
    }
}
`

const cppMultiDecl = `int add(int a, int b) {
  return a + b;
}

namespace math {
int subtract(int a, int b) {
  return a - b;
}
}
`

const cssMultiRule = `body { color: red; }

main { color: blue; }

@media screen { body { color: black; } }
`

const htmlMultiElement = `<section>One</section>
<section>Two</section>
<script>const answer = 42;</script>
`

const javaMultiDecl = `class Greeter {
    String hello() { return "hi"; }
}

class Parting {
    String bye() { return "bye"; }
}
`

const phpMultiDecl = `<?php
function hello() {
    return "hi";
}

function bye() {
    return "bye";
}
`

const rubyMultiDecl = `class Greeter
  def hello
    "hi"
  end
end

class Parting
  def bye
    "bye"
  end
end
`

const scalaMultiDecl = `object Greeter {
  def hello(): String = "hi"
}

object Parting {
  def bye(): String = "bye"
}
`

const ocamlMultiDecl = `let hello name =
  "hi " ^ name

let bye name =
  "bye " ^ name
`

const ocamlInterfaceMultiDecl = `val hello : string -> string
val bye : string -> string
`

const arduinoMultiDecl = `int add(int a, int b) {
  return a + b;
}

int subtract(int a, int b) {
  return a - b;
}
`

const hclMultiBlock = `locals {
  name = "demo"
}

resource "null_resource" "one" {
  triggers = {
    name = local.name
  }
}
`

const luaMultiDecl = `function hello(name)
  return "hi " .. name
end

local function bye(name)
  return "bye " .. name
end
`

const tomlMultiTable = `title = "demo"

[server]
host = "localhost"

[clients]
count = 2
`

const svgMultiElement = `<?xml version="1.0"?>
<svg>
  <rect width="10" height="10" />
  <circle r="5" />
</svg>
`

func TestASTSplitter_Split_TypeScriptDeclarations(t *testing.T) {
	s := NewASTSplitter(80, 10)

	chunks := collectChunksFromString(t, s, tsMultiDecl, "users.ts")

	require.NotEmpty(t, chunks)

	combined := combinedContent(chunks)
	assert.Contains(t, combined, "interface User")
	assert.Contains(t, combined, "interface Admin")
	assert.Contains(t, combined, "function greet")
	assert.Contains(t, combined, "function farewell")
}

func TestASTSplitter_Split_TypeScriptDocCommentMergedIntoDeclaration(t *testing.T) {
	s := NewASTSplitter(100, 10)

	chunks := collectChunksFromString(t, s, tsDocCommentBeforeFunction, "users.ts")

	require.Len(t, chunks, 3)
	assert.Equal(t, tsDocCommentBeforeFunction, combinedContent(chunks))
	assert.Contains(t, chunks[0].Content, "interface User")

	greetChunk := chunkContaining(t, chunks, "function greet")
	assert.Contains(t, greetChunk.Content, "/** Greets the user. */\nfunction greet")

	farewellChunk := chunkContaining(t, chunks, "function farewell")
	assert.NotContains(t, farewellChunk.Content, "/** Greets the user. */")
}

func TestASTSplitter_Split_DockerfileFallsBackToTextChunks(t *testing.T) {
	resolved := resolveLanguage("Dockerfile")
	require.NotNil(t, resolved)
	assert.Equal(t, grammarSupportSupportedNow, resolved.grammar.Support)

	astChunks := collectChunksFromString(t, NewASTSplitter(35, 5), dockerfileMultiInstruction, "Dockerfile")
	textChunks := collectChunksFromString(t, NewTextSplitter(35, 5), dockerfileMultiInstruction, "Dockerfile")

	assert.Equal(t, textChunks, astChunks)
	assert.Equal(t, dockerfileMultiInstruction, mergeChunkContent(astChunks))
}

func TestASTSplitter_Split_DeferredSpecialFilenamesFallBackToTextChunks(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
	}{
		{name: "Makefile", filePath: "Makefile"},
		{name: "GNUmakefile", filePath: "GNUmakefile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := resolveLanguage(tt.filePath)
			require.NotNil(t, resolved)
			assert.Equal(t, grammarSupportDeferred, resolved.grammar.Support)

			astChunks := collectChunksFromString(t, NewASTSplitter(30, 5), makefileMultiRule, tt.filePath)
			textChunks := collectChunksFromString(t, NewTextSplitter(30, 5), makefileMultiRule, tt.filePath)

			assert.Equal(t, textChunks, astChunks)
			assert.Equal(t, makefileMultiRule, mergeChunkContent(astChunks))
		})
	}
}

func TestASTSplitter_Split_ASTPathMigrationSmoke(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		resolvedExt string
		content     string
		chunkSize   int
		markers     []string
	}{
		{
			name:        "Arduino",
			filePath:    "sketch.ino",
			resolvedExt: ".ino",
			content:     arduinoMultiDecl,
			chunkSize:   60,
			markers:     []string{"int add", "int subtract"},
		},
		{
			name:        "C",
			filePath:    "math.c",
			resolvedExt: ".c",
			content:     cMultiDecl,
			chunkSize:   60,
			markers:     []string{"int add", "int subtract"},
		},
		{
			name:        "C#",
			filePath:    "Greeter.cs",
			resolvedExt: ".cs",
			content:     csharpMultiDecl,
			chunkSize:   75,
			markers:     []string{"class Greeter", "class Parting"},
		},
		{
			name:        "C++ alias",
			filePath:    "math.cc",
			resolvedExt: ".cc",
			content:     cppMultiDecl,
			chunkSize:   70,
			markers:     []string{"int add", "namespace math", "int subtract"},
		},
		{
			name:        "CSS",
			filePath:    "styles.css",
			resolvedExt: ".css",
			content:     cssMultiRule,
			chunkSize:   35,
			markers:     []string{"body { color: red; }", "main { color: blue; }", "@media screen"},
		},
		{
			name:        "HCL alias",
			filePath:    "main.tf",
			resolvedExt: ".tf",
			content:     hclMultiBlock,
			chunkSize:   85,
			markers:     []string{"locals {", "resource \"null_resource\" \"one\""},
		},
		{
			name:        "HTML alias",
			filePath:    "index.htm",
			resolvedExt: ".htm",
			content:     htmlMultiElement,
			chunkSize:   30,
			markers:     []string{"<section>One</section>", "<section>Two</section>", "<script>const answer = 42;</script>"},
		},
		{
			name:        "Java",
			filePath:    "Greeter.java",
			resolvedExt: ".java",
			content:     javaMultiDecl,
			chunkSize:   70,
			markers:     []string{"class Greeter", "class Parting"},
		},
		{
			name:        "Lua",
			filePath:    "script.lua",
			resolvedExt: ".lua",
			content:     luaMultiDecl,
			chunkSize:   55,
			markers:     []string{"function hello", "local function bye"},
		},
		{
			name:        "PHP",
			filePath:    "index.php",
			resolvedExt: ".php",
			content:     phpMultiDecl,
			chunkSize:   45,
			markers:     []string{"function hello", "function bye"},
		},
		{
			name:        "Ruby",
			filePath:    "greeting.rb",
			resolvedExt: ".rb",
			content:     rubyMultiDecl,
			chunkSize:   80,
			markers:     []string{"class Greeter", "class Parting"},
		},
		{
			name:        "Scala alias",
			filePath:    "build.sbt",
			resolvedExt: ".sbt",
			content:     scalaMultiDecl,
			chunkSize:   45,
			markers:     []string{"object Greeter", "object Parting"},
		},
		{
			name:        "TOML",
			filePath:    "config.toml",
			resolvedExt: ".toml",
			content:     tomlMultiTable,
			chunkSize:   35,
			markers:     []string{"title = \"demo\"", "[server]", "[clients]"},
		},
		{
			name:        "OCaml",
			filePath:    "main.ml",
			resolvedExt: ".ml",
			content:     ocamlMultiDecl,
			chunkSize:   30,
			markers:     []string{"let hello", "let bye"},
		},
		{
			name:        "OCaml interface",
			filePath:    "main.mli",
			resolvedExt: ".mli",
			content:     ocamlInterfaceMultiDecl,
			chunkSize:   20,
			markers:     []string{"val hello", "val bye"},
		},
		{
			name:        "Go",
			filePath:    "main.go",
			resolvedExt: ".go",
			content:     goMultiFunc,
			chunkSize:   100,
			markers:     []string{"func Hello()", "func Goodbye()"},
		},
		{
			name:        "Python",
			filePath:    "animals.py",
			resolvedExt: ".py",
			content:     pyMultiClass,
			chunkSize:   100,
			markers:     []string{"class Animal", "class Dog", "class Cat"},
		},
		{
			name:        "TypeScript",
			filePath:    "users.ts",
			resolvedExt: ".ts",
			content:     tsMultiDecl,
			chunkSize:   80,
			markers:     []string{"interface User", "interface Admin", "function greet", "function farewell"},
		},
		{
			name:        "JSX alias",
			filePath:    "component.jsx",
			resolvedExt: ".jsx",
			content:     jsxMultiDecl,
			chunkSize:   70,
			markers:     []string{"export function Title()", "const Footer = () => <footer>Bye</footer>;"},
		},
		{
			name:        "XML alias",
			filePath:    "icon.svg",
			resolvedExt: ".svg",
			content:     svgMultiElement,
			chunkSize:   45,
			markers:     []string{"<?xml version=\"1.0\"?>", "<rect width=\"10\" height=\"10\" />", "<circle r=\"5\" />"},
		},
		{
			name:        "Bash alias",
			filePath:    "build.bash",
			resolvedExt: ".bash",
			content:     bashMultiFunc,
			chunkSize:   40,
			markers:     []string{"setup()", "build()", "deploy()"},
		},
		{
			name:        "YAML",
			filePath:    "config.yaml",
			resolvedExt: ".yaml",
			content:     yamlMultiDoc,
			chunkSize:   25,
			markers:     []string{"top:", "next:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, resolveLanguageFromExt(tt.resolvedExt))

			s := NewASTSplitter(tt.chunkSize, 5)
			chunks := collectChunksFromString(t, s, tt.content, tt.filePath)

			require.Greater(t, len(chunks), 1, "expected AST path to emit multiple chunks")
			assertASTChunkPath(t, chunks, tt.content)

			combined := combinedContent(chunks)
			for _, marker := range tt.markers {
				assert.Contains(t, combined, marker)
				assert.Equal(t, 1, strings.Count(combined, marker), "marker %q should not be duplicated", marker)
			}
		})
	}
}

// ─── ASTSplitter.Split – splitNode fallback when node exceeds chunkSize ──────

// A Go file whose single function is intentionally larger than the chunkSize,
// so splitNode must delegate to the fallback TextSplitter and adjust line offsets.
func TestASTSplitter_Split_LargeNodeDelegatedToFallback(t *testing.T) {
	// Build a Go function that is ~300 chars; chunkSize=80 < 300 → splitNode fallback
	var sb strings.Builder
	sb.WriteString("package main\n\nfunc BigFunc() {\n")

	for range 12 {
		sb.WriteString("\t_ = \"line " + strings.Repeat("x", 12) + "\"\n")
	}

	sb.WriteString("}\n")
	content := sb.String()

	s := NewASTSplitter(80, 15)
	chunks := collectChunksFromString(t, s, content, "big.go")

	require.Greater(t, len(chunks), 1,
		"oversized function should be delegated to fallback, producing multiple chunks")

	// Every chunk line number must be valid (>= 1)
	for i, c := range chunks {
		assert.GreaterOrEqual(t, c.StartLine, 1, "chunk[%d] StartLine invalid", i)
		assert.LessOrEqual(t, c.StartLine, c.EndLine, "chunk[%d] inverted range", i)
	}
}

// ─── ASTSplitter.Split – only non-declarations (no top-level decl nodes) ─────

func TestASTSplitter_Split_OnlyNonDeclarations(t *testing.T) {
	// A Go file with just a package clause and import – no function declarations.
	// All top-level nodes are non-declarations and will be grouped together.
	content := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n"
	s := NewASTSplitter(10, 2) // tiny chunkSize → forces AST path

	chunks := collectChunksFromString(t, s, content, "noDecl.go")

	require.NotEmpty(t, chunks)
	combined := combinedContent(chunks)
	assert.Contains(t, combined, "package main")
	assert.Contains(t, combined, "import")
}

// ─── ASTSplitter.Split – interleaved decl / non-decl / decl ordering ─────────

// goInterleavedDeclNonDecl is a valid Go source with declarations and a
// top-level var sandwiched between two functions.  In the Go tree-sitter
// grammar, var_declaration is NOT a "declaration type" (see declarationTypes),
// so the order of AST nodes is: package_clause (non-decl) → function_declaration
// (decl) → var_declaration (non-decl) → function_declaration (decl).
// This exercises the else-branch of the main chunking loop when a non-decl
// group appears *after* at least one decl has already been processed.
const goInterleavedDeclNonDecl = `package main

func First() int {
	return 1
}

var Sentinel = 42

// Second returns the sentinel.
func Second() int {
	return Sentinel
}
`

func TestASTSplitter_Split_GoInterleavedDeclNonDecl(t *testing.T) {
	// chunkSize=80: total content (~80+ chars) exceeds it → AST path.
	// Each individual function (~30 chars) fits within the limit.
	s := NewASTSplitter(80, 10)

	chunks := collectChunksFromString(t, s, goInterleavedDeclNonDecl, "interleaved.go")

	require.NotEmpty(t, chunks)
	assert.Equal(t, goInterleavedDeclNonDecl, combinedContent(chunks))
	require.Len(t, chunks, 2)

	combined := combinedContent(chunks)
	assert.Contains(t, combined, "func First()")
	assert.Contains(t, combined, "Sentinel")
	assert.Contains(t, combined, "func Second()")

	firstChunk := chunkContaining(t, chunks, "func First()")
	assert.Contains(t, firstChunk.Content, "func First()")
	assert.Contains(t, firstChunk.Content, "var Sentinel = 42")
	assert.NotContains(t, firstChunk.Content, "// Second returns the sentinel.")
	assert.NotContains(t, firstChunk.Content, "func Second()")

	secondChunk := chunkContaining(t, chunks, "func Second()")
	assert.Contains(t, secondChunk.Content, "// Second returns the sentinel.\nfunc Second()")

	for i, c := range chunks {
		assert.GreaterOrEqual(t, c.StartLine, 1, "chunk[%d] StartLine invalid", i)
		assert.LessOrEqual(t, c.StartLine, c.EndLine, "chunk[%d] inverted range", i)
	}
}

// ─── ASTSplitter.Split – extensionless files through the AST chunking path ────

// TestASTSplitter_Split_MakefileFallsBackToTextPath ensures deferred official
// grammars keep the text fallback even for extensionless special filenames.
func TestASTSplitter_Split_MakefileFallsBackToTextPath(t *testing.T) {
	content := strings.Repeat("target:\n\t@echo hi\n\n", 5)

	astChunks := collectChunksFromString(t, NewASTSplitter(40, 5), content, "Makefile")
	textChunks := collectChunksFromString(t, NewTextSplitter(40, 5), content, "Makefile")

	assert.Equal(t, textChunks, astChunks)
}

// TestASTSplitter_Split_DockerfileFallsBackToTextPath keeps the current text
// fallback for Dockerfile until an importable official binding is available.
func TestASTSplitter_Split_DockerfileFallsBackToTextPath(t *testing.T) {
	content := strings.Repeat("FROM ubuntu:22.04\nRUN apt-get update\n\n", 4)

	astChunks := collectChunksFromString(t, NewASTSplitter(60, 10), content, "Dockerfile")
	textChunks := collectChunksFromString(t, NewTextSplitter(60, 10), content, "Dockerfile")

	assert.Equal(t, textChunks, astChunks)
}

// ─── ASTSplitter.Split – zero top-level children ─────────────────────────────

// TestASTSplitter_Split_NoTopLevelChildren tests a known language file whose
// parsed AST has zero top-level declaration nodes so that the chunking loop
// body is never entered and the function returns nil.
// We use a whitespace-only TypeScript string that exceeds the chunk size.
func TestASTSplitter_Split_NoTopLevelChildren(t *testing.T) {
	// A valid-ish TS file containing nothing but newlines — tree-sitter may
	// still produce a root with zero relevant children depending on how the
	// grammar treats whitespace-only input.  What matters is that Split does
	// not panic and returns a (possibly nil) slice.
	content := strings.Repeat("\n", 200) // 200 newlines > chunkSize=50
	s := NewASTSplitter(50, 5)

	// Must not panic; result shape is grammar-dependent.
	chunks := collectChunksFromString(t, s, content, "empty.ts")
	for i, c := range chunks {
		assert.GreaterOrEqual(t, c.StartLine, 1, "chunk[%d] StartLine must be >= 1", i)
	}
}

func TestASTSplitter_Split_ReadError(t *testing.T) {
	s := NewASTSplitter(50, 5)
	wantErr := assert.AnError

	err := s.Split(iotest.ErrReader(wantErr), "main.go", func(Chunk) error {
		t.Fatal("emit must not be called")

		return nil
	})

	require.ErrorIs(t, err, wantErr)
}

func TestASTSplitter_Split_DeferredGrammarFallsBackToTextChunks(t *testing.T) {
	const markdownDoc = `# Title

First paragraph.

- one
- two
`

	const (
		chunkSize = 25
		overlap   = 5
	)

	resolved := resolveLanguage("README.md")
	require.NotNil(t, resolved)
	assert.Equal(t, grammarSupportDeferred, resolved.grammar.Support)

	astChunks := collectChunksFromString(t, NewASTSplitter(chunkSize, overlap), markdownDoc, "README.md")
	textChunks := collectChunksFromString(t, NewTextSplitter(chunkSize, overlap), markdownDoc, "README.md")

	require.NotEmpty(t, astChunks)
	assert.Equal(t, textChunks, astChunks)
	assert.Equal(t, markdownDoc, mergeChunkContent(astChunks))
}

func TestASTSplitter_Split_MalformedSupportedLanguageFallsBackToTextChunks(t *testing.T) {
	const malformedGo = "package main\n\nfunc (\n"

	astChunks := collectChunksFromString(t, NewASTSplitter(10, 2), malformedGo, "broken.go")
	textChunks := collectChunksFromString(t, NewTextSplitter(10, 2), malformedGo, "broken.go")

	require.NotEmpty(t, astChunks)
	assert.Equal(t, textChunks, astChunks)
}

func TestResolveLanguageForASTSplit_EmptyReaderReturnsNil(t *testing.T) {
	assert.Nil(t, resolveLanguageForASTSplit(bufio.NewReader(strings.NewReader("")), "README"))
}

func TestASTSplitter_SplitResolvedContent_EmitErrors(t *testing.T) {
	t.Run("declaration node", func(t *testing.T) {
		s := NewASTSplitter(10, 2)
		resolved := resolveLanguage("main.go")
		require.NotNil(t, resolved)

		err := s.splitResolvedContent([]byte("func main() {\n}\n"), resolved, "main.go", func(Chunk) error {
			return assert.AnError
		})

		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("non declaration prefix before declaration", func(t *testing.T) {
		s := NewASTSplitter(90, 5)
		resolved := resolveLanguage("main.go")
		require.NotNil(t, resolved)

		calls := 0
		err := s.splitResolvedContent([]byte(goDocCommentBeforeDecl), resolved, "main.go", func(Chunk) error {
			calls++
			if calls == 1 {
				return assert.AnError
			}

			return nil
		})

		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("trivia merged declaration", func(t *testing.T) {
		s := NewASTSplitter(90, 5)
		resolved := resolveLanguage("main.go")
		require.NotNil(t, resolved)

		calls := 0
		err := s.splitResolvedContent([]byte(goDocCommentBeforeDecl), resolved, "main.go", func(Chunk) error {
			calls++
			if calls == 2 {
				return assert.AnError
			}

			return nil
		})

		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("only non declarations", func(t *testing.T) {
		s := NewASTSplitter(10, 2)
		resolved := resolveLanguage("main.go")
		require.NotNil(t, resolved)

		err := s.splitResolvedContent([]byte("package main\n\nimport \"fmt\"\n"), resolved, "main.go", func(Chunk) error {
			return assert.AnError
		})

		require.ErrorIs(t, err, assert.AnError)
	})
}

func TestTreeByteOffsetAndLineNumberBounds(t *testing.T) {
	offset, ok := treeByteOffset(7, 8)
	assert.True(t, ok)
	assert.Equal(t, uint(7), offset)

	_, ok = treeByteOffset(uint(math.MaxInt)+1, math.MaxInt)
	assert.False(t, ok)

	_, ok = treeByteOffset(9, 8)
	assert.False(t, ok)

	line, ok := treeLineNumber(3)
	assert.True(t, ok)
	assert.Equal(t, 4, line)

	_, ok = treeLineNumber(uint(math.MaxInt))
	assert.False(t, ok)
}

func TestDeclarationTypes_FallbackSwitchCases(t *testing.T) {
	mutated := map[canonicalLanguageID]grammarBundle{}
	for _, id := range []canonicalLanguageID{
		canonicalLanguageJava,
		canonicalLanguageC,
		canonicalLanguageCPP,
		canonicalLanguageCSharp,
		canonicalLanguageRuby,
		canonicalLanguageHTML,
		canonicalLanguageCSS,
		canonicalLanguagePHP,
		canonicalLanguageOCaml,
		canonicalLanguageTOML,
		canonicalLanguageXML,
		canonicalLanguageLua,
		canonicalLanguageHCL,
	} {
		mutated[id] = grammarRegistry[id]
		bundle := grammarRegistry[id]
		bundle.DeclarationKinds = nil
		grammarRegistry[id] = bundle
	}

	t.Cleanup(func() {
		maps.Copy(grammarRegistry, mutated)
	})

	tests := []struct {
		ext  string
		kind string
	}{
		{ext: ".java", kind: "method_declaration"},
		{ext: ".c", kind: "function_definition"},
		{ext: ".cpp", kind: "namespace_definition"},
		{ext: ".cs", kind: "namespace_declaration"},
		{ext: ".rb", kind: "singleton_method"},
		{ext: ".htm", kind: "script_element"},
		{ext: ".css", kind: "media_statement"},
		{ext: ".php", kind: "trait_declaration"},
		{ext: ".ml", kind: "module_definition"},
		{ext: ".toml", kind: "array_table"},
		{ext: ".svg", kind: "processing_instruction"},
		{ext: ".lua", kind: "local_function"},
		{ext: ".tf", kind: "attribute"},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			assert.True(t, declarationTypes(tt.ext)[tt.kind])
		})
	}
}

func BenchmarkASTSplitter_Split_MigrationSmoke(b *testing.B) {
	tests := []struct {
		name      string
		filePath  string
		content   string
		chunkSize int
	}{
		{name: "Go", filePath: "main.go", content: goMultiFunc, chunkSize: 100},
		{name: "Python", filePath: "animals.py", content: pyMultiClass, chunkSize: 100},
		{name: "TypeScript", filePath: "users.ts", content: tsMultiDecl, chunkSize: 80},
		{name: "Makefile", filePath: "Makefile", content: makefileMultiRule, chunkSize: 30},
		{name: "Dockerfile", filePath: "Dockerfile", content: dockerfileMultiInstruction, chunkSize: 35},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			s := NewASTSplitter(tt.chunkSize, 5)

			b.ReportAllocs()
			b.SetBytes(int64(len(tt.content)))
			b.ResetTimer()

			for range b.N {
				chunks := collectChunksFromStringTB(b, s, tt.content, tt.filePath)
				if len(chunks) == 0 {
					b.Fatal("expected chunks")
				}
			}
		})
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func assertASTChunkPath(t *testing.T, chunks []Chunk, content string) {
	t.Helper()

	require.NotEmpty(t, chunks)
	assert.Equal(t, 1, chunks[0].StartLine)

	for i, c := range chunks {
		assert.NotEmpty(t, c.Content, "chunk[%d] must have content", i)
		assert.GreaterOrEqual(t, c.StartLine, 1, "chunk[%d] StartLine must be >= 1", i)
		assert.LessOrEqual(t, c.StartLine, c.EndLine, "chunk[%d] must have valid range", i)
	}

	for i := 1; i < len(chunks); i++ {
		assert.GreaterOrEqual(t, chunks[i].StartLine, chunks[i-1].EndLine,
			"chunk[%d] should not move backwards to stay on AST path", i)
	}

	assert.GreaterOrEqual(t, chunks[len(chunks)-1].EndLine, strings.Count(content, "\n"))
	assert.LessOrEqual(t, chunks[len(chunks)-1].EndLine, len(splitLines(content)))
}

// combinedContent concatenates all chunk Contents for assertion convenience.
func combinedContent(chunks []Chunk) string {
	return mergeChunkContent(chunks)
}

func chunkContaining(t *testing.T, chunks []Chunk, marker string) Chunk {
	t.Helper()

	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, marker) {
			return chunk
		}
	}

	t.Fatalf("expected chunk containing %q", marker)

	return Chunk{}
}
