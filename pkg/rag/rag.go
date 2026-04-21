// Package rag provides a Postgres+LLM RAG pipeline for AgentScript.
// Bring your own Postgres (with pgvector) and LLM server (Ollama/vLLM).
// The plugin handles: schema discovery, chunking, embedding, vector search, and LLM Q&A.
package rag

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Config holds RAG connection settings
type Config struct {
	PostgresURL string // postgres://user:pass@host:port/db
	LLMURL      string // http://localhost:11434 (Ollama) or any OpenAI-compatible endpoint
	LLMModel    string // e.g. "qwen2.5-coder:7b" or "nomic-embed-text"
	EmbedModel  string // model for embeddings (defaults to LLMModel)
	BatchSize   int    // rows per batch during indexing (default 1000)
	ChunkSize   int    // max chars per chunk (default 2000)
	TopK        int    // number of results for similarity search (default 5)
}

// Client is the RAG plugin client
type Client struct {
	cfg        Config
	db         *sql.DB
	httpClient *http.Client
	serverType string // "ollama" or "llama"
	embedDim   int    // detected embedding dimension
}

// NewClient creates a new RAG client. Does NOT connect yet — call Connect().
func NewClient(cfg Config) *Client {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 2000
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.EmbedModel == "" {
		cfg.EmbedModel = cfg.LLMModel
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Connect establishes the Postgres connection and ensures pgvector + schema exist
func (c *Client) Connect(ctx context.Context) error {
	db, err := sql.Open("postgres", c.cfg.PostgresURL)
	if err != nil {
		return fmt.Errorf("rag: failed to open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("rag: failed to ping postgres: %w", err)
	}
	c.db = db

	// Ensure pgvector extension
	if _, err := c.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("rag: failed to create vector extension: %w", err)
	}

	// Create embeddings table
	if _, err := c.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS rag_embeddings (
			id BIGSERIAL PRIMARY KEY,
			source_table TEXT NOT NULL,
			source_id TEXT NOT NULL,
			chunk_index INT NOT NULL DEFAULT 0,
			chunk_text TEXT NOT NULL,
			embedding text,
			metadata JSONB DEFAULT '{}',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE(source_table, source_id, chunk_index)
		)
	`); err != nil {
		return fmt.Errorf("rag: failed to create embeddings table: %w", err)
	}

	// Vector index created in ensureVectorSchema() after dimension detection
	return nil
}

// Close closes the database connection
func (c *Client) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// DiscoverSchema returns column names and types for a table
func (c *Client) DiscoverSchema(ctx context.Context, table string) ([]ColumnInfo, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns 
		WHERE table_name = $1 
		ORDER BY ordinal_position
	`, table)
	if err != nil {
		return nil, fmt.Errorf("rag: schema discovery failed: %w", err)
	}
	defer rows.Close()

	var cols []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		if err := rows.Scan(&col.Name, &col.Type, &col.Nullable); err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("rag: table %q not found or has no columns", table)
	}
	return cols, nil
}

// ColumnInfo describes a database column
type ColumnInfo struct {
	Name     string
	Type     string
	Nullable string
}

// isTextColumn returns true for text-like column types
func isTextColumn(colType string) bool {
	colType = strings.ToLower(colType)
	return strings.Contains(colType, "text") ||
		strings.Contains(colType, "char") ||
		strings.Contains(colType, "varchar")
}

