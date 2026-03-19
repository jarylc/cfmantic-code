package splitter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveLanguageFromExt_OfficialAndDeferredExtensions(t *testing.T) {
	tests := []struct {
		name        string
		ext         string
		wantID      canonicalLanguageID
		wantSupport grammarSupport
	}{
		{name: "go", ext: ".go", wantID: canonicalLanguageGo, wantSupport: grammarSupportSupportedNow},
		{name: "rust", ext: ".rs", wantID: canonicalLanguageRust, wantSupport: grammarSupportSupportedNow},
		{name: "javascript", ext: ".js", wantID: canonicalLanguageJavaScript, wantSupport: grammarSupportSupportedNow},
		{name: "jsx alias", ext: ".jsx", wantID: canonicalLanguageJavaScript, wantSupport: grammarSupportSupportedNow},
		{name: "typescript", ext: ".ts", wantID: canonicalLanguageTypeScript, wantSupport: grammarSupportSupportedNow},
		{name: "tsx", ext: ".tsx", wantID: canonicalLanguageTSX, wantSupport: grammarSupportSupportedNow},
		{name: "python", ext: ".py", wantID: canonicalLanguagePython, wantSupport: grammarSupportSupportedNow},
		{name: "c", ext: ".c", wantID: canonicalLanguageC, wantSupport: grammarSupportSupportedNow},
		{name: "csharp", ext: ".cs", wantID: canonicalLanguageCSharp, wantSupport: grammarSupportSupportedNow},
		{name: "cpp", ext: ".cpp", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "cpp alias .cc", ext: ".cc", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "cpp alias .cxx", ext: ".cxx", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "cpp alias .hpp", ext: ".hpp", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "cpp alias .hx", ext: ".hx", wantID: canonicalLanguageCPP, wantSupport: grammarSupportSupportedNow},
		{name: "css", ext: ".css", wantID: canonicalLanguageCSS, wantSupport: grammarSupportSupportedNow},
		{name: "html", ext: ".html", wantID: canonicalLanguageHTML, wantSupport: grammarSupportSupportedNow},
		{name: "html alias", ext: ".htm", wantID: canonicalLanguageHTML, wantSupport: grammarSupportSupportedNow},
		{name: "java", ext: ".java", wantID: canonicalLanguageJava, wantSupport: grammarSupportSupportedNow},
		{name: "php", ext: ".php", wantID: canonicalLanguagePHP, wantSupport: grammarSupportSupportedNow},
		{name: "ruby", ext: ".rb", wantID: canonicalLanguageRuby, wantSupport: grammarSupportSupportedNow},
		{name: "scala", ext: ".scala", wantID: canonicalLanguageScala, wantSupport: grammarSupportSupportedNow},
		{name: "scala alias", ext: ".sbt", wantID: canonicalLanguageScala, wantSupport: grammarSupportSupportedNow},
		{name: "arduino", ext: ".ino", wantID: canonicalLanguageArduino, wantSupport: grammarSupportSupportedNow},
		{name: "hcl", ext: ".hcl", wantID: canonicalLanguageHCL, wantSupport: grammarSupportSupportedNow},
		{name: "terraform alias .tf", ext: ".tf", wantID: canonicalLanguageHCL, wantSupport: grammarSupportSupportedNow},
		{name: "terraform vars alias", ext: ".tfvars", wantID: canonicalLanguageHCL, wantSupport: grammarSupportSupportedNow},
		{name: "opentofu alias", ext: ".tofu", wantID: canonicalLanguageHCL, wantSupport: grammarSupportSupportedNow},
		{name: "kotlin", ext: ".kt", wantID: canonicalLanguageKotlin, wantSupport: grammarSupportSupportedNow},
		{name: "kotlin alias", ext: ".kts", wantID: canonicalLanguageKotlin, wantSupport: grammarSupportSupportedNow},
		{name: "lua", ext: ".lua", wantID: canonicalLanguageLua, wantSupport: grammarSupportSupportedNow},
		{name: "luau", ext: ".luau", wantID: canonicalLanguageLuau, wantSupport: grammarSupportSupportedNow},
		{name: "shell", ext: ".sh", wantID: canonicalLanguageShell, wantSupport: grammarSupportSupportedNow},
		{name: "bash alias", ext: ".bash", wantID: canonicalLanguageShell, wantSupport: grammarSupportSupportedNow},
		{name: "dockerfile", ext: ".dockerfile", wantID: canonicalLanguageDockerfile, wantSupport: grammarSupportSupportedNow},
		{name: "makefile", ext: ".mk", wantID: canonicalLanguageMakefile, wantSupport: grammarSupportDeferred},
		{name: "json", ext: ".json", wantID: canonicalLanguageJSON, wantSupport: grammarSupportSupportedNow},
		{name: "toml", ext: ".toml", wantID: canonicalLanguageTOML, wantSupport: grammarSupportSupportedNow},
		{name: "yaml", ext: ".yaml", wantID: canonicalLanguageYAML, wantSupport: grammarSupportSupportedNow},
		{name: "yaml alias", ext: ".yml", wantID: canonicalLanguageYAML, wantSupport: grammarSupportSupportedNow},
		{name: "git attributes pseudo ext", ext: ".gitattributes", wantID: canonicalLanguageGitAttributes, wantSupport: grammarSupportSupportedNow},
		{name: "go sum pseudo ext", ext: ".go.sum", wantID: canonicalLanguageGoSum, wantSupport: grammarSupportSupportedNow},
		{name: "requirements pseudo ext", ext: ".requirements.txt", wantID: canonicalLanguageRequirements, wantSupport: grammarSupportSupportedNow},
		{name: "xml", ext: ".xml", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .svg", ext: ".svg", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .xsd", ext: ".xsd", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .xslt", ext: ".xslt", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .xsl", ext: ".xsl", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "xml alias .rng", ext: ".rng", wantID: canonicalLanguageXML, wantSupport: grammarSupportSupportedNow},
		{name: "dtd", ext: ".dtd", wantID: canonicalLanguageDTD, wantSupport: grammarSupportSupportedNow},
		{name: "ocaml", ext: ".ml", wantID: canonicalLanguageOCaml, wantSupport: grammarSupportSupportedNow},
		{name: "ocaml interface", ext: ".mli", wantID: canonicalLanguageOCamlInterface, wantSupport: grammarSupportSupportedNow},
		{name: "markdown", ext: ".md", wantID: canonicalLanguageMarkdown, wantSupport: grammarSupportDeferred},
		{name: "markdown alias", ext: ".markdown", wantID: canonicalLanguageMarkdown, wantSupport: grammarSupportDeferred},
		{name: "svelte", ext: ".svelte", wantID: canonicalLanguageSvelte, wantSupport: grammarSupportSupportedNow},
		{name: "astro", ext: ".astro", wantID: canonicalLanguageAstro, wantSupport: grammarSupportDeferred},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := resolveLanguageFromExt(tt.ext)
			require.NotNil(t, resolved)
			assert.Equal(t, tt.ext, resolved.ext)
			assert.Equal(t, tt.wantID, resolved.grammar.ID)
			assert.Equal(t, tt.wantSupport, resolved.grammar.Support)
		})
	}
}

