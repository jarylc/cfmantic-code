package splitter

import (
	"maps"
	"strings"

	arduino "github.com/tree-sitter-grammars/tree-sitter-arduino/bindings/go"
	gitattributes "github.com/tree-sitter-grammars/tree-sitter-gitattributes/bindings/go"
	gosum "github.com/tree-sitter-grammars/tree-sitter-go-sum/bindings/go"
	hcl "github.com/tree-sitter-grammars/tree-sitter-hcl/bindings/go"
	kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	lua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	luau "github.com/tree-sitter-grammars/tree-sitter-luau/bindings/go"
	requirements "github.com/tree-sitter-grammars/tree-sitter-requirements/bindings/go"
	svelte "github.com/tree-sitter-grammars/tree-sitter-svelte/bindings/go"
	toml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"
	xml "github.com/tree-sitter-grammars/tree-sitter-xml/bindings/go"
	yaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"
	bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	cgrammar "github.com/tree-sitter/tree-sitter-c/bindings/go"
	cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	css "github.com/tree-sitter/tree-sitter-css/bindings/go"
	gogrammar "github.com/tree-sitter/tree-sitter-go/bindings/go"
	html "github.com/tree-sitter/tree-sitter-html/bindings/go"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	jsonlang "github.com/tree-sitter/tree-sitter-json/bindings/go"
	ocaml "github.com/tree-sitter/tree-sitter-ocaml/bindings/go"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

const (
	treeSitterRuntimeImportPath = "github.com/tree-sitter/go-tree-sitter"
	grammarQueriesDir           = "queries"
)

type canonicalLanguageID string

const (
	canonicalLanguageGo             canonicalLanguageID = "go"
	canonicalLanguageRust           canonicalLanguageID = "rust"
	canonicalLanguageC              canonicalLanguageID = "c"
	canonicalLanguageCSharp         canonicalLanguageID = "csharp"
	canonicalLanguageCPP            canonicalLanguageID = "cpp"
	canonicalLanguageCSS            canonicalLanguageID = "css"
	canonicalLanguageHTML           canonicalLanguageID = "html"
	canonicalLanguageJava           canonicalLanguageID = "java"
	canonicalLanguageJavaScript     canonicalLanguageID = "javascript"
	canonicalLanguageTypeScript     canonicalLanguageID = "typescript"
	canonicalLanguageTSX            canonicalLanguageID = "tsx"
	canonicalLanguagePHP            canonicalLanguageID = "php"
	canonicalLanguagePython         canonicalLanguageID = "python"
	canonicalLanguageRuby           canonicalLanguageID = "ruby"
	canonicalLanguageScala          canonicalLanguageID = "scala"
	canonicalLanguageOCaml          canonicalLanguageID = "ocaml"
	canonicalLanguageOCamlInterface canonicalLanguageID = "ocaml-interface"
	canonicalLanguageArduino        canonicalLanguageID = "arduino"
	canonicalLanguageGitAttributes  canonicalLanguageID = "gitattributes"
	canonicalLanguageGoSum          canonicalLanguageID = "go-sum"
	canonicalLanguageHCL            canonicalLanguageID = "hcl"
	canonicalLanguageKotlin         canonicalLanguageID = "kotlin"
	canonicalLanguageLua            canonicalLanguageID = "lua"
	canonicalLanguageLuau           canonicalLanguageID = "luau"
	canonicalLanguageRequirements   canonicalLanguageID = "requirements"
	canonicalLanguageTOML           canonicalLanguageID = "toml"
	canonicalLanguageXML            canonicalLanguageID = "xml"
	canonicalLanguageDTD            canonicalLanguageID = "dtd"
	canonicalLanguageShell          canonicalLanguageID = "shell"
	canonicalLanguageDockerfile     canonicalLanguageID = "dockerfile"
	canonicalLanguageMakefile       canonicalLanguageID = "makefile"
	canonicalLanguageJSON           canonicalLanguageID = "json"
	canonicalLanguageYAML           canonicalLanguageID = "yaml"
	canonicalLanguageMarkdown       canonicalLanguageID = "markdown"
	canonicalLanguageSvelte         canonicalLanguageID = "svelte"
	canonicalLanguageAstro          canonicalLanguageID = "astro"
)

type grammarSupport string

const (
	grammarSupportSupportedNow grammarSupport = "supported_now"
	grammarSupportDeferred     grammarSupport = "deferred"
	grammarSupportUnmapped     grammarSupport = "unmapped"
)

type grammarSource string

const (
	grammarSourceOfficial  grammarSource = "official"
	grammarSourceCommunity grammarSource = "community"
)

type queryAvailability string

const (
	queryAvailabilityAvailable queryAvailability = "available"
	queryAvailabilityAbsent    queryAvailability = "absent"
	queryAvailabilityDeferred  queryAvailability = "deferred"
	queryAvailabilityUnmapped  queryAvailability = "unmapped"
)

type grammarParser struct {
	RuntimeImportPath string
	GrammarImportPath string
	LanguageFunc      string
	RequiresCGo       bool
	LoadLanguage      func() *sitter.Language
}

type grammarQueryAsset struct {
	RelativePath string
	Availability queryAvailability
}

type grammarQueries struct {
	Repository string
	BaseDir    string
	Tags       grammarQueryAsset
}

type grammarBundle struct {
	ID                     canonicalLanguageID
	CanonicalName          string
	PrimaryExt             string
	ExtAliases             []string
	NameAliases            []string
	EnryAliases            []string
	Support                grammarSupport
	Source                 grammarSource
	Parser                 grammarParser
	Queries                grammarQueries
	DeclarationKinds       map[string]bool
	SymbolDeclarationKinds map[string]bool
	Reason                 string
}

func declarationKindSet(kinds ...string) map[string]bool {
	set := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		set[kind] = true
	}

	return set
}

