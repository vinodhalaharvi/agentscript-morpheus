package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MCPClient manages connections to MCP servers
type MCPClient struct {
	servers map[string]*MCPServer
	mu      sync.RWMutex
}

// MCPServer represents a connected MCP server process
type MCPServer struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	tools  []MCPTool
	reqID  int64
}

// MCPTool represents an available tool from an MCP server
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// JSON-RPC structures
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewMCPClient creates a new MCP client
func NewMCPClient() *MCPClient {
	return &MCPClient{
		servers: make(map[string]*MCPServer),
	}
}

// Connect starts an MCP server and establishes connection
func (m *MCPClient) Connect(ctx context.Context, name, command string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already connected
	if _, exists := m.servers[name]; exists {
		return fmt.Errorf("server '%s' already connected", name)
	}

	// Parse command
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	// Create command
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Env = append(os.Environ()) // Inherit environment for tokens

	// Setup pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Capture stderr for debugging
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start MCP server: %w", err)
	}

	// Log stderr in background
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				fmt.Fprintf(os.Stderr, "[MCP %s] %s", name, string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	}()

	server := &MCPServer{
		name:   name,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		reqID:  0,
	}

	// Give server a moment to start
	time.Sleep(500 * time.Millisecond)

	// Initialize connection
	if err := server.initialize(); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("failed to initialize MCP server: %w", err)
	}

	// Get available tools
	tools, err := server.listTools()
	if err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("failed to list tools: %w", err)
	}
	server.tools = tools

	m.servers[name] = server
	return nil
}

// initialize sends the initialize request to the MCP server
func (s *MCPServer) initialize() error {
	// Many MCP servers work without full init handshake
	// Skip init and go straight to listing tools
	return nil
}

// listTools gets available tools from the MCP server
func (s *MCPServer) listTools() ([]MCPTool, error) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      atomic.AddInt64(&s.reqID, 1),
		Method:  "tools/list",
		Params:  map[string]interface{}{},
	}

	resp, err := s.sendRequest(req)
	if err != nil {
		// Try alternative method name
		req.ID = atomic.AddInt64(&s.reqID, 1)
		req.Method = "listTools"
		resp, err = s.sendRequest(req)
		if err != nil {
			// Try without params
			req.ID = atomic.AddInt64(&s.reqID, 1)
			req.Method = "tools/list"
			req.Params = nil
			resp, err = s.sendRequest(req)
			if err != nil {
				return nil, err
			}
		}
	}

	var result struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools: %w", err)
	}

	return result.Tools, nil
}

// sendRequest sends a JSON-RPC request and waits for response
func (s *MCPServer) sendRequest(req jsonRPCRequest) (*jsonRPCResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Debug: show what we're sending
	fmt.Fprintf(os.Stderr, "[MCP DEBUG] Sending: %s\n", string(data))

	// Send request
	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read response
	line, err := s.stdout.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Debug: show what we received
	fmt.Fprintf(os.Stderr, "[MCP DEBUG] Received: %s\n", line)

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	return &resp, nil
}

// CallTool invokes a tool on an MCP server
func (m *MCPClient) CallTool(ctx context.Context, serverName, toolName, argsJSON string) (string, error) {
	m.mu.RLock()
	server, exists := m.servers[serverName]
	m.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("server '%s' not connected. Use mcp_connect first", serverName)
	}

	// Debug: show raw argsJSON
	fmt.Fprintf(os.Stderr, "[MCP DEBUG] argsJSON raw: %q\n", argsJSON)

	// Parse arguments - must be valid JSON object
	var args map[string]interface{}
	if argsJSON != "" {
		// Trim any whitespace
		argsJSON = strings.TrimSpace(argsJSON)

		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			fmt.Fprintf(os.Stderr, "[MCP DEBUG] JSON parse error: %v\n", err)
			return "", fmt.Errorf("invalid JSON arguments: %w", err)
		}
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      atomic.AddInt64(&server.reqID, 1),
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	resp, err := server.sendRequest(req)
	if err != nil {
		return "", err
	}

	// Parse result
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		// Return raw result
		return string(resp.Result), nil
	}

	// Combine text content
	var texts []string
	for _, c := range result.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}

	return strings.Join(texts, "\n"), nil
}

// ListTools returns available tools for a server
func (m *MCPClient) ListTools(serverName string) ([]MCPTool, error) {
	m.mu.RLock()
	server, exists := m.servers[serverName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("server '%s' not connected", serverName)
	}

	return server.tools, nil
}

// ListServers returns all connected servers
func (m *MCPClient) ListServers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for name := range m.servers {
		names = append(names, name)
	}
	return names
}

// Close shuts down all MCP server connections
func (m *MCPClient) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, server := range m.servers {
		server.stdin.Close()
		server.cmd.Process.Kill()
	}
	m.servers = make(map[string]*MCPServer)
}

// Disconnect closes a specific server connection
func (m *MCPClient) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	server, exists := m.servers[name]
	if !exists {
		return fmt.Errorf("server '%s' not connected", name)
	}

	server.stdin.Close()
	server.cmd.Process.Kill()
	delete(m.servers, name)
	return nil
}