func TestResolveLanguageFromExt_UnsupportedExtension(t *testing.T) {
	tests := []string{".unknown", "", ".xyz", ".foo", ".h", ".hbs", ".handlebars", ".csv", ".psv", ".tsv", ".inc", ".php5", ".rbw", ".sc", ".gradle", ".ktm", ".nomad", ".workflow", ".xml.dist", ".tftpl", ".angular", ".erl"}
	for _, ext := range tests {
		t.Run(ext, func(t *testing.T) {
			assert.Nil(t, resolveLanguageFromExt(ext))
		})
	}
}

func TestResolveLanguage_SpecialFilenamesAndAliases(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		wantExt  string
		wantID   canonicalLanguageID
	}{
		{name: "Makefile", filePath: "Makefile", wantExt: ".mk", wantID: canonicalLanguageMakefile},
		{name: "GNUmakefile", filePath: "GNUmakefile", wantExt: ".mk", wantID: canonicalLanguageMakefile},
		{name: "Dockerfile", filePath: "Dockerfile", wantExt: ".dockerfile", wantID: canonicalLanguageDockerfile},
		{name: "Git attributes", filePath: ".gitattributes", wantExt: ".gitattributes", wantID: canonicalLanguageGitAttributes},
		{name: "Go sum", filePath: "go.sum", wantExt: ".go.sum", wantID: canonicalLanguageGoSum},
		{name: "requirements", filePath: "requirements.txt", wantExt: ".requirements.txt", wantID: canonicalLanguageRequirements},
		{name: "jsx alias", filePath: "component.jsx", wantExt: ".jsx", wantID: canonicalLanguageJavaScript},
		{name: "bash alias", filePath: "build.bash", wantExt: ".bash", wantID: canonicalLanguageShell},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := resolveLanguage(tt.filePath)
			require.NotNil(t, resolved)
			assert.Equal(t, tt.wantExt, resolved.ext)
			assert.Equal(t, tt.wantID, resolved.grammar.ID)
		})
	}

	t.Run("unsupported handlebars falls back", func(t *testing.T) {
		assert.Nil(t, resolveLanguage("template.hbs"))
		assert.Nil(t, resolveLanguage("template.handlebars"))
		assert.Nil(t, resolveLanguage("dev-requirements.txt"))
	})
}

func TestResolveLanguageForContent_ContentSensitiveFallbacks(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		content  string
		wantExt  string
		wantID   canonicalLanguageID
	}{
		{
			name:     "extensionless shebang shell",
			filePath: "script",
			content:  "#!/usr/bin/env bash\nsetup() {\n  echo hi\n}\n",
			wantExt:  ".sh",
			wantID:   canonicalLanguageShell,
		},
		{
			name:     "unknown extension modeline python",
			filePath: "notes.txt",
			content:  "# -*- mode: python -*-\ndef hello():\n    return 1\n",
			wantExt:  ".py",
			wantID:   canonicalLanguagePython,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := resolveLanguageForContent(tt.filePath, []byte(tt.content))
			require.NotNil(t, resolved)
			assert.Equal(t, tt.wantExt, resolved.ext)
			assert.Equal(t, tt.wantID, resolved.grammar.ID)
		})
	}
}

