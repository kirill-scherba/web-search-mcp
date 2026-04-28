# Design & Architecture

## Why Go?

Go provides first-class libraries for all key components:
- chromedp (Chrome DevTools Protocol) — available only in Go
- go-readability (Mozilla Readability port) — available only in Go
- go-libsql (Turso/libSQL driver) — needed for vector search
- mcp-go (MCP framework) — clean idiomatic API

## Two-Phase Search Workflow

```
Phase 1: web_search_analyze(query)
────────────────────────────────────────────────────
1. DuckDuckGo HTML search → 8-10 URLs
2. Parallel fetch each URL:
   ├─ chromedp → render JS (3s wait)
   └─ go-readability → extract full text
3. Ollama embeddinggemma → embedding for each page
4. Ollama embeddinggemma → embedding for the query
5. Cosine similarity (query_emb, page_emb) → relevance
6. Sort by relevance descending
7. Save pages + chunks + embeddings to libSQL DB
8. Return [{url, title, relevance, snippet}, ...]
   ↑ AI uses this to decide which pages to read fully

Phase 2: web_fetch(url)
────────────────────────────────────────────────────
1. Check DB cache (valid for 24h)
2. If cached: return stored full text
3. If not: chromedp + readability → full text
4. Save to DB for future caching
5. Return {url, title, full_text, length}
```

## Database Schema

```sql
-- Pages cache
CREATE TABLE web_pages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    url        TEXT NOT NULL UNIQUE,
    title      TEXT NOT NULL DEFAULT '',
    full_text  TEXT NOT NULL DEFAULT '',
    text_hash  TEXT NOT NULL DEFAULT '',
    fetched_at INTEGER NOT NULL DEFAULT 0
);

-- Text chunks with embeddings
CREATE TABLE web_chunks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    page_id    INTEGER NOT NULL,
    url        TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    chunk_idx  INTEGER NOT NULL DEFAULT 0,
    text       TEXT NOT NULL DEFAULT '',
    embedding  BLOB,       -- 768 float32 values
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (page_id) REFERENCES web_pages(id) ON DELETE CASCADE,
    UNIQUE(url, chunk_idx)
);
```

Embedding dimension: 768 (embeddinggemma model).

## Similarity Search

Cosine similarity is computed in Go (not SQL):

```go
func cosineSimilarity(a, b []float32) float64 {
    dotProduct += float64(a[i]) * float64(b[i])
    normA += float64(a[i]) * float64(a[i])
    normB += float64(b[i]) * float64(b[i])
    return dotProduct / (sqrt(normA) * sqrt(normB))
}
```

All stored embeddings are fetched and sorted in memory. For large collections (>10K chunks), SQL-level vector search via libsql vector extension would be activated.

## MCP Protocol

- Transport: stdin/stdout
- Protocol: JSON-RPC 2.0 (via mcp-go library)
- Framework: github.com/mark3labs/mcp-go

## Tool Definitions

| Tool | Input | Output | Description |
|------|-------|--------|-------------|
| web_search | query, limit | [{url, title, snippet}] | Lightweight search |
| web_search_analyze | query, limit, model | [{url, title, relevance, snippet}] | Full pipeline |
| web_fetch | url, wait_time | {url, title, text, excerpt, length} | Page content |
| web_semantic_search | query, limit, model | [{url, title, snippet}] | Indexed search |