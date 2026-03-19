package splitter

import (
	"errors"
	"fmt"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	errASTGrammarUnavailable = errors.New("AST grammar unavailable")
	errNilParseTree          = errors.New("parse tree: nil tree")
)

func parseTree(source []byte, resolved *resolvedLanguage) (*sitter.Tree, error) {
	if resolved == nil || resolved.grammar.Support != grammarSupportSupportedNow {
		return nil, errASTGrammarUnavailable
	}

	loadLanguage := resolved.grammar.Parser.LoadLanguage
	if loadLanguage == nil {
		return nil, fmt.Errorf("%w: %q", errASTGrammarUnavailable, resolved.grammar.ID)
	}

	language := loadLanguage()
	if language == nil || language.Inner == nil {
		return nil, fmt.Errorf("%w: %q", errASTGrammarUnavailable, resolved.grammar.ID)
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(language); err != nil { // Defensive: bundled grammars are version-locked to the runtime.
		return nil, fmt.Errorf("set split language: %w", err)
	}

	tree := parser.Parse(source, nil)
	if tree == nil { // Defensive: Parse should return a tree for a configured parser.
		return nil, errNilParseTree
	}

	return tree, nil
}

func nodeKind(node *sitter.Node) string {
	if node == nil {
		return ""
	}

	if kind := node.Kind(); kind != "" {
		return kind
	}

	return node.GrammarName()
}
