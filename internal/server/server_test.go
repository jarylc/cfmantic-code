package mcpserver

import (
	"cfmantic-code/internal/config"
	"cfmantic-code/internal/handler"
	"cfmantic-code/internal/milvus"
	"cfmantic-code/internal/snapshot"
	"cfmantic-code/internal/splitter"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_ReturnsNonNilServer(t *testing.T) {
	s := newTestServer(t)

	assert.NotNil(t, s)
}

func TestNew_ToolInputSchemasUsePlainArrayNodes(t *testing.T) {
	s := newTestServer(t)
	tools := s.ListTools()
	require.NotEmpty(t, tools)

	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		tool := tools[name].Tool

		t.Run(name, func(t *testing.T) {
			payload, err := json.Marshal(tool)
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(payload, &decoded))

			schema, ok := decoded["inputSchema"]
			require.True(t, ok, "tool %q is missing inputSchema", name)

			violations := findNullableArraySchemaViolations(schema, "inputSchema")
			require.Empty(t, violations, "Gemini-incompatible array schema found:\n%s", strings.Join(violations, "\n"))
		})
	}
}

func TestNew_PathPropertiesDescribeStableScopeConstraints(t *testing.T) {
	s := newTestServer(t)

	cases := []struct {
		tool     string
		property string
		required bool
		typeName string
		contains []string
	}{
		{
			tool:     "index_codebase",
			property: "path",
			required: true,
			typeName: "string",
			contains: []string{"codebase root", "ignore-file handling", "status tracking"},
		},
		{
			tool:     "search_code",
			property: "path",
			required: true,
			typeName: "string",
			contains: []string{"indexed codebase root", "subdirectory", "indexed ancestor"},
		},
		{
			tool:     "get_indexing_status",
			property: "path",
			required: true,
			typeName: "string",
			contains: []string{"codebase or a subdirectory", "nearest managed ancestor"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.tool+"/"+tc.property, func(t *testing.T) {
			properties := toolProperties(t, s, tc.tool)

			property, ok := properties[tc.property].(map[string]any)
			require.True(t, ok, "tool %q missing property %q", tc.tool, tc.property)
			assert.Equal(t, tc.typeName, property["type"])

			for _, fragment := range tc.contains {
				assert.Contains(t, toolPropertyDescription(t, s, tc.tool, tc.property), fragment)
			}

			if tc.required {
				assert.Contains(t, toolRequiredProperties(t, s, tc.tool), tc.property)
			}
		})
	}
}

func TestNew_InstructionsExplainIndexCodebaseTimeoutBehavior(t *testing.T) {
	s := newTestServer(t)

	instructions := serverInstructionText(t, s)
	for _, fragment := range []string{
		"index_codebase",
		"project root",
		"async=false",
		"background",
		"client timeouts",
		"Incremental refreshes",
		"search_code",
		"subdirectories",
		"clear_index",
	} {
		assert.Contains(t, instructions, fragment)
	}
}

func TestNew_IndexCodebaseAsyncDefaultsToTrue(t *testing.T) {
	s := newTestServer(t)

	properties := toolProperties(t, s, "index_codebase")
	asyncProperty, ok := properties["async"].(map[string]any)
	require.True(t, ok, "index_codebase missing async property")
	assert.Equal(t, true, asyncProperty["default"])
	assert.Equal(t, "boolean", asyncProperty["type"])
}

func TestNew_SearchCodeLimitDescriptionMatchesBackendCap(t *testing.T) {
	s := newTestServer(t)

	properties := toolProperties(t, s, "search_code")
	limit, ok := properties["limit"].(map[string]any)
	require.True(t, ok, "search_code missing limit property")
	assert.Equal(t, "number", limit["type"])
	assert.InDelta(t, 10, limit["default"], 0)

	description := toolPropertyDescription(t, s, "search_code", "limit")
	for _, fragment := range []string{"default 10", "max 20"} {
		assert.Contains(t, description, fragment)
	}
}

func newTestServer(t *testing.T) *server.MCPServer {
	t.Helper()

	t.Setenv("WORKER_URL", "https://worker.example.com")
	t.Setenv("AUTH_TOKEN", "test-token")

	cfg, err := config.Load()
	require.NoError(t, err)

	mc := milvus.NewClient(cfg.WorkerURL, cfg.AuthToken)
	sm := snapshot.NewManager()
	sp := splitter.NewASTSplitter(cfg.ChunkSize, cfg.ChunkOverlap)
	h := handler.New(mc, sm, cfg, sp, nil)

	return New(cfg, h)
}

func toolPropertyDescription(t *testing.T, s *server.MCPServer, toolName, propertyName string) string {
	t.Helper()

	properties := toolProperties(t, s, toolName)

	property, ok := properties[propertyName].(map[string]any)
	require.True(t, ok, "tool %q missing property %q", toolName, propertyName)

	description, ok := property["description"].(string)
	require.True(t, ok, "tool %q property %q missing description", toolName, propertyName)

	return description
}

func serverInstructionText(t *testing.T, s *server.MCPServer) string {
	t.Helper()

	message, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	})
	require.NoError(t, err)

	response := s.HandleMessage(context.Background(), message)
	payload, err := json.Marshal(response)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	result, ok := decoded["result"].(map[string]any)
	require.True(t, ok, "initialize response missing result")

	instructions, ok := result["instructions"].(string)
	require.True(t, ok, "initialize response missing instructions")

	return instructions
}

