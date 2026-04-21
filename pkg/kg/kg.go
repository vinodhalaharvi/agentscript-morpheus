// Package kg provides a Knowledge Graph + GraphRAG pipeline for AgentScript.
// Uses Apache AGE (Cypher in Postgres) + pgvector for hybrid retrieval.
// Bring your own Postgres (with AGE + pgvector) and LLM server (Ollama/vLLM).
package kg

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

	"github.com/vinodhalaharvi/agentscript/pkg/plugin"
)

// Config holds KG connection settings
type Config struct {
	PostgresURL string // postgres://user:pass@host:port/db
	LLMURL      string // http://localhost:11434 (Ollama)
	LLMModel    string // e.g. "qwen2.5-coder:7b"
	EmbedModel  string // model for embeddings (defaults to LLMModel)
	GraphName   string // AGE graph name (default "knowledge")
	TopK        int    // vector search results (default 5)
	MaxHops     int    // max graph traversal depth (default 3)
}

// Client is the Knowledge Graph plugin client
type Client struct {
	cfg        Config
	db         *sql.DB
	httpClient *http.Client
}

// NewClient creates a new KG client
func NewClient(cfg Config) *Client {
	if cfg.GraphName == "" {
		cfg.GraphName = "knowledge"
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.MaxHops <= 0 {
		cfg.MaxHops = 3
	}
	if cfg.EmbedModel == "" {
		cfg.EmbedModel = cfg.LLMModel
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Connect establishes Postgres connection and ensures AGE + pgvector + schema exist
func (c *Client) Connect(ctx context.Context) error {
	db, err := sql.Open("postgres", c.cfg.PostgresURL)
	if err != nil {
		return fmt.Errorf("kg: failed to open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("kg: failed to ping postgres: %w", err)
	}
	c.db = db

	// Ensure extensions
	if _, err := c.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS age"); err != nil {
		return fmt.Errorf("kg: failed to create AGE extension: %w", err)
	}
	if _, err := c.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("kg: failed to create vector extension: %w", err)
	}

	// Load AGE and set search path
	c.db.ExecContext(ctx, "LOAD 'age'")
	c.db.ExecContext(ctx, `SET search_path = ag_catalog, "$user", public`)

	// Create graph if not exists
	var exists bool
	c.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM ag_catalog.ag_graph WHERE name = $1)",
		c.cfg.GraphName).Scan(&exists)
	if !exists {
		_, err := c.db.ExecContext(ctx,
			fmt.Sprintf("SELECT create_graph('%s')", c.cfg.GraphName))
		if err != nil {
			return fmt.Errorf("kg: failed to create graph: %w", err)
		}
	}

	// Create entity embeddings table for hybrid retrieval
	c.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS kg_entity_embeddings (
			entity_name TEXT PRIMARY KEY,
			entity_type TEXT,
			embedding   VECTOR(4096),
			properties  JSONB DEFAULT '{}',
			created_at  TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	c.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS kg_entity_embed_idx
		ON kg_entity_embeddings USING hnsw (embedding vector_cosine_ops)
	`)

	// Create text chunks table for supporting context
	c.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS kg_chunks (
			id          BIGSERIAL PRIMARY KEY,
			entity_name TEXT,
			source_table TEXT,
			content     TEXT NOT NULL,
			embedding   VECTOR(4096),
			created_at  TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	c.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS kg_chunks_embed_idx
		ON kg_chunks USING hnsw (embedding vector_cosine_ops)
	`)
	c.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS kg_chunks_entity_idx
		ON kg_chunks (entity_name)
	`)

	return nil
}

// Close closes the database connection
func (c *Client) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// primeConn loads AGE and sets search path — call before any Cypher query
func (c *Client) primeConn(ctx context.Context) {
	c.db.ExecContext(ctx, "LOAD 'age'")
	c.db.ExecContext(ctx, `SET search_path = ag_catalog, "$user", public`)
}

// cypher executes a Cypher query via AGE and returns rows
func (c *Client) cypher(ctx context.Context, query string, columns string) (*sql.Rows, error) {
	c.primeConn(ctx)
	sql := fmt.Sprintf(
		"SELECT * FROM cypher('%s', $$ %s $$) AS (%s)",
		c.cfg.GraphName, query, columns)
	return c.db.QueryContext(ctx, sql)
}

// Extract discovers schema from tables and uses LLM to extract entities and relationships
func (c *Client) Extract(ctx context.Context, tables []string) (*ExtractResult, error) {
	result := &ExtractResult{}

	for _, table := range tables {
		fmt.Printf("   📊 Extracting entities from %s...\n", table)

		// Discover columns
		cols, err := c.discoverColumns(ctx, table)
		if err != nil {
			result.Errors++
			continue
		}

		// Sample rows for entity extraction
		colNames := make([]string, len(cols))
		for i, col := range cols {
			colNames[i] = col.Name
		}

		query := fmt.Sprintf("SELECT %s FROM %s LIMIT 100",
			strings.Join(colNames, ", "), table)
		rows, err := c.db.QueryContext(ctx, query)
		if err != nil {
			result.Errors++
			continue
		}

		var sampleData []map[string]interface{}
		for rows.Next() {
			values := make([]interface{}, len(colNames))
			ptrs := make([]interface{}, len(colNames))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				continue
			}
			row := make(map[string]interface{})
			for i, col := range colNames {
				if values[i] != nil {
					row[col] = fmt.Sprintf("%v", values[i])
				}
			}
			sampleData = append(sampleData, row)
		}
		rows.Close()

		if len(sampleData) == 0 {
			continue
		}

		// Ask LLM to extract entities and relationships
		sampleJSON, _ := json.Marshal(sampleData[:min(10, len(sampleData))])
		prompt := fmt.Sprintf(`Extract entities and relationships from this database table.
Table: %s
Columns: %s
Sample data (first 10 rows):
%s

Return a JSON object with:
{
  "entities": [{"name": "...", "type": "...", "properties": {...}}],
  "relationships": [{"from": "...", "to": "...", "type": "..."}]
}

Entity names should be meaningful values from the data (names, IDs, categories).
Relationship types should be uppercase like PLACED_ORDER, BELONGS_TO, SUPPLIED_BY.
Return ONLY valid JSON, no explanation.`, table, strings.Join(colNames, ", "), string(sampleJSON))

		response, err := c.generate(ctx, prompt)
		if err != nil {
			result.Errors++
			continue
		}

		// Parse LLM response
		extracted, err := parseExtraction(response)
		if err != nil {
			result.Errors++
			continue
		}

		// Create nodes in AGE
		for _, entity := range extracted.Entities {
			propsJSON, _ := json.Marshal(entity.Properties)
			cypherQ := fmt.Sprintf(
				"MERGE (n:%s {name: '%s'}) SET n.properties = '%s', n.source_table = '%s'",
				sanitizeLabel(entity.Type), sanitizeCypher(entity.Name),
				sanitizeCypher(string(propsJSON)), table)
			c.cypher(ctx, cypherQ, "v agtype")
			result.Nodes++

			// Generate and store embedding for the entity
			text := fmt.Sprintf("%s: %s (%s)", entity.Type, entity.Name, string(propsJSON))
			embedding, err := c.embed(ctx, text)
			if err == nil {
				c.db.ExecContext(ctx, `
					INSERT INTO kg_entity_embeddings (entity_name, entity_type, embedding, properties)
					VALUES ($1, $2, $3::vector, $4)
					ON CONFLICT (entity_name) DO UPDATE SET embedding = $3::vector, properties = $4`,
					entity.Name, entity.Type, vectorString(embedding), propsJSON)
			}
		}

		// Create edges in AGE
		for _, rel := range extracted.Relationships {
			cypherQ := fmt.Sprintf(
				"MATCH (a {name: '%s'}), (b {name: '%s'}) MERGE (a)-[:%s]->(b)",
				sanitizeCypher(rel.From), sanitizeCypher(rel.To),
				sanitizeLabel(rel.Type))
			c.cypher(ctx, cypherQ, "v agtype")
			result.Edges++
		}

		// Store text chunks for supporting context
		for _, row := range sampleData {
			var parts []string
			for k, v := range row {
				parts = append(parts, fmt.Sprintf("%s: %v", k, v))
			}
			chunkText := strings.Join(parts, ", ")
			embedding, err := c.embed(ctx, chunkText)
			if err == nil {
				c.db.ExecContext(ctx, `
					INSERT INTO kg_chunks (entity_name, source_table, content, embedding)
					VALUES ($1, $2, $3, $4::vector)`,
					table, table, chunkText, vectorString(embedding))
				result.Chunks++
			}
		}

		fmt.Printf("   ✅ %s: %d entities, %d relationships\n",
			table, len(extracted.Entities), len(extracted.Relationships))
	}

	return result, nil
}

// Query performs natural language graph query
func (c *Client) Query(ctx context.Context, question string) (string, error) {
	// Ask LLM to generate Cypher from the question
	schema, _ := c.getSchema(ctx)
	prompt := fmt.Sprintf(`You are a Cypher query generator for Apache AGE in PostgreSQL.
Graph name: %s
Schema:
%s

Translate this question into a Cypher MATCH/RETURN query:
"%s"

Return ONLY the Cypher query, no explanation. Use variable-length paths [*1..%d] when exploring connections.`,
		c.cfg.GraphName, schema, question, c.cfg.MaxHops)

	cypherQuery, err := c.generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("kg: failed to generate Cypher: %w", err)
	}
	cypherQuery = cleanCypher(cypherQuery)

	// Execute the Cypher query
	fmt.Printf("   🔍 Cypher: %s\n", strings.TrimSpace(cypherQuery))
	rows, err := c.cypher(ctx, cypherQuery, "result agtype")
	if err != nil {
		// Fallback to hybrid retrieval on Cypher error
		fmt.Printf("   ⚠️  Cypher failed, falling back to hybrid retrieval\n")
		return c.Hybrid(ctx, question)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err == nil {
			results = append(results, r)
		}
	}

	if len(results) == 0 {
		return c.Hybrid(ctx, question)
	}

	// Summarize with LLM
	return c.generate(ctx, fmt.Sprintf(
		"Answer this question based on these graph query results.\nQuestion: %s\nResults:\n%s\n\nProvide a clear answer citing the data.",
		question, strings.Join(results, "\n")))
}

// Hybrid performs hybrid retrieval: vectors find seeds, graph expands, text fills
func (c *Client) Hybrid(ctx context.Context, question string) (string, error) {
	// 1. Embed question
	qvec, err := c.embed(ctx, question)
	if err != nil {
		return "", fmt.Errorf("kg: failed to embed question: %w", err)
	}

	// 2. Vector search: find seed entities
	rows, err := c.db.QueryContext(ctx, `
		SELECT entity_name, entity_type,
		       1 - (embedding <=> $1::vector) as similarity
		FROM kg_entity_embeddings
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2`, vectorString(qvec), c.cfg.TopK)
	if err != nil {
		return "", fmt.Errorf("kg: vector search failed: %w", err)
	}

	var seeds []string
	var seedInfo []string
	for rows.Next() {
		var name, etype string
		var sim float64
		rows.Scan(&name, &etype, &sim)
		seeds = append(seeds, name)
		seedInfo = append(seedInfo, fmt.Sprintf("%s:%s (%.2f)", etype, name, sim))
	}
	rows.Close()

	if len(seeds) == 0 {
		return "No relevant entities found in knowledge graph.", nil
	}
	fmt.Printf("   🌱 Seeds: %s\n", strings.Join(seedInfo, ", "))

	// 3. Graph expansion: find neighborhood around seeds
	seedList := "'" + strings.Join(seeds, "','") + "'"
	cypherQ := fmt.Sprintf(`
		MATCH (s)-[r]-(o)
		WHERE s.name IN [%s]
		RETURN s.name, type(r), o.name`, seedList)

	tripleRows, err := c.cypher(ctx, cypherQ, "subject agtype, predicate agtype, object agtype")
	var triples []string
	if err == nil {
		for tripleRows.Next() {
			var s, p, o string
			tripleRows.Scan(&s, &p, &o)
			triples = append(triples, fmt.Sprintf("(%s)-[%s]->(%s)",
				cleanAgtype(s), cleanAgtype(p), cleanAgtype(o)))
		}
		tripleRows.Close()
	}
	fmt.Printf("   🔗 Graph facts: %d triples\n", len(triples))

	// 4. Text chunks: pull supporting text linked to seeds
	chunkRows, err := c.db.QueryContext(ctx, `
		SELECT content FROM kg_chunks
		WHERE entity_name = ANY($1)
		ORDER BY embedding <=> $2::vector
		LIMIT $3`, seedsToArray(seeds), vectorString(qvec), c.cfg.TopK)

	var chunks []string
	if err == nil {
		for chunkRows.Next() {
			var content string
			chunkRows.Scan(&content)
			chunks = append(chunks, content)
		}
		chunkRows.Close()
	}

	// 5. Build grounded prompt
	var contextParts []string
	if len(triples) > 0 {
		contextParts = append(contextParts,
			"GRAPH FACTS:\n"+strings.Join(triples, "\n"))
	}
	if len(chunks) > 0 {
		contextParts = append(contextParts,
			"SUPPORTING TEXT:\n"+strings.Join(chunks, "\n---\n"))
	}

	fullContext := strings.Join(contextParts, "\n\n")
	return c.generate(ctx, fmt.Sprintf(
		"Answer this question based ONLY on the provided facts and text. Show your reasoning path.\n\n%s\n\nQUESTION: %s",
		fullContext, question))
}

// Path finds shortest path between two entities
func (c *Client) Path(ctx context.Context, from, to string) (string, error) {
	cypherQ := fmt.Sprintf(`
		MATCH (a {name: '%s'}), (b {name: '%s'}),
		      p = shortestPath((a)-[*..%d]-(b))
		RETURN [n IN nodes(p) | n.name] AS chain,
		       [r IN relationships(p) | type(r)] AS rels`,
		sanitizeCypher(from), sanitizeCypher(to), c.cfg.MaxHops)

	fmt.Printf("   🔍 Finding path: %s → %s\n", from, to)
	rows, err := c.cypher(ctx, cypherQ, "chain agtype, rels agtype")
	if err != nil {
		return "", fmt.Errorf("kg: path query failed: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var chain, rels string
		rows.Scan(&chain, &rels)
		paths = append(paths, fmt.Sprintf("Path: %s\nRelationships: %s", chain, rels))
	}

	if len(paths) == 0 {
		return fmt.Sprintf("No path found between '%s' and '%s' within %d hops.",
			from, to, c.cfg.MaxHops), nil
	}

	// Summarize with LLM
	return c.generate(ctx, fmt.Sprintf(
		"Explain this connection path in plain English:\n%s\n\nHow is '%s' connected to '%s'?",
		strings.Join(paths, "\n"), from, to))
}

// Ingest reads unstructured text, extracts entities/relationships via LLM, and upserts into graph
func (c *Client) Ingest(ctx context.Context, text string) (*ExtractResult, error) {
	result := &ExtractResult{}

	prompt := fmt.Sprintf(`Extract all entities and relationships from this text.

TEXT:
%s

Return a JSON object:
{
  "entities": [{"name": "...", "type": "...", "properties": {"description": "..."}}],
  "relationships": [{"from": "...", "to": "...", "type": "RELATIONSHIP_TYPE", "properties": {}}]
}

Rules:
- Entity types should be PascalCase (Person, Company, Product, Event, Location, Concept)
- Relationship types should be UPPER_SNAKE_CASE (WORKS_FOR, LOCATED_IN, PRODUCES)
- Extract ALL entities and relationships, not just the obvious ones
- Return ONLY valid JSON`, text)

	response, err := c.generate(ctx, prompt)
	if err != nil {
		return result, err
	}

	extracted, err := parseExtraction(response)
	if err != nil {
		return result, fmt.Errorf("kg: failed to parse extraction: %w", err)
	}

	// Create nodes
	for _, entity := range extracted.Entities {
		propsJSON, _ := json.Marshal(entity.Properties)
		cypherQ := fmt.Sprintf(
			"MERGE (n:%s {name: '%s'}) SET n.properties = '%s'",
			sanitizeLabel(entity.Type), sanitizeCypher(entity.Name),
			sanitizeCypher(string(propsJSON)))
		c.cypher(ctx, cypherQ, "v agtype")
		result.Nodes++

		// Embed entity
		eText := fmt.Sprintf("%s: %s", entity.Type, entity.Name)
		if desc, ok := entity.Properties["description"]; ok {
			eText += fmt.Sprintf(" - %s", desc)
		}
		embedding, err := c.embed(ctx, eText)
		if err == nil {
			c.db.ExecContext(ctx, `
				INSERT INTO kg_entity_embeddings (entity_name, entity_type, embedding, properties)
				VALUES ($1, $2, $3::vector, $4)
				ON CONFLICT (entity_name) DO UPDATE SET embedding = $3::vector`,
				entity.Name, entity.Type, vectorString(embedding), propsJSON)
		}
	}

	// Create edges
	for _, rel := range extracted.Relationships {
		cypherQ := fmt.Sprintf(
			"MATCH (a {name: '%s'}), (b {name: '%s'}) MERGE (a)-[:%s]->(b)",
			sanitizeCypher(rel.From), sanitizeCypher(rel.To),
			sanitizeLabel(rel.Type))
		c.cypher(ctx, cypherQ, "v agtype")
		result.Edges++
	}

	return result, nil
}

// Status returns graph statistics
func (c *Client) Status(ctx context.Context) (string, error) {
	var sb strings.Builder
	sb.WriteString("Knowledge Graph Status:\n")

	// Node count by label
	rows, err := c.cypher(ctx,
		"MATCH (n) RETURN labels(n) AS label, count(n) AS cnt",
		"label agtype, cnt agtype")
	if err == nil {
		sb.WriteString("\n  Nodes:\n")
		for rows.Next() {
			var label, cnt string
			rows.Scan(&label, &cnt)
			sb.WriteString(fmt.Sprintf("    %-20s %s\n", cleanAgtype(label), cleanAgtype(cnt)))
		}
		rows.Close()
	}

	// Edge count by type
	rows, err = c.cypher(ctx,
		"MATCH ()-[r]->() RETURN type(r) AS rtype, count(r) AS cnt",
		"rtype agtype, cnt agtype")
	if err == nil {
		sb.WriteString("\n  Relationships:\n")
		for rows.Next() {
			var rtype, cnt string
			rows.Scan(&rtype, &cnt)
			sb.WriteString(fmt.Sprintf("    %-20s %s\n", cleanAgtype(rtype), cleanAgtype(cnt)))
		}
		rows.Close()
	}

	// Embedding counts
	var entityCount, chunkCount int
	c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM kg_entity_embeddings").Scan(&entityCount)
	c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM kg_chunks").Scan(&chunkCount)
	sb.WriteString(fmt.Sprintf("\n  Embeddings: %d entities, %d chunks\n", entityCount, chunkCount))

	return sb.String(), nil
}

// getSchema returns a description of the graph schema for LLM prompts
func (c *Client) getSchema(ctx context.Context) (string, error) {
	var sb strings.Builder

	// Get node labels
	rows, err := c.cypher(ctx,
		"MATCH (n) RETURN DISTINCT labels(n) AS label",
		"label agtype")
	if err == nil {
		sb.WriteString("Node labels: ")
		var labels []string
		for rows.Next() {
			var label string
			rows.Scan(&label)
			labels = append(labels, cleanAgtype(label))
		}
		rows.Close()
		sb.WriteString(strings.Join(labels, ", "))
	}

	// Get relationship types
	rows, err = c.cypher(ctx,
		"MATCH ()-[r]->() RETURN DISTINCT type(r) AS rtype",
		"rtype agtype")
	if err == nil {
		sb.WriteString("\nRelationship types: ")
		var rtypes []string
		for rows.Next() {
			var rtype string
			rows.Scan(&rtype)
			rtypes = append(rtypes, cleanAgtype(rtype))
		}
		rows.Close()
		sb.WriteString(strings.Join(rtypes, ", "))
	}

	return sb.String(), nil
}

// --- LLM helpers ---

func (c *Client) embed(ctx context.Context, text string) ([]float64, error) {
	reqBody := map[string]interface{}{
		"model":  c.cfg.EmbedModel,
		"prompt": text,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.cfg.LLMURL+"/api/embeddings", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embedding error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func (c *Client) generate(ctx context.Context, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":  c.cfg.LLMModel,
		"prompt": prompt,
		"stream": false,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.cfg.LLMURL+"/api/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("generate error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	return result.Response, nil
}

// --- Schema discovery ---

type columnInfo struct {
	Name string
	Type string
}

func (c *Client) discoverColumns(ctx context.Context, table string) ([]columnInfo, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_name = $1
		ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []columnInfo
	for rows.Next() {
		var col columnInfo
		rows.Scan(&col.Name, &col.Type)
		cols = append(cols, col)
	}
	return cols, nil
}

// --- Data types ---

// ExtractResult holds stats from extraction
type ExtractResult struct {
	Nodes  int
	Edges  int
	Chunks int
	Errors int
}

func (r *ExtractResult) String() string {
	return fmt.Sprintf("nodes=%d edges=%d chunks=%d errors=%d",
		r.Nodes, r.Edges, r.Chunks, r.Errors)
}

type extraction struct {
	Entities      []extractedEntity       `json:"entities"`
	Relationships []extractedRelationship `json:"relationships"`
}

type extractedEntity struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

type extractedRelationship struct {
	From       string                 `json:"from"`
	To         string                 `json:"to"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

// --- Parsing helpers ---

func parseExtraction(response string) (*extraction, error) {
	response = strings.TrimSpace(response)

	// Strip markdown fences
	if idx := strings.Index(response, "```"); idx >= 0 {
		response = response[idx+3:]
		if strings.HasPrefix(response, "json") {
			response = response[4:]
		}
		if end := strings.Index(response, "```"); end >= 0 {
			response = response[:end]
		}
	}

	response = strings.TrimSpace(response)
	if idx := strings.Index(response, "{"); idx >= 0 {
		response = response[idx:]
	}
	if idx := strings.LastIndex(response, "}"); idx >= 0 {
		response = response[:idx+1]
	}

	var ext extraction
	if err := json.Unmarshal([]byte(response), &ext); err != nil {
		return nil, fmt.Errorf("parse extraction: %w", err)
	}
	return &ext, nil
}

// --- String helpers ---

func sanitizeCypher(s string) string {
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return s
}

func sanitizeLabel(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	// Remove non-alphanumeric except underscore
	var clean strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			clean.WriteRune(r)
		}
	}
	result := clean.String()
	if result == "" {
		return "Entity"
	}
	return result
}

