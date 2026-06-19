package pipeline

import (
	"cfmantic-code/internal/milvus"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntityPayloadBytesMatchesJSONMarshal(t *testing.T) {
	entity := &milvus.Entity{
		ID:            `chunk_"<&>` + string(rune(0x2028)),
		Content:       "line 1\nline 2\t" + strings.Repeat("界", 3), //nolint:gosmopolitan // Intentionally verifies UTF-8 JSON payload bytes.
		RelativePath:  "dir/<file>&.go",
		StartLine:     -12,
		EndLine:       34,
		FileExtension: "go",
		Metadata:      `{"codebasePath":"/tmp/<repo>&"}`,
	}

	payload, err := json.Marshal(entity)
	require.NoError(t, err)

	got, err := entityPayloadBytes(entity)
	require.NoError(t, err)
	assert.Equal(t, len(payload), got)
}

func TestEntityPayloadBytesAvoidsMarshalAllocation(t *testing.T) {
	entity := &milvus.Entity{
		ID:            "chunk_alloc",
		Content:       strings.Repeat("content", 100),
		RelativePath:  "main.go",
		StartLine:     1,
		EndLine:       10,
		FileExtension: "go",
		Metadata:      `{"codebasePath":"/tmp/repo"}`,
	}

	allocs := testing.AllocsPerRun(100, func() {
		_, _ = entityPayloadBytes(entity)
	})

	assert.Zero(t, allocs)
}
