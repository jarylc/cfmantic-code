package splitter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrammarForCanonicalName(t *testing.T) {
	tests := []struct {
		name              string
		canonical         string
		wantID            canonicalLanguageID
		wantSupport       grammarSupport
		wantSource        grammarSource
		wantGrammarImport string
		wantLanguageFunc  string
	}{
		{
			name:              "go official grammar",
			canonical:         "go",
			wantID:            canonicalLanguageGo,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-go/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "c official grammar",
			canonical:         "c",
			wantID:            canonicalLanguageC,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-c/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "csharp official grammar",
			canonical:         "c#",
			wantID:            canonicalLanguageCSharp,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "cpp official grammar",
			canonical:         "c++",
			wantID:            canonicalLanguageCPP,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-cpp/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "css official grammar",
			canonical:         "css",
			wantID:            canonicalLanguageCSS,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-css/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "html official grammar",
			canonical:         "html",
			wantID:            canonicalLanguageHTML,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-html/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "java official grammar",
			canonical:         "java",
			wantID:            canonicalLanguageJava,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-java/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "typescript official grammar",
			canonical:         "TypeScript",
			wantID:            canonicalLanguageTypeScript,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-typescript/bindings/go",
			wantLanguageFunc:  "LanguageTypescript",
		},
		{
			name:              "tsx official grammar",
			canonical:         "tsx",
			wantID:            canonicalLanguageTSX,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-typescript/bindings/go",
			wantLanguageFunc:  "LanguageTSX",
		},
		{
			name:              "yaml supported community grammar",
			canonical:         "yaml",
			wantID:            canonicalLanguageYAML,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "markdown deferred until adopted binding",
			canonical:         "markdown",
			wantID:            canonicalLanguageMarkdown,
			wantSupport:       grammarSupportDeferred,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "",
			wantLanguageFunc:  "",
		},
		{
			name:              "dockerfile supported community grammar",
			canonical:         "dockerfile",
			wantID:            canonicalLanguageDockerfile,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/camdencheek/tree-sitter-dockerfile",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "rust official grammar",
			canonical:         "rust",
			wantID:            canonicalLanguageRust,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-rust/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "javascript official grammar",
			canonical:         "javascript",
			wantID:            canonicalLanguageJavaScript,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-javascript/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "python official grammar",
			canonical:         "python",
			wantID:            canonicalLanguagePython,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-python/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "php official grammar",
			canonical:         "php",
			wantID:            canonicalLanguagePHP,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-php/bindings/go",
			wantLanguageFunc:  "LanguagePHP",
		},
		{
			name:              "ruby official grammar",
			canonical:         "ruby",
			wantID:            canonicalLanguageRuby,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-ruby/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "scala official grammar",
			canonical:         "scala",
			wantID:            canonicalLanguageScala,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-scala/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "ocaml official grammar",
			canonical:         "ocaml",
			wantID:            canonicalLanguageOCaml,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-ocaml/bindings/go",
			wantLanguageFunc:  "LanguageOCaml",
		},
		{
			name:              "ocaml interface official grammar",
			canonical:         "ocaml-interface",
			wantID:            canonicalLanguageOCamlInterface,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-ocaml/bindings/go",
			wantLanguageFunc:  "LanguageOCamlInterface",
		},
		{
			name:              "arduino supported community grammar",
			canonical:         "arduino",
			wantID:            canonicalLanguageArduino,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-arduino/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "gitattributes supported community grammar",
			canonical:         "gitattributes",
			wantID:            canonicalLanguageGitAttributes,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-gitattributes/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "go sum supported community grammar",
			canonical:         "go-sum",
			wantID:            canonicalLanguageGoSum,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-go-sum/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "hcl supported community grammar",
			canonical:         "hcl",
			wantID:            canonicalLanguageHCL,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-hcl/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "kotlin supported community grammar",
			canonical:         "kotlin",
			wantID:            canonicalLanguageKotlin,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "lua supported community grammar",
			canonical:         "lua",
			wantID:            canonicalLanguageLua,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "luau supported community grammar",
			canonical:         "luau",
			wantID:            canonicalLanguageLuau,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-luau/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "requirements supported community grammar",
			canonical:         "requirements",
			wantID:            canonicalLanguageRequirements,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-requirements/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "toml supported community grammar",
			canonical:         "toml",
			wantID:            canonicalLanguageTOML,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "xml supported community grammar",
			canonical:         "xml",
			wantID:            canonicalLanguageXML,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-xml/bindings/go",
			wantLanguageFunc:  "LanguageXML",
		},
		{
			name:              "dtd supported community grammar",
			canonical:         "dtd",
			wantID:            canonicalLanguageDTD,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-xml/bindings/go",
			wantLanguageFunc:  "LanguageDTD",
		},
		{
			name:              "shell official grammar",
			canonical:         "shell",
			wantID:            canonicalLanguageShell,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-bash/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "json official grammar",
			canonical:         "json",
			wantID:            canonicalLanguageJSON,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceOfficial,
			wantGrammarImport: "github.com/tree-sitter/tree-sitter-json/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:              "svelte community grammar",
			canonical:         "svelte",
			wantID:            canonicalLanguageSvelte,
			wantSupport:       grammarSupportSupportedNow,
			wantSource:        grammarSourceCommunity,
			wantGrammarImport: "github.com/tree-sitter-grammars/tree-sitter-svelte/bindings/go",
			wantLanguageFunc:  "Language",
		},
		{
			name:        "makefile deferred until adopted binding",
			canonical:   "makefile",
			wantID:      canonicalLanguageMakefile,
			wantSupport: grammarSupportDeferred,
			wantSource:  grammarSourceCommunity,
		},
		{
			name:        "astro deferred without selected go binding",
			canonical:   "astro",
			wantID:      canonicalLanguageAstro,
			wantSupport: grammarSupportDeferred,
			wantSource:  grammarSourceCommunity,
		},
		{
			name:        "unknown canonical id is unmapped",
			canonical:   "totally-unknown",
			wantID:      canonicalLanguageID("totally-unknown"),
			wantSupport: grammarSupportUnmapped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := grammarForCanonicalName(tt.canonical)

			assert.Equal(t, tt.wantID, bundle.ID)
			assert.Equal(t, tt.wantSupport, bundle.Support)
			assert.Equal(t, tt.wantSource, bundle.Source)
			assert.Equal(t, tt.wantGrammarImport, bundle.Parser.GrammarImportPath)
			assert.Equal(t, tt.wantLanguageFunc, bundle.Parser.LanguageFunc)

			if tt.wantSupport == grammarSupportSupportedNow {
				assert.Equal(t, treeSitterRuntimeImportPath, bundle.Parser.RuntimeImportPath)
				assert.True(t, bundle.Parser.RequiresCGo)
				assert.NotEmpty(t, bundle.Queries.Repository)
				assert.Equal(t, grammarQueriesDir, bundle.Queries.BaseDir)
				assert.NotEqual(t, queryAvailabilityDeferred, bundle.Queries.Tags.Availability)
				assert.NotEqual(t, queryAvailabilityUnmapped, bundle.Queries.Tags.Availability)
			} else {
				assert.Empty(t, bundle.Parser.RuntimeImportPath)
				assert.False(t, bundle.Parser.RequiresCGo)
			}
		})
	}
}

