package main

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kirill-scherba/sqlh"
	_ "github.com/tursodatabase/go-libsql"
)

// WebPageRecord represents a record in the web_pages table.
type WebPageRecord struct {
	_         string `db_table_name:"web_pages"`
	ID        int64  `db:"id" db_key:"primary key autoincrement"`
	URL       string `db:"url" db_key:"unique"`
	Title     string `db:"title"`
	FullText  string `db:"full_text"`
	TextHash  string `db:"text_hash"`
	FetchedAt int64  `db:"fetched_at"`
}

// WebChunkRecord represents a chunk in the web_chunks table with embedding.
type WebChunkRecord struct {
	_         string `db_table_name:"web_chunks"`
	ID        int64  `db:"id" db_key:"primary key autoincrement"`
	PageID    int64  `db:"page_id"`
	URL       string `db:"url"`
	Title     string `db:"title"`
	ChunkIdx  int    `db:"chunk_idx"`
	Text      string `db:"text"`
	Embedding []byte `db:"embedding"`
	CreatedAt string `db:"created_at"`

	// Composite unique constraint on (url, chunk_idx).
	_ string `db:"-" db_key:"CONSTRAINT url_chunk UNIQUE (url, chunk_idx)"`
	// Foreign key with cascade delete when a page is removed.
	_ string `db:"-" db_key:"CONSTRAINT webchunks_page_id_fk FOREIGN KEY (page_id) REFERENCES web_pages(id) ON DELETE CASCADE"`
}

// Store manages the web page database.
type Store struct {
	db      *sql.DB
	dbPath  string
	enabled bool
}

// NewStore creates a new Store and initializes the database.
func NewStore(dbPath string) *Store {
	if dbPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			log.Printf("⚠️  Could not get user config directory: %v", err)
			return &Store{enabled: false}
		}
		dbPath = filepath.Join(configDir, "web-search-mcp", "web_search.db")
	}

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("⚠️  Could not create database directory %s: %v", dir, err)
		return &Store{enabled: false}
	}

	// Connect to libsql database
	dataSourceName := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("libsql", dataSourceName)
	if err != nil {
		log.Printf("⚠️  Could not open database: %v", err)
		return &Store{enabled: false}
	}

	// Check connection
	if err := db.Ping(); err != nil {
		log.Printf("⚠️  Could not ping database: %v", err)
		return &Store{enabled: false}
	}

	store := &Store{
		db:      db,
		dbPath:  dbPath,
		enabled: true,
	}

	// Create tables
	if err := store.createTables(); err != nil {
		log.Printf("⚠️  Could not create tables: %v", err)
		store.enabled = false
		return store
	}

	log.Printf("✅ Database ready at: %s", dbPath)
	return store
}

// Enabled returns whether the store is ready for use.
func (s *Store) Enabled() bool {
	return s.enabled
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// createTables creates the database tables if they don't exist.
func (s *Store) createTables() error {
	// Enable vector extension in libsql. This is not a table operation, so it
	// stays as raw SQL.
	_, err := s.db.Exec("CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		// Vector extension might not be available in all libsql builds
		log.Printf("⚠️  Vector extension not available (cosine similarity will use custom function): %v", err)
	}

	// Create tables from struct definitions.
	if err := sqlh.Create[WebPageRecord](s.db); err != nil {
		return fmt.Errorf("create web_pages table: %w", err)
	}
	if err := sqlh.Create[WebChunkRecord](s.db); err != nil {
		return fmt.Errorf("create web_chunks table: %w", err)
	}

	// sqlh generates only CREATE TABLE statements; create the index manually.
	_, err = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_web_chunks_url ON web_chunks(url)")
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}

	return nil
}