func toolProperties(t *testing.T, s *server.MCPServer, toolName string) map[string]any {
	t.Helper()

	tool, ok := s.ListTools()[toolName]
	require.True(t, ok, "tool %q not registered", toolName)

	payload, err := json.Marshal(tool.Tool)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	inputSchema, ok := decoded["inputSchema"].(map[string]any)
	require.True(t, ok, "tool %q missing inputSchema", toolName)

	properties, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "tool %q missing properties", toolName)

	return properties
}

func toolRequiredProperties(t *testing.T, s *server.MCPServer, toolName string) []string {
	t.Helper()

	tool, ok := s.ListTools()[toolName]
	require.True(t, ok, "tool %q not registered", toolName)

	payload, err := json.Marshal(tool.Tool)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	inputSchema, ok := decoded["inputSchema"].(map[string]any)
	require.True(t, ok, "tool %q missing inputSchema", toolName)

	required, ok := inputSchema["required"].([]any)
	require.True(t, ok, "tool %q missing required list", toolName)

	properties := make([]string, 0, len(required))
	for _, item := range required {
		name, ok := item.(string)
		require.True(t, ok, "tool %q has non-string required property", toolName)

		properties = append(properties, name)
	}

	return properties
}

func findNullableArraySchemaViolations(node any, path string) []string {
	var violations []string

	switch node := node.(type) {
	case map[string]any:
		if _, hasItems := node["items"]; hasItems && !hasExactArrayType(node["type"]) {
			violations = append(violations,
				fmt.Sprintf("%s uses items with non-array type %s", path, schemaTypeString(node["type"])),
			)
		}

		for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
			branches, ok := node[keyword].([]any)
			if !ok {
				continue
			}

			for i, branch := range branches {
				if schemaNodeRepresentsArray(branch) {
					violations = append(violations,
						fmt.Sprintf("%s.%s[%d] uses an array branch", path, keyword, i),
					)
				}
			}
		}

		for key, child := range node {
			violations = append(violations, findNullableArraySchemaViolations(child, path+"."+key)...)
		}
	case []any:
		for i, child := range node {
			violations = append(violations, findNullableArraySchemaViolations(child, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}

	return violations
}

func hasExactArrayType(schemaType any) bool {
	typeName, ok := schemaType.(string)
	return ok && typeName == "array"
}

func schemaNodeRepresentsArray(node any) bool {
	schema, ok := node.(map[string]any)
	if !ok {
		return false
	}

	if _, hasItems := schema["items"]; hasItems {
		return true
	}

	if hasExactArrayType(schema["type"]) {
		return true
	}

	types, ok := schema["type"].([]any)
	if !ok {
		return false
	}

	for _, entry := range types {
		if typeName, ok := entry.(string); ok && typeName == "array" {
			return true
		}
	}

	return false
}

func schemaTypeString(schemaType any) string {
	if schemaType == nil {
		return "<missing>"
	}

	encoded, err := json.Marshal(schemaType)
	if err != nil {
		return fmt.Sprintf("<unmarshalable %T>", schemaType)
	}

	return string(encoded)
}