// Index indexes a table's rows into rag_embeddings
func (c *Client) Index(ctx context.Context, table string, columns []string) (*IndexResult, error) {
	// Auto-discover columns if none specified
	if len(columns) == 0 {
		schema, err := c.DiscoverSchema(ctx, table)
		if err != nil {
			return nil, err
		}
		for _, col := range schema {
			if isTextColumn(col.Type) {
				columns = append(columns, col.Name)
			}
		}
		if len(columns) == 0 {
			return nil, fmt.Errorf("rag: no text columns found in table %q", table)
		}
	}

	// Find primary key column
	pkCol, err := c.findPrimaryKey(ctx, table)
	if err != nil {
		return nil, err
	}

	// Build SELECT query
	selectCols := append([]string{pkCol}, columns...)
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(selectCols, ", "), table)

	// Count total rows
	var totalRows int
	c.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&totalRows)

	result := &IndexResult{
		Table:     table,
		Columns:   columns,
		TotalRows: totalRows,
		BatchSize: c.cfg.BatchSize,
	}

	// Process in batches
	offset := 0
	for {
		batchQuery := fmt.Sprintf("%s ORDER BY %s LIMIT %d OFFSET %d", query, pkCol, c.cfg.BatchSize, offset)
		rows, err := c.db.QueryContext(ctx, batchQuery)
		if err != nil {
			return result, fmt.Errorf("rag: batch query failed at offset %d: %w", offset, err)
		}

		batchCount := 0
		for rows.Next() {
			// Scan all columns
			values := make([]interface{}, len(selectCols))
			ptrs := make([]interface{}, len(selectCols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return result, fmt.Errorf("rag: scan failed: %w", err)
			}

			// Extract primary key
			sourceID := fmt.Sprintf("%v", values[0])

			// Build chunk text from text columns
			var chunks []string
			metadata := make(map[string]interface{})
			for i, col := range columns {
				val := values[i+1]
				if val != nil {
					text := fmt.Sprintf("%v", val)
					if text != "" {
						chunks = append(chunks, fmt.Sprintf("%s: %s", col, text))
					}
					metadata[col] = text
				}
			}

			if len(chunks) == 0 {
				continue
			}

			chunkText := strings.Join(chunks, "\n")

			// Split into smaller chunks if needed
			textChunks := splitChunks(chunkText, c.cfg.ChunkSize)
			for chunkIdx, chunk := range textChunks {
				// Generate embedding
				embedding, err := c.embed(ctx, chunk)
				if err != nil {
					result.Errors++
					continue
				}
				c.ensureVectorSchema(ctx, len(embedding))

				// Upsert into embeddings table
				metaJSON, _ := json.Marshal(metadata)
				_, err = c.db.ExecContext(ctx, `
					INSERT INTO rag_embeddings (source_table, source_id, chunk_index, chunk_text, embedding, metadata)
					VALUES ($1, $2, $3, $4, $5::vector, $6)
					ON CONFLICT (source_table, source_id, chunk_index) 
					DO UPDATE SET chunk_text = $4, embedding = $5::vector, metadata = $6, created_at = NOW()
				`, table, sourceID, chunkIdx, chunk, vectorString(embedding), metaJSON)
				if err != nil {
					result.Errors++
					continue
				}
				result.Indexed++
			}
			batchCount++
		}
		rows.Close()

		result.BatchesProcessed++
		fmt.Printf("   📊 Indexed batch %d: %d rows (total: %d/%d)\n",
			result.BatchesProcessed, batchCount, offset+batchCount, totalRows)

		if batchCount < c.cfg.BatchSize {
			break // last batch
		}
		offset += c.cfg.BatchSize
	}

	return result, nil
}

// IndexResult holds stats from an indexing run
type IndexResult struct {
	Table            string
	Columns          []string
	TotalRows        int
	Indexed          int
	Errors           int
	BatchSize        int
	BatchesProcessed int
}

func (r *IndexResult) String() string {
	return fmt.Sprintf("table=%s rows=%d indexed=%d errors=%d batches=%d",
		r.Table, r.TotalRows, r.Indexed, r.Errors, r.BatchesProcessed)
}

// Query performs RAG: embed question → vector search → LLM answer
func (c *Client) Query(ctx context.Context, question string, tables ...string) (string, error) {
	// 1. Embed the question
	qEmbed, err := c.embed(ctx, question)
	if err != nil {
		return "", fmt.Errorf("rag: failed to embed question: %w", err)
	}
	c.ensureVectorSchema(ctx, len(qEmbed))

	// 2. Vector similarity search
	tableFilter := ""
	if len(tables) > 0 {
		quoted := make([]string, len(tables))
		for i, t := range tables {
			quoted[i] = fmt.Sprintf("'%s'", t)
		}
		tableFilter = fmt.Sprintf("AND source_table IN (%s)", strings.Join(quoted, ","))
	}

	query := fmt.Sprintf(`
		SELECT source_table, source_id, chunk_text, metadata,
			   1 - (embedding <=> $1::vector) as similarity
		FROM rag_embeddings
		WHERE embedding IS NOT NULL %s
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`, tableFilter)

	rows, err := c.db.QueryContext(ctx, query, vectorString(qEmbed), c.cfg.TopK)
	if err != nil {
		return "", fmt.Errorf("rag: vector search failed: %w", err)
	}
	defer rows.Close()

	var chunks []SearchResult
	for rows.Next() {
		var r SearchResult
		var metaJSON string
		if err := rows.Scan(&r.Table, &r.SourceID, &r.ChunkText, &metaJSON, &r.Similarity); err != nil {
			continue
		}
		json.Unmarshal([]byte(metaJSON), &r.Metadata)
		chunks = append(chunks, r)
	}

	if len(chunks) == 0 {
		return "No relevant data found.", nil
	}

	// 3. Build context from chunks
	var contextBuilder strings.Builder
	contextBuilder.WriteString("Answer the following question based on the provided data.\n\n")
	contextBuilder.WriteString("DATA:\n")
	for i, chunk := range chunks {
		contextBuilder.WriteString(fmt.Sprintf("\n--- Source: %s (id: %s, similarity: %.3f) ---\n",
			chunk.Table, chunk.SourceID, chunk.Similarity))
		contextBuilder.WriteString(chunk.ChunkText)
		if i >= c.cfg.TopK-1 {
			break
		}
	}
	contextBuilder.WriteString(fmt.Sprintf("\n\nQUESTION: %s\n", question))
	contextBuilder.WriteString("\nProvide a clear, concise answer based only on the data above.")

	// 4. Send to LLM for answer
	answer, err := c.generate(ctx, contextBuilder.String())
	if err != nil {
		return "", fmt.Errorf("rag: LLM generation failed: %w", err)
	}

	return answer, nil
}

