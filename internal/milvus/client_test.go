package milvus

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── transport helpers ────────────────────────────────────────────────────────

var (
	errSimulatedRead  = errors.New("simulated read error")
	errSimulatedClose = errors.New("simulated close error")
)

// roundTripFunc adapts a function to http.RoundTripper for injecting custom HTTP behavior.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// errReader is a ReadCloser whose Read always returns an error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errSimulatedRead }
func (errReader) Close() error             { return nil }

// errCloser wraps a Reader with a Close that always errors.
type errCloser struct{ io.Reader }

func (errCloser) Close() error { return errSimulatedClose }

// errReadErrClose errors on both Read and Close, exercising the branch where
// io.ReadAll fails and the subsequent Body.Close also fails.
type errReadErrClose struct{}

func (errReadErrClose) Read([]byte) (int, error) { return 0, errSimulatedRead }
func (errReadErrClose) Close() error             { return errSimulatedClose }

// ─── response builder ─────────────────────────────────────────────────────────

// writeAPIResp encodes a standard apiResponse to w.
func writeAPIResp(w http.ResponseWriter, code int, data any, message string) {
	type resp struct {
		Code    int    `json:"code"`
		Data    any    `json:"data"`
		Message string `json:"message"`
	}

	w.Header().Set("Content-Type", "application/json")

	b, err := json.Marshal(resp{Code: code, Data: data, Message: message})
	if err != nil {
		panic(err)
	}

	_, _ = w.Write(b)
}

// ─── NewClient ────────────────────────────────────────────────────────────────

func TestNewClient(t *testing.T) {
	c := NewClient("http://example.com", "my-token")
	assert.Equal(t, "http://example.com", c.baseURL)
	assert.Equal(t, "my-token", c.authToken)
	assert.Equal(t, "workers_ai", c.rerankStrategy)
	assert.NotNil(t, c.httpClient)
}

func TestSetRerankStrategy_EmptyUsesDefault(t *testing.T) {
	c := NewClient("http://example.com", "my-token")
	c.SetRerankStrategy("rrf")
	c.SetRerankStrategy("")

	assert.Equal(t, defaultRerankStrategy, c.rerankStrategy)
}

func TestClassifyError_ReturnsNilForNilError(t *testing.T) {
	assert.NoError(t, classifyError(ErrAPIResponse, nil))
}

func TestClassifyError_ReturnsOriginalForMatchingKind(t *testing.T) {
	original := errors.Join(ErrAPIResponse, errors.New("detail"))

	assert.Same(t, original, classifyError(ErrAPIResponse, original))
}

// ─── do() – fast paths ────────────────────────────────────────────────────────

func TestDo_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		writeAPIResp(w, 0, "hello", "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")

	var result string

	err := c.do(context.Background(), "/test", map[string]string{}, &result)
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestDo_MarshalError(t *testing.T) {
	c := NewClient("http://example.com", "token")
	// channels are not JSON-serialisable → Marshal must fail.
	err := c.do(context.Background(), "/test", make(chan int), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal request")
}

func TestDo_RequestCreationFailure(t *testing.T) {
	// A nil context.Context causes http.NewRequestWithContext to return an error.
	c := NewClient("http://example.com", "token")

	var nilCtx context.Context // nil interface

	err := c.do(nilCtx, "/test", map[string]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create request")
}

func TestDo_ReadBodyError(t *testing.T) {
	c := NewClient("http://example.com", "token")
	c.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: errReader{}}, nil
		}),
	}
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read response body")
	assert.ErrorIs(t, err, errSimulatedRead)
}

func TestDo_ReadBodyError_CloseAlsoFails(t *testing.T) {
	// When io.ReadAll fails AND the subsequent Body.Close also fails, the close
	// error must be returned (lines 309–310 in do()).
	c := NewClient("http://example.com", "token")
	c.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: errReadErrClose{}}, nil
		}),
	}
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "close response body")
	assert.ErrorIs(t, err, errSimulatedClose)
}

func TestDo_CloseBodyError(t *testing.T) {
	c := NewClient("http://example.com", "token")
	c.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			body := errCloser{strings.NewReader(`{"code":0,"data":null,"message":""}`)}
			return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
		}),
	}
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "close response body")
}

func TestDo_NonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnexpectedResponse)
}

func TestDo_NonJSONResponsePreviewTruncated(t *testing.T) {
	body := strings.Repeat("x", 200) + "TAIL" + strings.Repeat("y", 40)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnexpectedResponse)
	assert.Contains(t, err.Error(), strings.Repeat("x", 200)+"...")
	assert.NotContains(t, err.Error(), "TAIL")
}

