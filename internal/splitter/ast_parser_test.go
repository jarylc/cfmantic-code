package splitter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestParseTree_RequiresSupportedGrammar(t *testing.T) {
	tests := []struct {
		name     string
		resolved *resolvedLanguage
	}{
		{name: "nil language", resolved: nil},
		{name: "deferred language", resolved: resolveLanguageFromExt(".md")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree, err := parseTree([]byte("content"), tt.resolved)
			require.ErrorIs(t, err, errASTGrammarUnavailable)
			assert.Nil(t, tree)
		})
	}
}

func TestParseTree_RequiresLoadedLanguage(t *testing.T) {
	resolved := &resolvedLanguage{
		ext: ".fake",
		grammar: grammarBundle{
			ID:      canonicalLanguageID("fake"),
			Support: grammarSupportSupportedNow,
		},
	}

	tree, err := parseTree([]byte("content"), resolved)
	require.Error(t, err)
	require.ErrorIs(t, err, errASTGrammarUnavailable)
	assert.Nil(t, tree)
}

func TestParseTree_RejectsLanguageWithoutInner(t *testing.T) {
	resolved := &resolvedLanguage{
		ext: ".fake",
		grammar: grammarBundle{
			ID:      canonicalLanguageID("fake"),
			Support: grammarSupportSupportedNow,
			Parser: grammarParser{
				LoadLanguage: func() *sitter.Language {
					return &sitter.Language{}
				},
			},
		},
	}

	tree, err := parseTree([]byte("content"), resolved)
	require.ErrorIs(t, err, errASTGrammarUnavailable)
	assert.Nil(t, tree)
}

func TestParseTree_ParsesSupportedLanguageAndNodeKindHandlesNil(t *testing.T) {
	resolved := resolveLanguageFromExt(".go")
	require.NotNil(t, resolved)

	tree, err := parseTree([]byte("package main\nfunc main() {}\n"), resolved)
	require.NoError(t, err)
	require.NotNil(t, tree)
	t.Cleanup(func() { tree.Close() })

	root := tree.RootNode()
	require.NotNil(t, root)
	assert.NotEmpty(t, nodeKind(root))
	assert.Empty(t, nodeKind(nil))
}