func TestSupportedExtForEnryLanguage_StandaloneAndAmbiguousCases(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		language string
		wantExt  string
	}{
		{name: "python fallback from modeline language", filePath: "notes.txt", language: "Python", wantExt: ".py"},
		{name: "c header promotes to c", filePath: "types.h", language: "C", wantExt: ".c"},
		{name: "cpp header promotes to cpp", filePath: "types.h", language: "C++", wantExt: ".cpp"},
		{name: "arduino keeps ino path ext even when enry reports cpp", filePath: "sketch.ino", language: "C++", wantExt: ".ino"},
		{name: "html alias preserves supported path ext", filePath: "index.htm", language: "HTML", wantExt: ".htm"},
		{name: "php supported path ext stays php", filePath: "index.php", language: "PHP", wantExt: ".php"},
		{name: "hcl terraform alias preserves tf", filePath: "main.tf", language: "HCL", wantExt: ".tf"},
		{name: "hcl opentofu alias preserves tofu", filePath: "stack.tofu", language: "HCL", wantExt: ".tofu"},
		{name: "kotlin alias preserves kts", filePath: "build.gradle.kts", language: "Kotlin", wantExt: ".kts"},
		{name: "svg preserves supported path ext", filePath: "icon.svg", language: "SVG", wantExt: ".svg"},
		{name: "xslt preserves supported path ext", filePath: "transform.xsl", language: "XSLT", wantExt: ".xsl"},
		{name: "php unsupported standalone alias stays disabled", filePath: "include.inc", language: "PHP", wantExt: ""},
		{name: "requirements filename stays explicit only", filePath: "dev-requirements.txt", language: "Pip Requirements", wantExt: ""},
		{name: "ruby extensionless stays disabled", filePath: "Rakefile", language: "Ruby", wantExt: ""},
		{name: "scala script extension stays disabled", filePath: "build.sc", language: "Scala", wantExt: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantExt, supportedExtForEnryLanguage(tt.filePath, tt.language))
		})
	}
}

func TestResolveLanguageForContent_PreservesSupportedPathOverrides(t *testing.T) {
	resolved := resolveLanguageForContent("build.bash", []byte("#!/usr/bin/env python\ndef hello():\n    return 1\n"))
	require.NotNil(t, resolved)
	assert.Equal(t, ".bash", resolved.ext)
	assert.Equal(t, canonicalLanguageShell, resolved.grammar.ID)
}

func TestResolveLanguageFromExt_PreservesDeclarationAliases(t *testing.T) {
	tests := []struct {
		name         string
		ext          string
		canonicalExt string
	}{
		{name: "go control", ext: ".go", canonicalExt: ".go"},
		{name: "python control", ext: ".py", canonicalExt: ".py"},
		{name: "jsx alias", ext: ".jsx", canonicalExt: ".js"},
		{name: "cpp alias .cc", ext: ".cc", canonicalExt: ".cpp"},
		{name: "cpp alias .cxx", ext: ".cxx", canonicalExt: ".cpp"},
		{name: "cpp alias .hpp", ext: ".hpp", canonicalExt: ".cpp"},
		{name: "cpp alias .hx", ext: ".hx", canonicalExt: ".cpp"},
		{name: "arduino reuses cpp declarations", ext: ".ino", canonicalExt: ".cpp"},
		{name: "bash alias", ext: ".bash", canonicalExt: ".sh"},
		{name: "html alias", ext: ".htm", canonicalExt: ".html"},
		{name: "scala alias", ext: ".sbt", canonicalExt: ".scala"},
		{name: "terraform alias .tf", ext: ".tf", canonicalExt: ".hcl"},
		{name: "terraform alias .tfvars", ext: ".tfvars", canonicalExt: ".hcl"},
		{name: "opentofu alias", ext: ".tofu", canonicalExt: ".hcl"},
		{name: "xml alias .svg", ext: ".svg", canonicalExt: ".xml"},
		{name: "xml alias .xsd", ext: ".xsd", canonicalExt: ".xml"},
		{name: "xml alias .xslt", ext: ".xslt", canonicalExt: ".xml"},
		{name: "xml alias .xsl", ext: ".xsl", canonicalExt: ".xml"},
		{name: "xml alias .rng", ext: ".rng", canonicalExt: ".xml"},
		{name: "make explicit extension", ext: ".mk", canonicalExt: ".mk"},
		{name: "docker explicit extension", ext: ".dockerfile", canonicalExt: ".dockerfile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, resolveLanguageFromExt(tt.ext), "resolveLanguageFromExt(%q) should resolve", tt.ext)
			assert.Equal(t, declarationTypes(tt.canonicalExt), declarationTypes(tt.ext))
		})
	}
}

func TestEnryLanguageSupportsExt_ReturnsFalseForMissingExtension(t *testing.T) {
	assert.False(t, enryLanguageSupportsExt("Go", ".py"))
}
