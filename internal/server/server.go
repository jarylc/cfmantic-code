package mcpserver

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/handler"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	serverInstructions           = "Semantic code search for local codebases. First call index_codebase on the working directory. Initial indexing and reindexing always start in the background. If async=false is sent for those runs, it is ignored because they may exceed MCP client timeouts; use get_indexing_status for progress. Incremental refreshes can still use async=false to wait for completion. Then call search_code on that indexed working directory or one of its subdirectories. Use clear_index to remove stored index data."
	indexToolDescription         = "Create or refresh a semantic index for a local codebase. Initial indexing and reindexing always start in the background; incremental refreshes can still wait with async=false, or you can poll with get_indexing_status."
	indexAsyncDescription        = "Run asynchronously by default. Ignored for an initial full index or any reindex because those runs may exceed MCP client timeouts; set async=false only to wait for incremental refresh completion."
	searchSymbolsToolDescription = "Search for function, method, class, struct, interface, and type definitions across a local codebase using tree-sitter symbol extraction. No prior indexing required."
	traceSymbolToolDescription   = "Trace lexical occurrences of a symbol (definitions, calls, references) across a local codebase using tree-sitter symbol extraction. This is lexical classification, not a resolved call graph. No prior indexing required."
)

// New creates and returns an MCPServer with all tools registered.
func New(cfg *config.Config, h *handler.Handler) *server.MCPServer {
	s := server.NewMCPServer(cfg.ServerName, cfg.ServerVersion,
		server.WithInstructions(serverInstructions),
		server.WithToolCapabilities(false),
		server.WithLogging(),
		server.WithRecovery(),
	)

	indexTool := mcp.NewTool("index_codebase",
		mcp.WithDescription(indexToolDescription),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to the codebase root to index. Hint: Use your current working directory if unsure.")),
		mcp.WithBoolean("reindex", mcp.Description("Delete existing index data for this codebase path before rebuilding."), mcp.DefaultBool(false)),
		mcp.WithBoolean("async", mcp.Description(indexAsyncDescription), mcp.DefaultBool(true)),
		mcp.WithArray("ignorePatterns", mcp.Description("Extra ignore patterns to apply in addition to .gitignore, .indexignore, and Git exclude rules."), mcp.WithStringItems()),
	)
	s.AddTool(indexTool, h.HandleIndex)

	searchTool := mcp.NewTool("search_code",
		mcp.WithDescription("Run a natural-language semantic search against a previously indexed local codebase."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to an indexed codebase root or subdirectory. Hint: Use your current working directory if unsure.")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural-language description of the code, behavior, or symbols to find.")),
		mcp.WithNumber("limit", mcp.Description("Maximum results to return (recommended default 5, max 20)."), mcp.DefaultNumber(5)),
		mcp.WithBoolean("metadataOnly", mcp.Description("Omit code content and code fences, returning only result headers and metadata (default false)."), mcp.DefaultBool(false)),
		mcp.WithNumber("maxContentLines", mcp.Description("Maximum content lines per result (default 40; 0 disables the line cap)."), mcp.DefaultNumber(40)),
		mcp.WithNumber("maxContentChars", mcp.Description("Maximum content characters per result (default 2000; 0 disables the character cap)."), mcp.DefaultNumber(2000)),
		mcp.WithArray("extensionFilter", mcp.Description("Restrict results to these file extensions (for example '.go', '.ts')."), mcp.WithStringItems()),
	)
	s.AddTool(searchTool, h.HandleSearch)

	searchSymbolsTool := mcp.NewTool("search_symbols",
		mcp.WithDescription(searchSymbolsToolDescription),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to a codebase root or subdirectory. Hint: Use your current working directory if unsure.")),
		mcp.WithString("query", mcp.Description("Substring or glob match on symbol name. If omitted, all symbols are returned.")),
		mcp.WithArray("kinds", mcp.Description("Filter by symbol kinds (e.g. \"function\", \"method\", \"class\", \"struct\", \"interface\", \"type\")."), mcp.WithStringItems()),
	)
	s.AddTool(searchSymbolsTool, h.HandleSearchSymbols)

	traceSymbolTool := mcp.NewTool("trace_symbol",
		mcp.WithDescription(traceSymbolToolDescription),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to a codebase root or subdirectory. Hint: Use your current working directory if unsure.")),
		mcp.WithString("symbol", mcp.Required(), mcp.Description("Exact symbol/identifier name to trace (e.g. \"HandleSearch\").")),
		mcp.WithString("mode", mcp.Description("Trace mode: \"callers\" (calls inside functions), \"references\" (calls and references, excluding definitions), \"definitions\" (definitions only), or \"all\" (default)."), mcp.DefaultString("all")),
	)
	s.AddTool(traceSymbolTool, h.HandleTraceSymbol)

	clearTool := mcp.NewTool("clear_index",
		mcp.WithDescription("Remove the stored semantic index and local index state for a codebase."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to an indexed codebase root. Hint: Use your current working directory if unsure.")),
	)
	s.AddTool(clearTool, h.HandleClear)

	statusTool := mcp.NewTool("get_indexing_status",
		mcp.WithDescription("Return indexing status and progress for a codebase."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to a codebase root or subdirectory. Hint: Use your current working directory if unsure.")),
	)
	s.AddTool(statusTool, h.HandleStatus)

	return s
}
