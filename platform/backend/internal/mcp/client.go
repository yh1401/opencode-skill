package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client 极简 MCP streamable-http 客户端（JSON-RPC 2.0）。
// 覆盖 initialize / notifications/initialized / tools/list / tools/call，
// 处理 Mcp-Session-Id 与 text/event-stream 两种响应形态。
type Client struct {
	Name string
	URL  string

	http *http.Client

	mu        sync.Mutex
	sessionID string
	lastID    int64
	ready     bool
}

func NewClient(name, url string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Client{Name: name, URL: url, http: &http.Client{Timeout: timeout}}
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("上游调用错误 %d: %s", e.Code, e.Message) }

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (c *Client) nextID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastID++
	return c.lastID
}

func (c *Client) post(ctx context.Context, payload any) (*rpcResponse, http.Header, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	c.mu.Lock()
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil, resp.Header, nil
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, resp.Header, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return parseSSE(resp.Body)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, resp.Header, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, resp.Header, nil
	}
	var out rpcResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, resp.Header, fmt.Errorf("解析上游响应失败: %w", err)
	}
	return &out, resp.Header, nil
}

// parseSSE 读取 data: 行，返回首个带 result/error 的消息。
func parseSSE(r io.Reader) (*rpcResponse, http.Header, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		if strings.TrimSpace(line) != "" {
			continue
		}
		if data.Len() == 0 {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(data.String()), &resp); err != nil {
			data.Reset()
			continue
		}
		data.Reset()
		if resp.Result != nil || resp.Error != nil {
			return &resp, nil, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	return nil, nil, fmt.Errorf("SSE 流中未收到 JSON-RPC 结果")
}

// Initialize 建立会话（幂等）。
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	if c.ready {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	init := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "aiops-platform", "version": "0.1.0"},
		},
	}
	resp, _, err := c.post(ctx, init)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("initialize 无响应")
	}
	if resp.Error != nil {
		return resp.Error
	}

	// notifications/initialized 无 id，服务端通常回 202
	_, _, _ = c.post(ctx, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})

	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
	return nil
}

// Tool MCP 工具描述（原始形态，与 domain.MCPTool 对齐）。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	resp, _, err := c.post(ctx, map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("tools/list 无响应")
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	resp, _, err := c.post(ctx, map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID(),
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("tools/call 无响应")
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}
