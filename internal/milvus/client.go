package milvus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors returned by the milvus client.
var (
	ErrHTTPRequest        = errors.New("milvus: HTTP request failed")
	ErrAPIResponse        = errors.New("milvus: API error")
	ErrUnexpectedResponse = errors.New("milvus: unexpected non-JSON response")
	ErrBackendUnavailable = errors.New("milvus: backend unavailable")
	ErrSearchStateMissing = errors.New("milvus: backend search state missing")
)

const defaultRerankStrategy = "workers_ai"

// VectorClient is the interface consumed by handler and sync packages.
type VectorClient interface {
	CreateCollection(ctx context.Context, name string, dimension int, hybrid bool) error
	DropCollection(ctx context.Context, name string) error
	HasCollection(ctx context.Context, name string) (bool, error)
	ListCollections(ctx context.Context) ([]string, error)
	Insert(ctx context.Context, collection string, entities []Entity) (*InsertResult, error)
	Delete(ctx context.Context, collection, filter string) error
	Search(ctx context.Context, collection, query string, limit int, filter string) ([]SearchResult, error)
	HybridSearch(ctx context.Context, collection, query string, limit, rrfK int, filter string) ([]SearchResult, error)
	Query(ctx context.Context, collection, filter string, limit int) ([]Entity, error)
}

// Client is a thin HTTP client for the cf-workers-milvus Cloudflare Worker.
type Client struct {
	baseURL        string
	authToken      string
	rerankStrategy string
	httpClient     *http.Client
}

// NewClient creates a new Client with a 60-second HTTP timeout.
func NewClient(baseURL, authToken string) *Client {
	return &Client{
		baseURL:        baseURL,
		authToken:      authToken,
		rerankStrategy: defaultRerankStrategy,
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
	}
}

// SetRerankStrategy sets the Milvus hybrid rerank strategy.
func (c *Client) SetRerankStrategy(strategy string) {
	if strategy == "" {
		c.rerankStrategy = defaultRerankStrategy
		return
	}

	c.rerankStrategy = strategy
}