func TestDo_NonJSON404Response_ClassifiedAsBackendUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("backend missing"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrBackendUnavailable)
	require.ErrorIs(t, err, ErrUnexpectedResponse)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Contains(t, err.Error(), "backend missing")
}

func TestDo_APIErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 100, nil, "some server error")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "code 100")
	assert.Contains(t, err.Error(), "some server error")
}

func TestDo_APIErrorNoSuchTable_ClassifiedAsSearchStateMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "Error: D1_ERROR: no such table: fts_code_chunks_deadbeef: SQLITE_ERROR")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/v2/vectordb/entities/hybrid_search", map[string]string{}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSearchStateMissing)
	require.ErrorIs(t, err, ErrAPIResponse)
	assert.Contains(t, err.Error(), "no such table")
}

func TestDo_UnmarshalDataError(t *testing.T) {
	// data is a JSON object, but result expects a string → Unmarshal must fail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"nested":"object"},"message":""}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")

	var result string

	err := c.do(context.Background(), "/test", map[string]string{}, &result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal data")
}

// ─── do() – retry / slow paths ───────────────────────────────────────────────
// NOTE: These tests exercise real time.Sleep inside the retry loop.
// TestDo_Retry429        ≈  1 s  (one 429 → one sleep(1s))
// TestDo_MaxRetriesExhausted ≈  7 s  (3 retries → sleep 1+2+4 s)
// TestDo_DoFailureMaxRetries ≈  7 s  (3 retries → sleep 1+2+4 s)

func TestDo_ContextCancelled_NetworkError(t *testing.T) {
	// Verify that a canceled context interrupts the backoff sleep and returns
	// context.Canceled quickly (not blocking for the full 1 s backoff).
	ctx, cancel := context.WithCancel(context.Background())

	c := NewClient("http://example.com", "token")
	c.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			cancel() // cancel on the first attempt so the subsequent backoff is interrupted
			return nil, errors.New("network error")
		}),
	}

	start := time.Now()
	err := c.do(ctx, "/test", map[string]string{}, nil)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 500*time.Millisecond, "backoff should have been interrupted by context cancellation")
}

func TestDo_ContextCancelled_Retry429(t *testing.T) {
	// Same as above but via the HTTP 429 retry path.
	ctx, cancel := context.WithCancel(context.Background())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cancel() // cancel on the first attempt so the subsequent backoff is interrupted
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")

	start := time.Now()
	err := c.do(ctx, "/test", map[string]string{}, nil)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 500*time.Millisecond, "backoff should have been interrupted by context cancellation")
}

func TestDo_ContextCancelled_Retry500(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	c := NewClient("http://example.com", "token")
	c.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			cancel()

			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("server error")),
			}, nil
		}),
	}

	start := time.Now()
	err := c.do(ctx, "/test", map[string]string{}, nil)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 500*time.Millisecond, "backoff should have been interrupted by context cancellation")
}