func TestGrammarRegistry_QueryAvailabilityIsExplicit(t *testing.T) {
	c := grammarForCanonicalName("c")
	assert.Equal(t, queryAvailabilityAvailable, c.Queries.Tags.Availability)

	css := grammarForCanonicalName("css")
	assert.Equal(t, queryAvailabilityAbsent, css.Queries.Tags.Availability)

	lua := grammarForCanonicalName("lua")
	assert.Equal(t, queryAvailabilityAvailable, lua.Queries.Tags.Availability)

	xml := grammarForCanonicalName("xml")
	assert.Equal(t, queryAvailabilityAbsent, xml.Queries.Tags.Availability)

	hcl := grammarForCanonicalName("hcl")
	assert.Equal(t, queryAvailabilityAbsent, hcl.Queries.Tags.Availability)

	yaml := grammarForCanonicalName("yaml")
	assert.Equal(t, queryAvailabilityAbsent, yaml.Queries.Tags.Availability)
	assert.Empty(t, yaml.Reason)

	bash := grammarForCanonicalName("shell")
	assert.Equal(t, queryAvailabilityAbsent, bash.Queries.Tags.Availability)

	deferred := grammarForCanonicalName("markdown")
	assert.Equal(t, queryAvailabilityDeferred, deferred.Queries.Tags.Availability)
	assert.NotEmpty(t, deferred.Reason)

	unmapped := grammarForCanonicalName("not-a-language")
	assert.Equal(t, queryAvailabilityUnmapped, unmapped.Queries.Tags.Availability)
}