func cleanAgtype(s string) string {
	s = strings.Trim(s, `"`)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	return s
}

func cleanCypher(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```cypher")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func vectorString(v []float64) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%f", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func seedsToArray(seeds []string) string {
	return "{" + strings.Join(seeds, ",") + "}"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Plugin interface ---

// Plugin implements the AgentScript plugin interface
type Plugin struct {
	client *Client
}

// NewPlugin creates the Knowledge Graph plugin
func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string { return "kg" }

func (p *Plugin) Commands() map[string]plugin.CommandFunc {
	return map[string]plugin.CommandFunc{
		"kg_connect": p.connectCmd,
		"kg_extract": p.extractCmd,
		"kg_query":   p.queryCmd,
		"kg_hybrid":  p.hybridCmd,
		"kg_path":    p.pathCmd,
		"kg_cypher":  p.cypherCmd,
		"kg_ingest":  p.ingestCmd,
		"kg_status":  p.statusCmd,
	}
}

func (p *Plugin) connectCmd(ctx context.Context, args []string, input string) (string, error) {
	pgURL, err := plugin.RequireArg(args, 0, "postgres_url")
	if err != nil {
		return "", err
	}
	llmURL := plugin.Coalesce(args, 1, "http://localhost:11434")
	llmModel := plugin.Coalesce(args, 2, "qwen2.5-coder:7b")
	graphName := plugin.Coalesce(args, 3, "knowledge")

	p.client = NewClient(Config{
		PostgresURL: pgURL,
		LLMURL:      llmURL,
		LLMModel:    llmModel,
		EmbedModel:  llmModel,
		GraphName:   graphName,
	})

	if err := p.client.Connect(ctx); err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ Knowledge Graph connected: postgres=%s llm=%s graph=%s",
		pgURL, llmURL, graphName), nil
}

func (p *Plugin) extractCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("kg: not connected. Run kg_connect first")
	}

	if len(args) == 0 {
		return "", fmt.Errorf("kg: specify table names")
	}

	// Support comma-separated or space-separated tables
	var tables []string
	for _, arg := range args {
		tables = append(tables, strings.Split(arg, ",")...)
	}

	fmt.Printf("📊 Extracting knowledge graph from %d tables...\n", len(tables))
	result, err := p.client.Extract(ctx, tables)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ Knowledge Graph extracted: %s", result.String()), nil
}

func (p *Plugin) queryCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("kg: not connected. Run kg_connect first")
	}

	question := plugin.Coalesce(args, 0, input)
	if question == "" {
		return "", fmt.Errorf("kg: no question provided")
	}

	return p.client.Query(ctx, question)
}

func (p *Plugin) hybridCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("kg: not connected. Run kg_connect first")
	}

	question := plugin.Coalesce(args, 0, input)
	if question == "" {
		return "", fmt.Errorf("kg: no question provided")
	}

	return p.client.Hybrid(ctx, question)
}

func (p *Plugin) pathCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("kg: not connected. Run kg_connect first")
	}

	from, err := plugin.RequireArg(args, 0, "from_entity")
	if err != nil {
		return "", err
	}
	to, err := plugin.RequireArg(args, 1, "to_entity")
	if err != nil {
		return "", err
	}

	return p.client.Path(ctx, from, to)
}

func (p *Plugin) cypherCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("kg: not connected. Run kg_connect first")
	}

	question := plugin.Coalesce(args, 0, input)
	if question == "" {
		return "", fmt.Errorf("kg: no question provided")
	}

	// This is essentially kg_query — text-to-cypher
	return p.client.Query(ctx, question)
}

func (p *Plugin) ingestCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("kg: not connected. Run kg_connect first")
	}

	text := plugin.Coalesce(args, 0, input)
	if text == "" {
		return "", fmt.Errorf("kg: no text provided")
	}

	result, err := p.client.Ingest(ctx, text)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ Ingested into knowledge graph: %s", result.String()), nil
}

func (p *Plugin) statusCmd(ctx context.Context, args []string, input string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("kg: not connected. Run kg_connect first")
	}

	return p.client.Status(ctx)
}