// SearchResult holds a single vector search result
type SearchResult struct {
	Table      string
	SourceID   string
	ChunkText  string
	Similarity float64
	Metadata   map[string]interface{}
}

// embed calls the LLM server to generate embeddings
// Auto-detects Ollama vs llama.cpp server format
func (c *Client) embed(ctx context.Context, text string) ([]float64, error) {
	if c.serverType == "" {
		c.detectServer(ctx)
	}
	var url string
	var jsonBody []byte
	if c.serverType == "llama" {
		url = c.cfg.LLMURL + "/embedding"
		reqBody := map[string]interface{}{"content": text}
		jsonBody, _ = json.Marshal(reqBody)
	} else {
		url = c.cfg.LLMURL + "/api/embeddings"
		reqBody := map[string]interface{}{"model": c.cfg.EmbedModel, "prompt": text}
		jsonBody, _ = json.Marshal(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embedding error %d: %s", resp.StatusCode, string(body))
	}
	if c.serverType == "llama" {
		var result []struct {
			Embedding json.RawMessage `json:"embedding"`
		}
		if err := json.Unmarshal(body, &result); err != nil || len(result) == 0 {
			return nil, fmt.Errorf("failed to parse llama embedding: %w", err)
		}
		raw := result[0].Embedding
		// Try nested format [[tok1_vec], [tok2_vec], ...] — mean-pool across tokens
		var nested [][]float64
		if err := json.Unmarshal(raw, &nested); err == nil && len(nested) > 0 {
			dim := len(nested[0])
			pooled := make([]float64, dim)
			for _, tok := range nested {
				for i, v := range tok {
					pooled[i] += v
				}
			}
			n := float64(len(nested))
			for i := range pooled {
				pooled[i] /= n
			}
			return pooled, nil
		}
		// Try flat format [0.1, 0.2, ...]
		var flat []float64
		if err := json.Unmarshal(raw, &flat); err == nil {
			return flat, nil
		}
		return nil, fmt.Errorf("cannot parse embedding format")
	}
	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse embedding response: %w", err)
	}
	return result.Embedding, nil
}

// ensureVectorSchema migrates the embedding column to proper vector type
// once we know the actual dimension from the LLM server
func (c *Client) ensureVectorSchema(ctx context.Context, dim int) {
	if c.embedDim > 0 {
		return
	}
	c.embedDim = dim
	fmt.Printf("   📐 Detected embedding dimension: %d\n", dim)
	var colType string
	c.db.QueryRowContext(ctx,
		"SELECT data_type FROM information_schema.columns WHERE table_name='rag_embeddings' AND column_name='embedding'",
	).Scan(&colType)
	if colType == "USER-DEFINED" {
		return // already vector type
	}
	c.db.ExecContext(ctx, "ALTER TABLE rag_embeddings DROP COLUMN IF EXISTS embedding")
	c.db.ExecContext(ctx, fmt.Sprintf(
		"ALTER TABLE rag_embeddings ADD COLUMN embedding vector(%d)", dim))
	c.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS rag_embeddings_vector_idx
		ON rag_embeddings USING hnsw (embedding vector_cosine_ops)
	`)
}

// generate calls the LLM server for text generation
// Auto-detects Ollama vs llama.cpp server format
func (c *Client) generate(ctx context.Context, prompt string) (string, error) {
	if c.serverType == "" {
		c.detectServer(ctx)
	}
	var url string
	var jsonBody []byte
	if c.serverType == "llama" {
		url = c.cfg.LLMURL + "/completion"
		reqBody := map[string]interface{}{"prompt": prompt, "n_predict": 2048, "stream": false}
		jsonBody, _ = json.Marshal(reqBody)
	} else {
		url = c.cfg.LLMURL + "/api/generate"
		reqBody := map[string]interface{}{"model": c.cfg.LLMModel, "prompt": prompt, "stream": false}
		jsonBody, _ = json.Marshal(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("generate request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("generate error %d: %s", resp.StatusCode, string(body))
	}
	if c.serverType == "llama" {
		var result struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("failed to parse response: %w", err)
		}
		return result.Content, nil
	}
	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse generate response: %w", err)
	}
	return result.Response, nil
}

// detectServer probes the LLM server to determine if it's Ollama or llama.cpp
func (c *Client) detectServer(ctx context.Context) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.cfg.LLMURL+"/health", nil)
	resp, err := c.httpClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "status") {
			c.serverType = "llama"
			fmt.Printf("   🔍 Detected llama.cpp server\n")
			return
		}
	}
	c.serverType = "ollama"
	fmt.Printf("   🔍 Detected Ollama server\n")
}

// findPrimaryKey discovers the primary key column for a table
func (c *Client) findPrimaryKey(ctx context.Context, table string) (string, error) {
	var pkCol string
	err := c.db.QueryRowContext(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = $1::regclass AND i.indisprimary
		LIMIT 1
	`, table).Scan(&pkCol)
	if err != nil {
		// Fallback to 'id' if no PK found
		return "id", nil
	}
	return pkCol, nil
}

