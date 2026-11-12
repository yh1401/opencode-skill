package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Result 统一封装 tools/call 的返回，屏蔽三种并存形态：
//
//  1. 标准 MCP envelope：{"content":[{"type":"text","text":"..."}],"structuredContent":{...},"isError":false}
//  2. MCP 服务直接返回业务 JSON：{"code":200,"data":{...}}
//  3. 双重编码：content[0].text 本身仍是一段 JSON 字符串
//
// 调用方只消费 Payload / Text / IsError，不再各自解 content[0].text。
type Result struct {
	Raw     json.RawMessage
	Payload json.RawMessage
	Text    string
	IsError bool
}

// ResultOf 把任意 MCP result 归一为 Result。
func ResultOf(raw json.RawMessage) Result {
	r := Result{Raw: raw}
	body := bytes.TrimSpace(raw)
	if len(body) == 0 {
		return r
	}

	// 裸 JSON 字符串（双重编码）：先解成字符串，内容仍是 JSON 则递归解一层。
	if body[0] == '"' {
		var s string
		if json.Unmarshal(body, &s) == nil {
			inner := bytes.TrimSpace([]byte(s))
			if json.Valid(inner) {
				return ResultOf(inner)
			}
			r.Text = s
			return r
		}
	}

	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if json.Unmarshal(body, &env) == nil &&
		(len(env.Content) > 0 || len(bytes.TrimSpace(env.StructuredContent)) > 0 || env.IsError) {
		r.IsError = env.IsError
		parts := make([]string, 0, len(env.Content))
		for _, c := range env.Content {
			if strings.EqualFold(c.Type, "text") && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, c.Text)
			}
		}
		if len(bytes.TrimSpace(env.StructuredContent)) > 0 {
			r.Payload = bytes.TrimSpace(env.StructuredContent)
			r.Text = strings.Join(parts, "\n")
			return r
		}
		if len(parts) > 0 {
			text := strings.TrimSpace(strings.Join(parts, "\n"))
			r.Text = text
			if inner := bytes.TrimSpace([]byte(text)); json.Valid(inner) {
				r.Payload = inner
			}
			return r
		}
		return r
	}

	// 直接业务 JSON。
	r.Payload = append(json.RawMessage{}, body...)
	return r
}

// ContentJSON 从 MCP envelope 的多段 text 中提取首个可解析为 JSON 的片段。
// ES MCP 的 list_indices / search 会先返回一行统计文案、再返回 JSON，整段拼接不是合法 JSON，
// 因此不能直接用 ResultOf 的 Payload。
func ContentJSON(raw json.RawMessage) (json.RawMessage, bool) {
	body := bytes.TrimSpace(raw)
	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(body, &env) == nil && len(env.Content) > 0 {
		for _, c := range env.Content {
			text := strings.TrimSpace(c.Text)
			if text != "" && json.Valid([]byte(text)) {
				return json.RawMessage(text), true
			}
		}
		return nil, false
	}
	if r := ResultOf(raw); len(r.Payload) > 0 {
		return r.Payload, true
	}
	return nil, false
}

// Empty 是否没有任何可展示内容。
func (r Result) Empty() bool { return len(r.Payload) == 0 && r.Text == "" }

// Decode 把业务 JSON 解到 v。
func (r Result) Decode(v any) error {
	if len(r.Payload) == 0 {
		return fmt.Errorf("上游返回无可解析的 JSON 内容")
	}
	return json.Unmarshal(r.Payload, v)
}

// Preview 生成放入证据链的截断视图：bytes / data（或 preview）/ truncated / isError。
func (r Result) Preview(max int) map[string]any {
	if max <= 0 {
		max = 4000
	}
	out := map[string]any{"bytes": len(r.Raw)}
	if r.IsError {
		out["isError"] = true
	}
	switch {
	case len(r.Payload) > 0:
		if len(r.Payload) <= max {
			var v any
			if json.Unmarshal(r.Payload, &v) == nil {
				out["data"] = v
			} else {
				out["preview"] = truncate(string(r.Payload), max)
			}
		} else {
			out["truncated"] = true
			out["preview"] = truncate(string(r.Payload), max)
		}
	case r.Text != "":
		if len(r.Text) <= max {
			out["preview"] = r.Text
		} else {
			out["truncated"] = true
			out["preview"] = truncate(r.Text, max)
		}
	}
	return out
}

// DecodeJSON 便捷泛型：解析 MCP result 的业务 JSON。
func DecodeJSON[T any](raw json.RawMessage) (T, error) {
	var v T
	err := ResultOf(raw).Decode(&v)
	return v, err
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[:max], "")
}