func TestGrammarRegistry_SymbolDeclarationKinds(t *testing.T) {
	t.Run("go reuses chunk declaration kinds", func(t *testing.T) {
		bundle := grammarForCanonicalName("go")
		assert.Equal(t, bundle.DeclarationKinds, bundle.SymbolDeclarationKinds)
	})

	t.Run("shell narrows symbol extraction without widening chunking", func(t *testing.T) {
		bundle := grammarForCanonicalName("shell")
		assert.Equal(t, map[string]bool{"function_definition": true}, bundle.SymbolDeclarationKinds)
		assert.True(t, bundle.DeclarationKinds["command"])
		assert.True(t, bundle.DeclarationKinds["if_statement"])
		assert.False(t, bundle.SymbolDeclarationKinds["command"])
		assert.False(t, bundle.SymbolDeclarationKinds["if_statement"])
	})

	t.Run("rust keeps symbol-only extras separate from chunking", func(t *testing.T) {
		bundle := grammarForCanonicalName("rust")
		for _, kind := range []string{"function_signature_item", "type_item", "union_item"} {
			assert.True(t, bundle.SymbolDeclarationKinds[kind])
			assert.False(t, bundle.DeclarationKinds[kind])
		}
	})

	t.Run("typescript keeps symbol-only extras separate from chunking", func(t *testing.T) {
		bundle := grammarForCanonicalName("typescript")
		for _, kind := range []string{"abstract_class_declaration", "abstract_method_signature", "internal_module", "method_signature"} {
			assert.True(t, bundle.SymbolDeclarationKinds[kind])
			assert.False(t, bundle.DeclarationKinds[kind])
		}
	})

	t.Run("tsx shares typescript symbol extras", func(t *testing.T) {
		bundle := grammarForCanonicalName("tsx")
		for _, kind := range []string{"abstract_class_declaration", "abstract_method_signature", "internal_module", "method_signature"} {
			assert.True(t, bundle.SymbolDeclarationKinds[kind])
		}
	})

	t.Run("other grammars do not gain symbol extraction accidentally", func(t *testing.T) {
		bundle := grammarForCanonicalName("dockerfile")
		assert.Nil(t, bundle.SymbolDeclarationKinds)
	})
}