func extendDeclarationKindSet(base map[string]bool, kinds ...string) map[string]bool {
	set := maps.Clone(base)
	if set == nil {
		set = make(map[string]bool, len(kinds))
	}

	for _, kind := range kinds {
		set[kind] = true
	}

	return set
}

var (
	goDeclarationKinds = declarationKindSet(
		"function_declaration",
		"method_declaration",
		"type_declaration",
	)
	cDeclarationKinds = declarationKindSet(
		"function_definition",
		"struct_specifier",
	)
	cppDeclarationKinds = declarationKindSet(
		"function_definition",
		"class_specifier",
		"struct_specifier",
		"namespace_definition",
	)
	csharpDeclarationKinds = declarationKindSet(
		"method_declaration",
		"class_declaration",
		"interface_declaration",
		"struct_declaration",
		"enum_declaration",
		"namespace_declaration",
	)
	cssDeclarationKinds = declarationKindSet(
		"rule_set",
		"at_rule",
		"media_statement",
		"keyframes_statement",
		"import_statement",
	)
	htmlDeclarationKinds = declarationKindSet(
		"element",
		"script_element",
		"style_element",
	)
	javaDeclarationKinds = declarationKindSet(
		"method_declaration",
		"class_declaration",
		"interface_declaration",
		"enum_declaration",
		"constructor_declaration",
	)
	rustDeclarationKinds = declarationKindSet(
		"function_item",
		"impl_item",
		"struct_item",
		"enum_item",
		"trait_item",
		"mod_item",
	)
	rustSymbolDeclarationKinds = extendDeclarationKindSet(
		rustDeclarationKinds,
		"function_signature_item",
		"type_item",
		"union_item",
	)
	javaScriptDeclarationKinds = declarationKindSet(
		"function_declaration",
		"class_declaration",
		"method_definition",
		"export_statement",
		"lexical_declaration",
	)
	typeScriptDeclarationKinds = extendDeclarationKindSet(
		javaScriptDeclarationKinds,
		"interface_declaration",
		"type_alias_declaration",
		"enum_declaration",
	)
	typeScriptSymbolDeclarationKinds = extendDeclarationKindSet(
		typeScriptDeclarationKinds,
		"abstract_class_declaration",
		"abstract_method_signature",
		"internal_module",
		"method_signature",
	)
	phpDeclarationKinds = declarationKindSet(
		"function_definition",
		"class_declaration",
		"method_declaration",
		"interface_declaration",
		"trait_declaration",
	)
	pythonDeclarationKinds = declarationKindSet(
		"function_definition",
		"async_function_definition",
		"class_definition",
		"decorated_definition",
	)
	rubyDeclarationKinds = declarationKindSet(
		"method",
		"singleton_method",
		"class",
		"module",
	)
	scalaDeclarationKinds = declarationKindSet(
		"function_definition",
		"class_definition",
		"object_definition",
		"trait_definition",
	)
	hclDeclarationKinds = declarationKindSet(
		"block",
		"attribute",
	)
	luaDeclarationKinds = declarationKindSet(
		"function_declaration",
		"local_function",
		"function_definition",
	)
	ocamlDeclarationKinds = declarationKindSet(
		"value_definition",
		"type_definition",
		"module_definition",
		"class_definition",
		"external",
	)
	tomlDeclarationKinds = declarationKindSet(
		"table",
		"array_table",
		"pair",
	)
	xmlDeclarationKinds = declarationKindSet(
		"element",
		"self_closing_element",
		"processing_instruction",
	)
	shellDeclarationKinds = declarationKindSet(
		"function_definition",
		"command",
		"if_statement",
		"for_statement",
		"while_statement",
		"case_statement",
	)
	shellSymbolDeclarationKinds = declarationKindSet("function_definition")
)

