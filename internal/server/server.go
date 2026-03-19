package mcpserver

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/handler"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	serverInstructions    = "Semantic code search for local codebases. First call index_codebase on a project root. Initial indexing and reindexing always start in the background. If async=false is sent for those runs, it is ignored because they may exceed MCP client timeouts; use get_indexing_status for progress. Incremental refreshes can still use async=false to wait for completion. Then call search_code on that indexed root or one of its subdirectories. Use clear_index to remove stored index data."
	indexToolDescription  = "Create or refresh a semantic index for a local codebase. Initial indexing and reindexing always start in the background; incremental refreshes can still wait with async=false, or you can poll with get_indexing_status."
	indexAsyncDescription = "Run asynchronously by default. Ignored for an initial full index or any reindex because those runs may exceed MCP client timeouts; set async=false only to wait for incremental refresh completion."
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
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to the local codebase root to index. Prefer the project root so ignore-file handling and status tracking stay stable.")),
		mcp.WithBoolean("reindex", mcp.Description("Delete existing index data for this codebase path before rebuilding."), mcp.DefaultBool(false)),
		mcp.WithBoolean("async", mcp.Description(indexAsyncDescription), mcp.DefaultBool(true)),
		mcp.WithArray("ignorePatterns", mcp.Description("Extra ignore patterns to apply in addition to .gitignore, .indexignore, and Git exclude rules."), mcp.WithStringItems()),
	)
	s.AddTool(indexTool, h.HandleIndex)

	searchTool := mcp.NewTool("search_code",
		mcp.WithDescription("Run a natural-language semantic search against a previously indexed local codebase."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to an indexed codebase root, or a subdirectory to limit results under an indexed ancestor.")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural-language description of the code, behavior, or symbols to find.")),
		mcp.WithNumber("limit", mcp.Description("Maximum results to return (default 10, max 20)."), mcp.DefaultNumber(10)),
		mcp.WithArray("extensionFilter", mcp.Description("Restrict results to these file extensions (for example '.go', '.ts')."), mcp.WithStringItems()),
	)
	s.AddTool(searchTool, h.HandleSearch)

	clearTool := mcp.NewTool("clear_index",
		mcp.WithDescription("Remove the stored semantic index and local index state for a codebase."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to the indexed codebase root to clear.")),
	)
	s.AddTool(clearTool, h.HandleClear)

	statusTool := mcp.NewTool("get_indexing_status",
		mcp.WithDescription("Return indexing status and progress for a codebase."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute path to the codebase or a subdirectory to inspect. If that exact path is not indexed, status falls back to the nearest managed ancestor.")),
	)
	s.AddTool(statusTool, h.HandleStatus)

	return s
}