func TestGrammarForExt_AliasesAndDeferredCases(t *testing.T) {
	tests := []struct {
		name        string
		ext         string
		wantID      canonicalLanguageID
		wantSupport grammarSupport
	}{
		{name: "csharp", ext: ".cs", wantID: canonicalLanguageCSharp, wantSupport: grammarSupportSupportedNow},
		{name: "cpp alias .cc", ext: ".cc", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "cpp alias .cxx", ext: ".cxx", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "cpp alias .hpp", ext: ".hpp", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "cpp alias .hx", ext: ".hx", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "html alias", ext: ".htm", wantID: canonicalLanguageHTML, wantSupport: grammarSupportSupportedNow},
		{name: "java", ext: ".java", wantID: canonicalLanguageJava, wantSupport: grammarSupportSupportedNow},
		{name: "php", ext: ".php", wantID: canonicalLanguagePHP, wantSupport: grammarSupportSupportedNow},
		{name: "ruby", ext: ".rb", wantID: canonicalLanguageRuby, wantSupport: grammarSupportSupportedNow},
		{name: "scala alias", ext: ".sbt", wantID: canonicalLanguageScala, wantSupport: grammarSupportSupportedNow},
		{name: "ocaml", ext: ".ml", wantID: canonicalLanguageOCaml, wantSupport: grammarSupportSupportedNow},
		{name: "ocaml interface", ext: ".mli", wantID: canonicalLanguageOCamlInterface, wantSupport: grammarSupportSupportedNow},
		{name: "arduino", ext: ".ino", wantID: canonicalLanguageArduino, wantSupport: grammarSupportSupportedNow},
		{name: "gitattributes pseudo extension", ext: ".gitattributes", wantID: canonicalLanguageGitAttributes, wantSupport: grammarSupportSupportedNow},
		{name: "go sum pseudo extension", ext: ".go.sum", wantID: canonicalLanguageGoSum, wantSupport: grammarSupportSupportedNow},
		{name: "hcl", ext: ".hcl", wantID: canonicalLanguageHCL, wantSupport: grammarSupportSupportedNow},
		{name: "terraform alias .tf", ext: ".tf", wantID: canonicalLanguageHCL, wantSupport: grammarSupportSupportedNow},
		{name: "terraform vars alias", ext: ".tfvars", wantID: canonicalLanguageHCL, wantSupport: grammarSupportSupportedNow},
		{name: "opentofu alias", ext: ".tofu", wantID: canonicalLanguageHCL, wantSupport: grammarSupportSupportedNow},
		{name: "kotlin", ext: ".kt", wantID: canonicalLanguageKotlin, wantSupport: grammarSupportSupportedNow},
		{name: "kotlin alias", ext: ".kts", wantID: canonicalLanguageKotlin, wantSupport: grammarSupportSupportedNow},
		{name: "lua", ext: ".lua", wantID: canonicalLanguageLua, wantSupport: grammarSupportSupportedNow},
		{name: "luau", ext: ".luau", wantID: canonicalLanguageLuau, wantSupport: grammarSupportSupportedNow},
		{name: "requirements pseudo extension", ext: ".requirements.txt", wantID: canonicalLanguageRequirements, wantSupport: grammarSupportSupportedNow},
		{name: "toml", ext: ".toml", wantID: canonicalLanguageTOML, wantSupport: grammarSupportSupportedNow},
		{name: "xml", ext: ".xml", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .svg", ext: ".svg", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .xsd", ext: ".xsd", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .xslt", ext: ".xslt", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .xsl", ext: ".xsl", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .rng", ext: ".rng", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "dtd", ext: ".dtd", wantID: canonicalLanguageDTD, wantSupport: grammarSupportSupportedNow},
		{name: "bash alias", ext: ".bash", wantID: canonicalLanguageShell, wantSupport: grammarSupportSupportedNow},
		{name: "yaml alias", ext: ".yml", wantID: canonicalLanguageYAML, wantSupport: grammarSupportSupportedNow},
		{name: "markdown alias", ext: ".markdown", wantID: canonicalLanguageMarkdown, wantSupport: grammarSupportDeferred},
		{name: "makefile pseudo extension", ext: ".mk", wantID: canonicalLanguageMakefile, wantSupport: grammarSupportDeferred},
		{name: "astro extension", ext: ".astro", wantID: canonicalLanguageAstro, wantSupport: grammarSupportDeferred},
		{name: "svelte extension", ext: ".svelte", wantID: canonicalLanguageSvelte, wantSupport: grammarSupportSupportedNow},
		{name: "ambiguous c header stays unmapped", ext: ".h", wantSupport: grammarSupportUnmapped},
		{name: "unknown extension", ext: ".unknown", wantSupport: grammarSupportUnmapped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := grammarForExt(tt.ext)

			assert.Equal(t, tt.wantID, bundle.ID)
			assert.Equal(t, tt.wantSupport, bundle.Support)
		})
	}
}

func TestGrammarForEnryLanguage(t *testing.T) {
	tests := []struct {
		name        string
		language    string
		wantID      canonicalLanguageID
		wantSupport grammarSupport
	}{
		{name: "python", language: "Python", wantID: canonicalLanguagePython, wantSupport: grammarSupportSupportedNow},
		{name: "shell", language: "Shell", wantID: canonicalLanguageShell, wantSupport: grammarSupportSupportedNow},
		{name: "tsx", language: "TSX", wantID: canonicalLanguageTSX, wantSupport: grammarSupportSupportedNow},
		{name: "c", language: "C", wantID: canonicalLanguageC, wantSupport: grammarSupportSupportedNow},
		{name: "cpp", language: "C++", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "csharp", language: "C#", wantID: canonicalLanguageCSharp, wantSupport: grammarSupportSupportedNow},
		{name: "dockerfile", language: "Dockerfile", wantID: canonicalLanguageDockerfile, wantSupport: grammarSupportSupportedNow},
		{name: "makefile deferred", language: "Makefile", wantID: canonicalLanguageMakefile, wantSupport: grammarSupportDeferred},
		{name: "hcl remains path only", language: "HCL", wantSupport: grammarSupportUnmapped},
		{name: "unknown", language: "UnknownLanguage", wantSupport: grammarSupportUnmapped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := grammarForEnryLanguage(tt.language)

			assert.Equal(t, tt.wantID, bundle.ID)
			assert.Equal(t, tt.wantSupport, bundle.Support)
		})
	}
}