var grammarRegistry = map[canonicalLanguageID]grammarBundle{
	canonicalLanguageGo: {
		ID:            canonicalLanguageGo,
		CanonicalName: "go",
		PrimaryExt:    ".go",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-go/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(gogrammar.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-go",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds:       goDeclarationKinds,
		SymbolDeclarationKinds: goDeclarationKinds,
	},
	canonicalLanguageC: {
		ID:            canonicalLanguageC,
		CanonicalName: "c",
		PrimaryExt:    ".c",
		EnryAliases:   []string{"c"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-c/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(cgrammar.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-c",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: cDeclarationKinds,
	},
	canonicalLanguageCSharp: {
		ID:            canonicalLanguageCSharp,
		CanonicalName: "csharp",
		PrimaryExt:    ".cs",
		NameAliases:   []string{"c#"},
		EnryAliases:   []string{"c#"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(csharp.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-c-sharp",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: csharpDeclarationKinds,
	},
	canonicalLanguageCPP: {
		ID:            canonicalLanguageCPP,
		CanonicalName: "cpp",
		PrimaryExt:    ".cpp",
		ExtAliases:    []string{".cc", ".cxx", ".hpp", ".hx"},
		NameAliases:   []string{"c++"},
		EnryAliases:   []string{"c++"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-cpp/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(cpp.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-cpp",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: cppDeclarationKinds,
	},
	canonicalLanguageCSS: {
		ID:            canonicalLanguageCSS,
		CanonicalName: "css",
		PrimaryExt:    ".css",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-css/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(css.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-css",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: cssDeclarationKinds,
	},
	canonicalLanguageHTML: {
		ID:            canonicalLanguageHTML,
		CanonicalName: "html",
		PrimaryExt:    ".html",
		ExtAliases:    []string{".htm"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-html/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(html.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-html",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: htmlDeclarationKinds,
	},
	canonicalLanguageJava: {
		ID:            canonicalLanguageJava,
		CanonicalName: "java",
		PrimaryExt:    ".java",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-java/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(java.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-java",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: javaDeclarationKinds,
	},
	canonicalLanguageRust: {
		ID:            canonicalLanguageRust,
		CanonicalName: "rust",
		PrimaryExt:    ".rs",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-rust/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(rust.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-rust",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds:       rustDeclarationKinds,
		SymbolDeclarationKinds: rustSymbolDeclarationKinds,
	},
	canonicalLanguageJavaScript: {
		ID:            canonicalLanguageJavaScript,
		CanonicalName: "javascript",
		PrimaryExt:    ".js",
		ExtAliases:    []string{".jsx"},
		NameAliases:   []string{"js"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-javascript/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(javascript.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-javascript",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds:       javaScriptDeclarationKinds,
		SymbolDeclarationKinds: javaScriptDeclarationKinds,
	},
	canonicalLanguageTypeScript: {
		ID:            canonicalLanguageTypeScript,
		CanonicalName: "typescript",
		PrimaryExt:    ".ts",
		NameAliases:   []string{"ts"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-typescript/bindings/go",
			LanguageFunc:      "LanguageTypescript",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(typescript.LanguageTypescript())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-typescript",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds:       typeScriptDeclarationKinds,
		SymbolDeclarationKinds: typeScriptSymbolDeclarationKinds,
	},
	canonicalLanguageTSX: {
		ID:            canonicalLanguageTSX,
		CanonicalName: "tsx",
		PrimaryExt:    ".tsx",
		EnryAliases:   []string{"tsx"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-typescript/bindings/go",
			LanguageFunc:      "LanguageTSX",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(typescript.LanguageTSX())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-typescript",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds:       typeScriptDeclarationKinds,
		SymbolDeclarationKinds: typeScriptSymbolDeclarationKinds,
	},
	canonicalLanguagePython: {
		ID:            canonicalLanguagePython,
		CanonicalName: "python",
		PrimaryExt:    ".py",
		NameAliases:   []string{"py"},
		EnryAliases:   []string{"python"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-python/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(python.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-python",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds:       pythonDeclarationKinds,
		SymbolDeclarationKinds: pythonDeclarationKinds,
	},
	canonicalLanguagePHP: {
		ID:            canonicalLanguagePHP,
		CanonicalName: "php",
		PrimaryExt:    ".php",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-php/bindings/go",
			LanguageFunc:      "LanguagePHP",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(php.LanguagePHP())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-php",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: phpDeclarationKinds,
	},
	canonicalLanguageRuby: {
		ID:            canonicalLanguageRuby,
		CanonicalName: "ruby",
		PrimaryExt:    ".rb",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-ruby/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(ruby.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-ruby",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: rubyDeclarationKinds,
	},
	canonicalLanguageScala: {
		ID:            canonicalLanguageScala,
		CanonicalName: "scala",
		PrimaryExt:    ".scala",
		ExtAliases:    []string{".sbt"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-scala/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(scala.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-scala",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: scalaDeclarationKinds,
	},
	canonicalLanguageOCaml: {
		ID:            canonicalLanguageOCaml,
		CanonicalName: "ocaml",
		PrimaryExt:    ".ml",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-ocaml/bindings/go",
			LanguageFunc:      "LanguageOCaml",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(ocaml.LanguageOCaml())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-ocaml",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: ocamlDeclarationKinds,
	},
	canonicalLanguageOCamlInterface: {
		ID:            canonicalLanguageOCamlInterface,
		CanonicalName: "ocaml-interface",
		PrimaryExt:    ".mli",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-ocaml/bindings/go",
			LanguageFunc:      "LanguageOCamlInterface",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(ocaml.LanguageOCamlInterface())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-ocaml",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: ocamlDeclarationKinds,
	},
	canonicalLanguageArduino: {
		ID:            canonicalLanguageArduino,
		CanonicalName: "arduino",
		PrimaryExt:    ".ino",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-arduino/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(arduino.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-arduino",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: cppDeclarationKinds,
	},
	canonicalLanguageGitAttributes: {
		ID:            canonicalLanguageGitAttributes,
		CanonicalName: "gitattributes",
		PrimaryExt:    ".gitattributes",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-gitattributes/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(gitattributes.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-gitattributes",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
	},
	canonicalLanguageGoSum: {
		ID:            canonicalLanguageGoSum,
		CanonicalName: "go-sum",
		PrimaryExt:    ".go.sum",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-go-sum/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(gosum.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-go-sum",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
	},
	canonicalLanguageHCL: {
		ID:            canonicalLanguageHCL,
		CanonicalName: "hcl",
		PrimaryExt:    ".hcl",
		ExtAliases:    []string{".tf", ".tfvars", ".tofu"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-hcl/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(hcl.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-hcl",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: hclDeclarationKinds,
	},
	canonicalLanguageKotlin: {
		ID:            canonicalLanguageKotlin,
		CanonicalName: "kotlin",
		PrimaryExt:    ".kt",
		ExtAliases:    []string{".kts"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(kotlin.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-kotlin",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
	},
	canonicalLanguageLua: {
		ID:            canonicalLanguageLua,
		CanonicalName: "lua",
		PrimaryExt:    ".lua",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(lua.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-lua",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAvailable,
			},
		},
		DeclarationKinds: luaDeclarationKinds,
	},
	canonicalLanguageLuau: {
		ID:            canonicalLanguageLuau,
		CanonicalName: "luau",
		PrimaryExt:    ".luau",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-luau/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(luau.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-luau",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
	},
	canonicalLanguageRequirements: {
		ID:            canonicalLanguageRequirements,
		CanonicalName: "requirements",
		PrimaryExt:    ".requirements.txt",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-requirements/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(requirements.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-requirements",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
	},
	canonicalLanguageTOML: {
		ID:            canonicalLanguageTOML,
		CanonicalName: "toml",
		PrimaryExt:    ".toml",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(toml.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-toml",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: tomlDeclarationKinds,
	},
	canonicalLanguageXML: {
		ID:            canonicalLanguageXML,
		CanonicalName: "xml",
		PrimaryExt:    ".xml",
		ExtAliases:    []string{".svg", ".xsd", ".xslt", ".xsl", ".rng"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-xml/bindings/go",
			LanguageFunc:      "LanguageXML",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(xml.LanguageXML())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-xml",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: xmlDeclarationKinds,
	},
	canonicalLanguageDTD: {
		ID:            canonicalLanguageDTD,
		CanonicalName: "dtd",
		PrimaryExt:    ".dtd",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-xml/bindings/go",
			LanguageFunc:      "LanguageDTD",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(xml.LanguageDTD())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-xml",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
	},
	canonicalLanguageShell: {
		ID:            canonicalLanguageShell,
		CanonicalName: "shell",
		PrimaryExt:    ".sh",
		ExtAliases:    []string{".bash"},
		NameAliases:   []string{"bash"},
		EnryAliases:   []string{"shell", "bash"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-bash/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(bash.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-bash",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds:       shellDeclarationKinds,
		SymbolDeclarationKinds: shellSymbolDeclarationKinds,
	},
	canonicalLanguageDockerfile: {
		ID:            canonicalLanguageDockerfile,
		CanonicalName: "dockerfile",
		PrimaryExt:    ".dockerfile",
		EnryAliases:   []string{"dockerfile"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/camdencheek/tree-sitter-dockerfile",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
		},
		Queries: grammarQueries{
			Repository: "camdencheek/tree-sitter-dockerfile",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: map[string]bool{
			"from_instruction":       true,
			"run_instruction":        true,
			"cmd_instruction":        true,
			"entrypoint_instruction": true,
			"copy_instruction":       true,
			"add_instruction":        true,
			"env_instruction":        true,
			"arg_instruction":        true,
		},
	},
	canonicalLanguageMakefile: {
		ID:            canonicalLanguageMakefile,
		CanonicalName: "makefile",
		PrimaryExt:    ".mk",
		EnryAliases:   []string{"makefile"},
		Support:       grammarSupportDeferred,
		Source:        grammarSourceCommunity,
		Queries: grammarQueries{
			Repository: "alemuller/tree-sitter-make",
		},
		DeclarationKinds: map[string]bool{
			"rule":                true,
			"variable_assignment": true,
			"include_directive":   true,
		},
		Reason: "deferred: no adopted go-tree-sitter Makefile binding package yet",
	},
	canonicalLanguageJSON: {
		ID:            canonicalLanguageJSON,
		CanonicalName: "json",
		PrimaryExt:    ".json",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceOfficial,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter/tree-sitter-json/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(jsonlang.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter/tree-sitter-json",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: map[string]bool{
			"pair":   true,
			"object": true,
			"array":  true,
		},
	},
	canonicalLanguageYAML: {
		ID:            canonicalLanguageYAML,
		CanonicalName: "yaml",
		PrimaryExt:    ".yaml",
		ExtAliases:    []string{".yml"},
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(yaml.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-yaml",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: map[string]bool{
			"block_mapping_pair":  true,
			"block_sequence_item": true,
			"document":            true,
		},
	},
	canonicalLanguageMarkdown: {
		ID:            canonicalLanguageMarkdown,
		CanonicalName: "markdown",
		PrimaryExt:    ".md",
		ExtAliases:    []string{".markdown"},
		NameAliases:   []string{"md"},
		Support:       grammarSupportDeferred,
		Source:        grammarSourceCommunity,
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-markdown",
		},
		Reason: "deferred: no adopted go-tree-sitter Markdown binding package yet",
	},
	canonicalLanguageSvelte: {
		ID:            canonicalLanguageSvelte,
		CanonicalName: "svelte",
		PrimaryExt:    ".svelte",
		Support:       grammarSupportSupportedNow,
		Source:        grammarSourceCommunity,
		Parser: grammarParser{
			GrammarImportPath: "github.com/tree-sitter-grammars/tree-sitter-svelte/bindings/go",
			LanguageFunc:      "Language",
			RequiresCGo:       true,
			LoadLanguage: func() *sitter.Language {
				return sitter.NewLanguage(svelte.Language())
			},
		},
		Queries: grammarQueries{
			Repository: "tree-sitter-grammars/tree-sitter-svelte",
			Tags: grammarQueryAsset{
				Availability: queryAvailabilityAbsent,
			},
		},
		DeclarationKinds: map[string]bool{
			"element":        true,
			"script_element": true,
			"style_element":  true,
			"if_statement":   true,
			"each_statement": true,
		},
	},
	canonicalLanguageAstro: {
		ID:            canonicalLanguageAstro,
		CanonicalName: "astro",
		PrimaryExt:    ".astro",
		Support:       grammarSupportDeferred,
		Source:        grammarSourceCommunity,
		Queries: grammarQueries{
			Repository: "virchau13/tree-sitter-astro",
		},
		DeclarationKinds: map[string]bool{
			"frontmatter": true,
			"element":     true,
			"expression":  true,
			"fragment":    true,
		},
		Reason: "deferred: no adopted go-tree-sitter Astro binding package yet",
	},
}

var canonicalLanguageByExt = buildLanguageIndex(func(bundle grammarBundle) []string {
	return append([]string{bundle.PrimaryExt}, bundle.ExtAliases...)
})

var canonicalLanguageByName = buildLanguageIndex(func(bundle grammarBundle) []string {
	return append([]string{bundle.CanonicalName}, bundle.NameAliases...)
})

var canonicalLanguageByEnryName = buildLanguageIndex(func(bundle grammarBundle) []string {
	return bundle.EnryAliases
})

func buildLanguageIndex(aliases func(grammarBundle) []string) map[string]canonicalLanguageID {
	index := make(map[string]canonicalLanguageID, len(grammarRegistry))
	for id := range grammarRegistry {
		bundle := grammarRegistry[id]
		for _, alias := range aliases(bundle) {
			alias = normalizeGrammarAlias(alias)
			if alias == "" {
				continue
			}

			index[alias] = id
		}
	}

	return index
}

func grammarForCanonicalName(name string) grammarBundle {
	name = normalizeGrammarAlias(name)

	id, ok := canonicalLanguageByName[name]
	if !ok {
		return unmappedGrammarBundle(canonicalLanguageID(name))
	}

	bundle := grammarRegistry[id]

	return finalizeGrammarBundle(&bundle)
}

func grammarForExt(ext string) grammarBundle {
	id, ok := canonicalLanguageByExt[normalizeGrammarAlias(ext)]
	if !ok {
		return unmappedGrammarBundle("")
	}

	bundle := grammarRegistry[id]

	return finalizeGrammarBundle(&bundle)
}

func grammarForEnryLanguage(language string) grammarBundle {
	id, ok := canonicalLanguageByEnryName[normalizeGrammarAlias(language)]
	if !ok {
		return unmappedGrammarBundle("")
	}

	bundle := grammarRegistry[id]

	return finalizeGrammarBundle(&bundle)
}

func finalizeGrammarBundle(bundle *grammarBundle) grammarBundle {
	if bundle == nil {
		return unmappedGrammarBundle("")
	}

	bundle.DeclarationKinds = maps.Clone(bundle.DeclarationKinds)
	bundle.SymbolDeclarationKinds = maps.Clone(bundle.SymbolDeclarationKinds)

	if bundle.Support == grammarSupportUnmapped {
		if bundle.Queries.Tags.Availability == "" {
			bundle.Queries.Tags.Availability = queryAvailabilityUnmapped
		}

		return *bundle
	}

	if bundle.Queries.BaseDir == "" {
		bundle.Queries.BaseDir = grammarQueriesDir
	}

	switch bundle.Support {
	case grammarSupportSupportedNow:
		if bundle.Parser.RuntimeImportPath == "" {
			bundle.Parser.RuntimeImportPath = treeSitterRuntimeImportPath
		}

		if bundle.Queries.Tags.RelativePath == "" {
			bundle.Queries.Tags.RelativePath = grammarQueriesDir + "/tags.scm"
		}

		if bundle.Queries.Tags.Availability == "" {
			bundle.Queries.Tags.Availability = queryAvailabilityAbsent
		}
	case grammarSupportDeferred:
		if bundle.Queries.Tags.Availability == "" {
			bundle.Queries.Tags.Availability = queryAvailabilityDeferred
		}
	default:
		// bundle.Support == grammarSupportUnmapped already returned above.
	}

	return *bundle
}

func unmappedGrammarBundle(id canonicalLanguageID) grammarBundle {
	return grammarBundle{
		ID:      id,
		Support: grammarSupportUnmapped,
		Queries: grammarQueries{
			Tags: grammarQueryAsset{Availability: queryAvailabilityUnmapped},
		},
	}
}

func normalizeGrammarAlias(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
