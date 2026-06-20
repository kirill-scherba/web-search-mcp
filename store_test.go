package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kirill-scherba/sqlh"
)

func TestStoreMigrate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store := NewStore(dbPath)
	defer store.Close()

	if !store.Enabled() {
		t.Fatal("store should be enabled")
	}

	// Verify that tables were created and the schema is usable.
	pageURL := "https://example.com/article"
	existing, err := store.PageExists(pageURL)
	if err != nil {
		t.Fatalf("PageExists failed: %v", err)
	}
	if existing != nil {
		t.Fatalf("expected nil for missing page, got %+v", existing)
	}

	// Save a page.
	pageID, err := store.SavePage(&FetchedPage{
		URL:       pageURL,
		Title:     "Example Article",
		Text:      "This is the full text of the article.",
		FetchedAt: 42,
	})
	if err != nil {
		t.Fatalf("SavePage failed: %v", err)
	}
	if pageID <= 0 {
		t.Fatalf("expected positive page id, got %d", pageID)
	}

	// Page should now exist and reflect saved values.
	existing, err = store.PageExists(pageURL)
	if err != nil {
		t.Fatalf("PageExists after SavePage failed: %v", err)
	}
	if existing == nil {
		t.Fatal("expected page to exist after SavePage")
	}
	if existing.Title != "Example Article" {
		t.Errorf("unexpected title: %q", existing.Title)
	}
	if existing.FetchedAt != 42 {
		t.Errorf("unexpected fetched_at: %d", existing.FetchedAt)
	}

	// Upsert the same page and verify ID stability.
	pageID2, err := store.SavePage(&FetchedPage{
		URL:       pageURL,
		Title:     "Updated Title",
		Text:      "Updated text content here.",
		FetchedAt: 100,
	})
	if err != nil {
		t.Fatalf("SavePage upsert failed: %v", err)
	}
	if pageID2 != pageID {
		t.Errorf("upsert changed page id: %d != %d", pageID2, pageID)
	}

	// Save a chunk with a simple embedding.
	embedding := []float32{0.1, 0.2, 0.3, 0.4}
	if err := store.SaveChunk(pageID, pageURL, "Updated Title", 0, "chunk text", embedding); err != nil {
		t.Fatalf("SaveChunk failed: %v", err)
	}

	// Upsert the same chunk and verify it does not error.
	if err := store.SaveChunk(pageID, pageURL, "Updated Title", 0, "chunk text updated", []float32{0.4, 0.3, 0.2, 0.1}); err != nil {
		t.Fatalf("SaveChunk upsert failed: %v", err)
	}

	// Search should return the chunk and preserve the stored embedding.
	results, err := store.SearchSemantic(embedding, 5)
	if err != nil {
		t.Fatalf("SearchSemantic failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].URL != pageURL {
		t.Errorf("unexpected result url: %q", results[0].URL)
	}

	// Raw SQL rows.Scan must not appear outside SearchSemantic.
	// SQLh operations should return typed values without manual Scan.
	rowCount, err := sqlh.Count[WebChunkRecord](store.db)
	if err != nil {
		t.Fatalf("sqlh.Count failed: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("expected 1 chunk in db, got %d", rowCount)
	}
}

func TestStorePageExistsNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "test2.db"))
	defer store.Close()

	if !store.Enabled() {
		t.Fatal("store should be enabled")
	}

	existing, err := store.PageExists("https://notfound.test/404")
	if err != nil {
		t.Fatalf("PageExists failed: %v", err)
	}
	if existing != nil {
		t.Fatalf("expected nil for missing page, got %+v", existing)
	}
}

func TestStoreDisabled(t *testing.T) {
	store := &Store{enabled: false}

	_, err := store.PageExists("https://example.com")
	if err != nil {
		t.Fatalf("PageExists on disabled store should not error: %v", err)
	}
	_, err = store.SavePage(&FetchedPage{URL: "https://example.com", Text: "text"})
	if err != nil {
		t.Fatalf("SavePage on disabled store should not error: %v", err)
	}
	err = store.SaveChunk(1, "https://example.com", "title", 0, "text", []float32{1.0})
	if err != nil {
		t.Fatalf("SaveChunk on disabled store should not error: %v", err)
	}
}

func TestStoreBadPath(t *testing.T) {
	// Use a path that cannot be created as a directory.
	dir := t.TempDir()
	file := filepath.Join(dir, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create blocking file: %v", err)
	}

	store := NewStore(filepath.Join(file, "web_search.db"))
	if store.Enabled() {
		t.Fatal("store should be disabled with invalid path")
	}
}