func TestResolveLanguage_AttachesGrammarMetadata(t *testing.T) {
	goResolved := resolveLanguage("main.go")
	require.NotNil(t, goResolved)
	assert.Equal(t, canonicalLanguageGo, goResolved.grammar.ID)
	assert.Equal(t, grammarSupportSupportedNow, goResolved.grammar.Support)

	dockerResolved := resolveLanguage("Dockerfile")
	require.NotNil(t, dockerResolved)
	assert.Equal(t, canonicalLanguageDockerfile, dockerResolved.grammar.ID)
	assert.Equal(t, grammarSupportSupportedNow, dockerResolved.grammar.Support)

	makeResolved := resolveLanguage("Makefile")
	require.NotNil(t, makeResolved)
	assert.Equal(t, canonicalLanguageMakefile, makeResolved.grammar.ID)
	assert.Equal(t, grammarSupportDeferred, makeResolved.grammar.Support)

	gitattributesResolved := resolveLanguage(".gitattributes")
	require.NotNil(t, gitattributesResolved)
	assert.Equal(t, canonicalLanguageGitAttributes, gitattributesResolved.grammar.ID)
	assert.Equal(t, grammarSupportSupportedNow, gitattributesResolved.grammar.Support)

	goSumResolved := resolveLanguage("go.sum")
	require.NotNil(t, goSumResolved)
	assert.Equal(t, canonicalLanguageGoSum, goSumResolved.grammar.ID)
	assert.Equal(t, grammarSupportSupportedNow, goSumResolved.grammar.Support)

	requirementsResolved := resolveLanguage("requirements.txt")
	require.NotNil(t, requirementsResolved)
	assert.Equal(t, canonicalLanguageRequirements, requirementsResolved.grammar.ID)
	assert.Equal(t, grammarSupportSupportedNow, requirementsResolved.grammar.Support)
}

func TestExtendDeclarationKindSet_InitializesNilBase(t *testing.T) {
	assert.Equal(t, map[string]bool{"alpha": true, "beta": true}, extendDeclarationKindSet(nil, "alpha", "beta"))
}

func TestBuildLanguageIndex_SkipsEmptyAliases(t *testing.T) {
	const testID canonicalLanguageID = "test-empty-alias"

	original, existed := grammarRegistry[testID]
	grammarRegistry[testID] = grammarBundle{CanonicalName: "  ", NameAliases: []string{" Test Alias "}}

	t.Cleanup(func() {
		if existed {
			grammarRegistry[testID] = original
		} else {
			delete(grammarRegistry, testID)
		}
	})

	index := buildLanguageIndex(func(bundle grammarBundle) []string {
		if bundle.CanonicalName == "  " {
			return append([]string{""}, bundle.NameAliases...)
		}

		return nil
	})

	assert.NotContains(t, index, "")
	assert.Equal(t, testID, index["test alias"])
}

func TestFinalizeGrammarBundle_DefaultBranches(t *testing.T) {
	t.Run("nil bundle becomes unmapped", func(t *testing.T) {
		bundle := finalizeGrammarBundle(nil)
		assert.Equal(t, grammarSupportUnmapped, bundle.Support)
		assert.Equal(t, queryAvailabilityUnmapped, bundle.Queries.Tags.Availability)
	})

	t.Run("unmapped fills query availability", func(t *testing.T) {
		bundle := finalizeGrammarBundle(&grammarBundle{Support: grammarSupportUnmapped})
		assert.Equal(t, queryAvailabilityUnmapped, bundle.Queries.Tags.Availability)
	})

	t.Run("supported now fills defaults", func(t *testing.T) {
		bundle := finalizeGrammarBundle(&grammarBundle{Support: grammarSupportSupportedNow})
		assert.Equal(t, treeSitterRuntimeImportPath, bundle.Parser.RuntimeImportPath)
		assert.Equal(t, grammarQueriesDir, bundle.Queries.BaseDir)
		assert.Equal(t, grammarQueriesDir+"/tags.scm", bundle.Queries.Tags.RelativePath)
		assert.Equal(t, queryAvailabilityAbsent, bundle.Queries.Tags.Availability)
	})

	t.Run("deferred fills deferred query availability", func(t *testing.T) {
		bundle := finalizeGrammarBundle(&grammarBundle{Support: grammarSupportDeferred})
		assert.Equal(t, queryAvailabilityDeferred, bundle.Queries.Tags.Availability)
	})
}

func TestGrammarRegistry_LoadLanguageFunctions(t *testing.T) {
	for _, canonical := range []string{"gitattributes", "go-sum", "kotlin", "luau", "requirements", "dtd", "json", "svelte"} {
		t.Run(canonical, func(t *testing.T) {
			bundle := grammarForCanonicalName(canonical)
			require.NotNil(t, bundle.Parser.LoadLanguage)
			assert.NotNil(t, bundle.Parser.LoadLanguage())
		})
	}
}