// splitChunks splits text into chunks of maxSize characters at word boundaries
func splitChunks(text string, maxSize int) []string {
	if len(text) <= maxSize {
		return []string{text}
	}

	var chunks []string
	words := strings.Fields(text)
	var current strings.Builder

	for _, word := range words {
		if current.Len()+len(word)+1 > maxSize && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte(' ')
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return chunks
}

// vectorString converts a float64 slice to pgvector string format: [0.1,0.2,0.3]
func vectorString(v []float64) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%f", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Plugin implements the AgentScript plugin interface
type Plugin struct {
	client *Client
}

// NewPlugin creates the RAG plugin
func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string { return "rag" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"rag_connect": p.connectCmd,
		"rag_index":   p.indexCmd,
		"rag_query":   p.queryCmd,
		"rag_schema":  p.schemaCmd,
		"rag_status":  p.statusCmd,
	}
}

func (p *Plugin) connectCmd(ctx context.Context, args []string, input string) (string, error) {
	pgURL, err := plugin.RequireArg(args, 0, "postgres_url")
	if err != nil {
		return "", err
	}
	llmURL := plugin.Coalesce(args, 1, "http://localhost:11434")
	llmModel := plugin.Coalesce(args, 2, "qwen2.5-coder:7b")

	p.client = NewClient(Config{
		PostgresURL: pgURL,
		LLMURL:      llmURL,
		LLMModel:    llmModel,
		EmbedModel:  llmModel,
	})

	if err := p.client.Connect(ctx); err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ RAG connected: postgres=%s llm=%s model=%s", pgURL, llmURL, llmModel), nil
}

func (p *Plugin) indexCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("rag: not connected. Run rag_connect first")
	}

	table, err := plugin.RequireArg(args, 0, "table_name")
	if err != nil {
		return "", err
	}

	var columns []string
	colArg := plugin.Arg(args, 1)
	if colArg != "" {
		columns = strings.Split(colArg, ",")
	}

	fmt.Printf("📋 Indexing table %q columns=%v...\n", table, columns)
	result, err := p.client.Index(ctx, table, columns)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ Indexed: %s", result.String()), nil
}

func (p *Plugin) queryCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("rag: not connected. Run rag_connect first")
	}

	question := plugin.Coalesce(args, 0, input)
	if question == "" {
		return "", fmt.Errorf("rag: no question provided")
	}

	// Optional table filter
	var tables []string
	tableArg := plugin.Arg(args, 1)
	if tableArg != "" {
		tables = strings.Split(tableArg, ",")
	}

	answer, err := p.client.Query(ctx, question, tables...)
	if err != nil {
		return "", err
	}

	return answer, nil
}

func (p *Plugin) schemaCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("rag: not connected. Run rag_connect first")
	}

	table, err := plugin.RequireArg(args, 0, "table_name")
	if err != nil {
		return "", err
	}

	cols, err := p.client.DiscoverSchema(ctx, table)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Table: %s\n", table))
	for _, col := range cols {
		sb.WriteString(fmt.Sprintf("  %-30s %-20s nullable=%s\n", col.Name, col.Type, col.Nullable))
	}
	return sb.String(), nil
}

func (p *Plugin) statusCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("rag: not connected. Run rag_connect first")
	}

	rows, err := p.client.db.QueryContext(ctx, `
		SELECT source_table, COUNT(*), MAX(created_at)
		FROM rag_embeddings
		GROUP BY source_table
		ORDER BY source_table
	`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var sb strings.Builder
	sb.WriteString("RAG Index Status:\n")
	for rows.Next() {
		var table string
		var count int
		var lastIndexed time.Time
		rows.Scan(&table, &count, &lastIndexed)
		sb.WriteString(fmt.Sprintf("  %-30s %d chunks (last: %s)\n",
			table, count, lastIndexed.Format("2006-01-02 15:04")))
	}
	return sb.String(), nil
}