// Entity represents a document chunk stored in the vector database.
type Entity struct {
	ID            string `json:"id"`
	Content       string `json:"content"`
	RelativePath  string `json:"relativePath"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
	FileExtension string `json:"fileExtension"`
	Metadata      string `json:"metadata"`
}

// SearchResult represents a single result from a vector or hybrid search.
type SearchResult struct {
	ID            string  `json:"id"`
	Distance      float64 `json:"distance"`
	Score         float64 `json:"score"`
	Content       string  `json:"content"`
	RelativePath  string  `json:"relativePath"`
	StartLine     int     `json:"startLine"`
	EndLine       int     `json:"endLine"`
	FileExtension string  `json:"fileExtension"`
	Metadata      string  `json:"metadata"`
}

// InsertResult contains the outcome of an insert operation.
type InsertResult struct {
	InsertCount int      `json:"insertCount"`
	InsertIDs   []string `json:"insertIds"`
}

type apiResponse struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

type classifiedError struct {
	kind error
	err  error
}

func (e *classifiedError) Error() string {
	return e.err.Error()
}

func (e *classifiedError) Unwrap() []error {
	return []error{e.kind, e.err}
}

func classifyError(kind, err error) error {
	if err == nil || errors.Is(err, kind) {
		return err
	}

	return &classifiedError{kind: kind, err: err}
}

func classifyResponseError(path string, statusCode int, message string, err error) error {
	if statusCode == http.StatusNotFound {
		return classifyError(ErrBackendUnavailable, err)
	}

	if path == "/v2/vectordb/entities/hybrid_search" && isMissingSearchStateMessage(message) {
		return classifyError(ErrSearchStateMissing, err)
	}

	return err
}

func isMissingSearchStateMessage(message string) bool {
	message = strings.ToLower(message)

	return strings.Contains(message, "no such table") || strings.Contains(message, "table not found")
}

// CreateCollection creates a collection with the given name and vector dimension.
// When hybrid is true, a BM25 sparse vector function is included in the schema.
// The operation is idempotent — if the collection already exists, nil is returned.
func (c *Client) CreateCollection(ctx context.Context, name string, dimension int, hybrid bool) error {
	type fieldParams struct {
		Dim string `json:"dim"`
	}

	type field struct {
		FieldName         string       `json:"fieldName"`
		DataType          string       `json:"dataType"`
		IsPrimary         bool         `json:"isPrimary,omitempty"`
		ElementTypeParams *fieldParams `json:"elementTypeParams,omitempty"`
	}

	type bm25Function struct {
		Type string `json:"type"`
	}

	type schema struct {
		Fields    []field        `json:"fields"`
		Functions []bm25Function `json:"functions,omitempty"`
	}

	type body struct {
		CollectionName string `json:"collectionName"`
		Schema         schema `json:"schema"`
	}

	fields := []field{
		{FieldName: "id", DataType: "VarChar", IsPrimary: true},
		{FieldName: "vector", DataType: "FloatVector", ElementTypeParams: &fieldParams{Dim: strconv.Itoa(dimension)}},
	}

	s := schema{Fields: fields}
	if hybrid {
		s.Functions = []bm25Function{{Type: "BM25"}}
	}

	err := c.do(ctx, "/v2/vectordb/collections/create", body{CollectionName: name, Schema: s}, nil)
	if err != nil {
		// Treat "already exists" as idempotent success.
		// The worker may return a non-zero code with a message containing "already exist".
		// We check HasCollection as a fallback to confirm.
		exists, checkErr := c.HasCollection(ctx, name)
		if checkErr == nil && exists {
			return nil
		}

		return err
	}

	return nil
}

// DropCollection removes the named collection from the database.
func (c *Client) DropCollection(ctx context.Context, name string) error {
	return c.do(ctx, "/v2/vectordb/collections/drop", map[string]string{"collectionName": name}, nil)
}

// HasCollection reports whether a collection with the given name exists.
func (c *Client) HasCollection(ctx context.Context, name string) (bool, error) {
	var data struct {
		Has bool `json:"has"`
	}
	if err := c.do(ctx, "/v2/vectordb/collections/has", map[string]string{"collectionName": name}, &data); err != nil {
		return false, err
	}

	return data.Has, nil
}

// ListCollections returns the names of all existing collections.
func (c *Client) ListCollections(ctx context.Context) ([]string, error) {
	var names []string
	if err := c.do(ctx, "/v2/vectordb/collections/list", map[string]any{}, &names); err != nil {
		return nil, err
	}

	return names, nil
}

// Insert adds entities into the named collection. The worker auto-generates
// embeddings from each entity's Content field.
func (c *Client) Insert(ctx context.Context, collection string, entities []Entity) (*InsertResult, error) {
	body := map[string]any{
		"collectionName": collection,
		"data":           entities,
	}

	var result InsertResult
	if err := c.do(ctx, "/v2/vectordb/entities/insert", body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// Delete removes entities from the named collection that match the given filter expression.
func (c *Client) Delete(ctx context.Context, collection, filter string) error {
	body := map[string]string{
		"collectionName": collection,
		"filter":         filter,
	}

	return c.do(ctx, "/v2/vectordb/entities/delete", body, nil)
}

// Search performs a dense vector search against the named collection.
// The query string is auto-embedded by the worker.
// Pass a non-empty filter to apply a Milvus filter expression (e.g. `fileExtension in ["go"]`).
func (c *Client) Search(ctx context.Context, collection, query string, limit int, filter string) ([]SearchResult, error) {
	body := map[string]any{
		"collectionName": collection,
		"data":           []string{query},
		"annsField":      "vector",
		"limit":          limit,
		"outputFields":   []string{"content", "relativePath", "startLine", "endLine", "fileExtension", "metadata"},
	}

	if filter != "" {
		body["filter"] = filter
	}

	var results []SearchResult
	if err := c.do(ctx, "/v2/vectordb/entities/search", body, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// HybridSearch performs a combined dense + sparse (BM25) search with configurable re-ranking.
// Pass a non-empty filter to apply a Milvus filter expression (e.g. `fileExtension in ["go"]`).
func (c *Client) HybridSearch(ctx context.Context, collection, query string, limit, rrfK int, filter string) ([]SearchResult, error) {
	body := map[string]any{
		"collectionName": collection,
		"search": []map[string]any{
			{"annsField": "vector", "data": []string{query}, "limit": limit * 2},
			{"annsField": "sparse_vector", "data": []string{query}, "limit": limit * 2},
		},
		"rerank": map[string]any{
			"strategy": c.rerankStrategy,
			"params":   map[string]any{"k": rrfK},
		},
		"limit":        limit,
		"outputFields": []string{"content", "relativePath", "startLine", "endLine", "fileExtension", "metadata"},
	}

	if filter != "" {
		body["filter"] = filter
	}

	var results []SearchResult
	if err := c.do(ctx, "/v2/vectordb/entities/hybrid_search", body, &results); err != nil {
		return nil, err
	}

	return results, nil
}

// Query retrieves entities from the named collection matching the given filter expression.
func (c *Client) Query(ctx context.Context, collection, filter string, limit int) ([]Entity, error) {
	body := map[string]any{
		"collectionName": collection,
		"filter":         filter,
		"outputFields":   []string{"id", "content", "relativePath", "startLine", "endLine", "fileExtension", "metadata"},
		"limit":          limit,
	}

	var entities []Entity
	if err := c.do(ctx, "/v2/vectordb/entities/query", body, &entities); err != nil {
		return nil, err
	}

	return entities, nil
}

func (c *Client) do(ctx context.Context, path string, reqBody, result any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("milvus: marshal request: %w", err)
	}

	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("milvus: create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.authToken)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < maxRetries {
				log.Printf("milvus: POST %s attempt %d failed: %v", path, attempt+1, err)

				if retryErr := sleepWithJitter(ctx, time.Duration(1<<attempt)*time.Second); retryErr != nil {
					return retryErr
				}

				continue
			}

			return fmt.Errorf("milvus: POST %s: %w", path, err)
		}

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			readErr := err

			if closeErr := resp.Body.Close(); closeErr != nil {
				return fmt.Errorf("milvus: close response body: %w", closeErr)
			}

			return fmt.Errorf("milvus: read response body: %w", readErr)
		}

		err = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("milvus: close response body: %w", err)
		}

		// Retry on 429/5xx
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt < maxRetries {
				log.Printf("milvus: POST %s attempt %d got %d, retrying", path, attempt+1, resp.StatusCode)

				if retryErr := sleepWithJitter(ctx, time.Duration(1<<attempt)*time.Second); retryErr != nil {
					return retryErr
				}

				continue
			}

			return fmt.Errorf("%w: POST %s: HTTP %d after %d retries", ErrHTTPRequest, path, resp.StatusCode, maxRetries)
		}

		var apiResp apiResponse
		if err := json.Unmarshal(raw, &apiResp); err != nil {
			preview := string(raw)
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}

			baseErr := fmt.Errorf("%w: POST %s: HTTP %d: %s", ErrUnexpectedResponse, path, resp.StatusCode, preview)

			return classifyResponseError(path, resp.StatusCode, preview, baseErr)
		}

		if apiResp.Code != 0 {
			if attempt < maxRetries && strings.Contains(strings.ToLower(apiResp.Message), "try again") {
				log.Printf("milvus: POST %s attempt %d API error (retryable): code %d: %s", path, attempt+1, apiResp.Code, apiResp.Message)

				if retryErr := sleepWithJitter(ctx, time.Duration(1<<attempt)*time.Second); retryErr != nil {
					return retryErr
				}

				continue
			}

			log.Printf("milvus: POST %s error code %d: %s", path, apiResp.Code, apiResp.Message)

			baseErr := fmt.Errorf("%w: POST %s: code %d: %s", ErrAPIResponse, path, apiResp.Code, apiResp.Message)

			return classifyResponseError(path, resp.StatusCode, apiResp.Message, baseErr)
		}

		if result != nil && len(apiResp.Data) > 0 {
			if err := json.Unmarshal(apiResp.Data, result); err != nil {
				return fmt.Errorf("milvus: unmarshal data: %w", err)
			}
		}

		break
	}

	return nil
}

// sleepWithJitter waits for the given base duration plus up to 25% random jitter,
// but returns immediately with ctx.Err() if the context is canceled first.
// This prevents thundering herd on retry storms and supports clean shutdown.
func sleepWithJitter(ctx context.Context, base time.Duration) error {
	jitter := time.Duration(rand.Int63n(int64(base) / 4)) //nolint:gosec // non-cryptographic jitter is intentional
	timer := time.NewTimer(base + jitter)

	select {
	case <-ctx.Done():
		timer.Stop()
		return fmt.Errorf("milvus: retry backoff interrupted: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