// PageExists checks if a page with the given URL exists in the database.
// Returns the page record if found, nil otherwise.
func (s *Store) PageExists(url string) (*WebPageRecord, error) {
	if !s.enabled {
		return nil, nil
	}

	record, err := sqlh.Get[WebPageRecord](s.db, sqlh.Eq("url", url))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

// SavePage saves a fetched page to the database.
func (s *Store) SavePage(fetched *FetchedPage) (int64, error) {
	if !s.enabled {
		return 0, nil
	}

	// Create hash for dedup
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(fetched.Text)))

	record := WebPageRecord{
		URL:       fetched.URL,
		Title:     fetched.Title,
		FullText:  fetched.Text,
		TextHash:  hash,
		FetchedAt: fetched.FetchedAt,
	}
	if err := sqlh.Set(s.db, record, sqlh.Eq("url", fetched.URL)); err != nil {
		return 0, fmt.Errorf("save page: %w", err)
	}

	// Get the page ID (inserted or updated row)
	existing, err := sqlh.Get[WebPageRecord](s.db, sqlh.Eq("url", fetched.URL))
	if err != nil {
		return 0, err
	}
	return existing.ID, nil
}

// SaveChunk saves a text chunk with its embedding to the database.
func (s *Store) SaveChunk(pageID int64, url, title string, chunkIdx int, text string, embedding []float32) error {
	if !s.enabled {
		return nil
	}

	record := WebChunkRecord{
		PageID:    pageID,
		URL:       url,
		Title:     title,
		ChunkIdx:  chunkIdx,
		Text:      text,
		Embedding: float32SliceToBytes(embedding),
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
	}
	if err := sqlh.Set(s.db, record, sqlh.Eq("url", url), sqlh.Eq("chunk_idx", chunkIdx)); err != nil {
		return fmt.Errorf("save chunk: %w", err)
	}
	return nil
}

// SearchSemantic performs a cosine similarity search using vector distances.
// This function is intentionally kept as raw SQL because the full-table vector
// scan and in-memory cosine sort are not a good fit for sqlh's struct-driven
// query generation.
func (s *Store) SearchSemantic(embedding []float32, limit int) ([]SearchResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("database is not available")
	}
	if limit <= 0 {
		limit = 5
	}

	// Use SQL to find nearest neighbors
	// Since libsql vector extension may not be available, we fetch all embeddings
	// and compute cosine similarity in Go
	rows, err := s.db.Query(`
		SELECT url, title, text, embedding
		FROM web_chunks
		WHERE embedding IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("query chunks: %w", err)
	}
	defer rows.Close()

	type scored struct {
		url   string
		title string
		text  string
		score float64
	}

	var scoredResults []scored
	for rows.Next() {
		var url, title, text string
		var embedBlob []byte
		if err := rows.Scan(&url, &title, &text, &embedBlob); err != nil {
			continue
		}

		storedEmb := bytesToFloat32Slice(embedBlob)
		score := cosineSimilarity(embedding, storedEmb)
		scoredResults = append(scoredResults, scored{
			url:   url,
			title: title,
			text:  text,
			score: score,
		})
	}

	// Sort by score descending (simple bubble sort for now)
	for i := 0; i < len(scoredResults); i++ {
		for j := i + 1; j < len(scoredResults); j++ {
			if scoredResults[j].score > scoredResults[i].score {
				scoredResults[i], scoredResults[j] = scoredResults[j], scoredResults[i]
			}
		}
	}

	// Limit results
	if len(scoredResults) > limit {
		scoredResults = scoredResults[:limit]
	}

	// Convert to SearchResult
	results := make([]SearchResult, len(scoredResults))
	for i, sr := range scoredResults {
		snippet := sr.text
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		results[i] = SearchResult{
			URL:     sr.url,
			Title:   sr.title,
			Snippet: fmt.Sprintf("%s\n(relevance: %.1f%%)", snippet, sr.score*100),
		}
	}

	return results, nil
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// chunkText splits text into chunks of approximately the given size.
func chunkText(text string, chunkSize int) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	words := strings.Fields(text)
	var current strings.Builder

	for _, word := range words {
		if current.Len()+len(word)+1 > chunkSize && current.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

// isCacheValid checks if the cached page is still valid (less than 24h old).
func isCacheValid(fetchedAt int64) bool {
	return time.Now().Unix()-fetchedAt < 24*60*60
}