func TestDo_ContextCancelled_RetryableAPIError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	c := NewClient("http://example.com", "token")
	c.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			cancel()

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"code":1,"data":null,"message":"AiError: 3040: Capacity temporarily exceeded, please try again."}`)),
			}, nil
		}),
	}

	start := time.Now()
	err := c.do(ctx, "/test", map[string]string{}, nil)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, elapsed, 500*time.Millisecond, "backoff should have been interrupted by context cancellation")
}

func TestDo_Retry429(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		writeAPIResp(w, 0, nil, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 2, callCount.Load())
}

func TestDo_MaxRetriesExhausted(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
	assert.Contains(t, err.Error(), "retries")
	assert.EqualValues(t, 4, callCount.Load()) // initial attempt + 3 retries
}

func TestDo_DoFailureMaxRetries(t *testing.T) {
	// RoundTripper always returns a network-level error; exercises the Do-error retry path.
	var callCount atomic.Int32

	c := NewClient("http://example.com", "token")
	c.httpClient = &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			callCount.Add(1)
			return nil, errors.New("network error")
		}),
	}
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "POST /test")
	assert.EqualValues(t, 4, callCount.Load()) // initial attempt + 3 retries
}

// TestDo_RetryAPICapacityExceeded ≈ 1 s (one retryable API error → one sleep(1s))
func TestDo_RetryAPICapacityExceeded(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			writeAPIResp(w, 1, nil, "AiError: 3040: Capacity temporarily exceeded, please try again.")
			return
		}

		writeAPIResp(w, 0, nil, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 2, callCount.Load())
}

func TestDo_NonRetryableAPIError(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		writeAPIResp(w, 1, nil, "collection not found")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAPIResponse)
	assert.EqualValues(t, 1, callCount.Load())
}

// TestDo_RetryAPIErrorMaxRetriesExhausted ≈ 7 s (3 retries → sleep 1+2+4 s)
func TestDo_RetryAPIErrorMaxRetriesExhausted(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		writeAPIResp(w, 1, nil, "AiError: 3040: Capacity temporarily exceeded, please try again.")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	err := c.do(context.Background(), "/test", map[string]string{}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAPIResponse)
	assert.EqualValues(t, 4, callCount.Load()) // initial attempt + 3 retries
}

// ─── CreateCollection ─────────────────────────────────────────────────────────

func TestCreateCollection_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/collections/create", r.URL.Path)
		writeAPIResp(w, 0, nil, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.NoError(t, c.CreateCollection(context.Background(), "my-coll", 1536, false))
}

func TestCreateCollection_HybridSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request contains the BM25 function definition.
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}

		schema, ok := body["schema"].(map[string]any)
		if !assert.True(t, ok) {
			return
		}

		fns, ok := schema["functions"].([]any)
		if !assert.True(t, ok) {
			return
		}

		assert.Len(t, fns, 1)
		writeAPIResp(w, 0, nil, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.NoError(t, c.CreateCollection(context.Background(), "hybrid-coll", 1536, true))
}

func TestCreateCollection_AlreadyExists(t *testing.T) {
	// create returns error, but HasCollection confirms the collection exists → nil (idempotent).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/vectordb/collections/create":
			writeAPIResp(w, 1, nil, "collection already exists")
		case "/v2/vectordb/collections/has":
			writeAPIResp(w, 0, map[string]bool{"has": true}, "")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.NoError(t, c.CreateCollection(context.Background(), "my-coll", 1536, false))
}

func TestCreateCollection_ErrorAndNotExists(t *testing.T) {
	// create returns error, HasCollection confirms it does NOT exist → propagate error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/vectordb/collections/create":
			writeAPIResp(w, 1, nil, "create failed")
		case "/v2/vectordb/collections/has":
			writeAPIResp(w, 0, map[string]bool{"has": false}, "")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.Error(t, c.CreateCollection(context.Background(), "my-coll", 1536, false))
}

func TestCreateCollection_ErrorAndHasCollectionFails(t *testing.T) {
	// create returns error, HasCollection itself fails → propagate original create error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/vectordb/collections/create":
			writeAPIResp(w, 1, nil, "create error")
		case "/v2/vectordb/collections/has":
			writeAPIResp(w, 99, nil, "has collection error")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.Error(t, c.CreateCollection(context.Background(), "my-coll", 1536, false))
}

// ───── DropCollection ───────────────────────────────────────────────────────────

func TestDropCollection_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/collections/drop", r.URL.Path)
		writeAPIResp(w, 0, nil, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.NoError(t, c.DropCollection(context.Background(), "my-coll"))
}

func TestDropCollection_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "drop failed")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.Error(t, c.DropCollection(context.Background(), "my-coll"))
}

// ─── HasCollection ────────────────────────────────────────────────────────────

func TestHasCollection_True(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/collections/has", r.URL.Path)
		writeAPIResp(w, 0, map[string]bool{"has": true}, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	exists, err := c.HasCollection(context.Background(), "my-coll")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestHasCollection_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 0, map[string]bool{"has": false}, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	exists, err := c.HasCollection(context.Background(), "my-coll")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestHasCollection_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "has collection failed")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	_, err := c.HasCollection(context.Background(), "my-coll")
	require.Error(t, err)
}

// ─── ListCollections ──────────────────────────────────────────────────────────

func TestListCollections_Multiple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/collections/list", r.URL.Path)
		writeAPIResp(w, 0, []string{"coll-a", "coll-b", "coll-c"}, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	names, err := c.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"coll-a", "coll-b", "coll-c"}, names)
}

func TestListCollections_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 0, []string{}, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	names, err := c.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestListCollections_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "list failed")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	names, err := c.ListCollections(context.Background())
	require.Error(t, err)
	assert.Nil(t, names)
}

// ─── Insert ───────────────────────────────────────────────────────────────────

func TestInsert_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/entities/insert", r.URL.Path)
		writeAPIResp(w, 0, InsertResult{InsertCount: 2, InsertIDs: []string{"id1", "id2"}}, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	entities := []Entity{
		{ID: "id1", Content: "foo", RelativePath: "a.go"},
		{ID: "id2", Content: "bar", RelativePath: "b.go"},
	}
	result, err := c.Insert(context.Background(), "my-coll", entities)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, result.InsertCount)
	assert.Equal(t, []string{"id1", "id2"}, result.InsertIDs)
}

func TestInsert_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "insert failed")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	result, err := c.Insert(context.Background(), "my-coll", nil)
	require.Error(t, err)
	assert.Nil(t, result)
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestDelete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/entities/delete", r.URL.Path)
		writeAPIResp(w, 0, nil, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.NoError(t, c.Delete(context.Background(), "my-coll", `id == "abc"`))
}

func TestDelete_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "delete failed")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	require.Error(t, c.Delete(context.Background(), "my-coll", `id == "abc"`))
}

// ─── Search ───────────────────────────────────────────────────────────────────

func TestSearch_Results(t *testing.T) {
	want := []SearchResult{
		{ID: "r1", Distance: 0.9, Content: "result one", RelativePath: "a.go", StartLine: 1, EndLine: 5},
		{ID: "r2", Distance: 0.7, Content: "result two", RelativePath: "b.go", StartLine: 10, EndLine: 20},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/entities/search", r.URL.Path)
		writeAPIResp(w, 0, want, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	results, err := c.Search(context.Background(), "my-coll", "query text", 5, "")
	require.NoError(t, err)
	assert.Equal(t, want, results)
}

func TestSearch_WithFilter(t *testing.T) {
	want := []SearchResult{
		{ID: "r1", Distance: 0.9, Content: "result one", RelativePath: "a.go", StartLine: 1, EndLine: 5},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, `fileExtension in ["go"]`, body["filter"])
		writeAPIResp(w, 0, want, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	results, err := c.Search(context.Background(), "my-coll", "query text", 5, `fileExtension in ["go"]`)
	require.NoError(t, err)
	assert.Equal(t, want, results)
}

func TestSearch_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 0, []SearchResult{}, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	results, err := c.Search(context.Background(), "my-coll", "nothing", 5, "")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSearch_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "search failed")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	results, err := c.Search(context.Background(), "my-coll", "query", 5, "")
	require.Error(t, err)
	assert.Nil(t, results)
}

// ─── HybridSearch ─────────────────────────────────────────────────────────────

func TestHybridSearch_Results(t *testing.T) {
	want := []SearchResult{
		{ID: "h1", Score: 0.95, Content: "hybrid result", RelativePath: "c.go"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/entities/hybrid_search", r.URL.Path)
		writeAPIResp(w, 0, want, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	results, err := c.HybridSearch(context.Background(), "my-coll", "hybrid query", 5, 60, "")
	require.NoError(t, err)
	assert.Equal(t, want, results)
}

func TestHybridSearch_WithFilter(t *testing.T) {
	want := []SearchResult{
		{ID: "h1", Score: 0.95, Content: "hybrid result", RelativePath: "c.go"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, `fileExtension in ["go", "ts"]`, body["filter"])
		writeAPIResp(w, 0, want, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	results, err := c.HybridSearch(context.Background(), "my-coll", "hybrid query", 5, 60, `fileExtension in ["go", "ts"]`)
	require.NoError(t, err)
	assert.Equal(t, want, results)
}

func TestHybridSearch_UsesConfiguredRerankStrategy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}

		rerank, ok := body["rerank"].(map[string]any)
		if !assert.True(t, ok) {
			return
		}

		assert.Equal(t, "rrf", rerank["strategy"])

		params, ok := rerank["params"].(map[string]any)
		if !assert.True(t, ok) {
			return
		}

		assert.InDelta(t, 60, params["k"], 0)

		writeAPIResp(w, 0, []SearchResult{}, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	c.SetRerankStrategy("rrf")

	results, err := c.HybridSearch(context.Background(), "my-coll", "hybrid query", 5, 60, "")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestHybridSearch_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "hybrid search failed")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	results, err := c.HybridSearch(context.Background(), "my-coll", "query", 5, 60, "")
	require.Error(t, err)
	assert.Nil(t, results)
}

// ─── Query ────────────────────────────────────────────────────────────────────

func TestQuery_Entities(t *testing.T) {
	want := []Entity{
		{ID: "e1", Content: "entity one", RelativePath: "x.go", StartLine: 1, EndLine: 3, FileExtension: ".go"},
		{ID: "e2", Content: "entity two", RelativePath: "y.go", StartLine: 5, EndLine: 8, FileExtension: ".go"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/vectordb/entities/query", r.URL.Path)
		writeAPIResp(w, 0, want, "")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	entities, err := c.Query(context.Background(), "my-coll", `relativePath == "x.go"`, 10)
	require.NoError(t, err)
	assert.Equal(t, want, entities)
}

func TestQuery_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIResp(w, 1, nil, "query failed")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token")
	entities, err := c.Query(context.Background(), "my-coll", `id == "x"`, 10)
	require.Error(t, err)
	assert.Nil(t, entities)
}
