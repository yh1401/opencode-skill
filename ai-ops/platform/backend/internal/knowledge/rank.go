package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Ranker 公网大模型语义重排：把候选案例压缩成文本，让模型按语义相关性打分。
// 只做重排，不参与归档与工单流转；调用失败时由 Engine 回退到字面检索。
type Ranker struct {
	apiKey  string
	baseURL string
	model   string
	timeout time.Duration
	client  *http.Client
}

// NewRanker apiKey 为空时返回 nil，表示语义重排关闭（完全沿用字面检索）。
func NewRanker(apiKey, baseURL, model string, timeout time.Duration) *Ranker {
	if strings.TrimSpace(apiKey) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Ranker{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
	}
}

func (r *Ranker) Enabled() bool { return r != nil && r.apiKey != "" }

const (
	// rankMinScore 模型分低于此值视为不相关，不出现在结果里。
	rankMinScore = 30
	// rankMaxCandidates 单次送入模型的候选上限，控制 prompt 体积与延迟。
	rankMaxCandidates = 60
)

const rankSystemPrompt = `你是电信运维故障知识库的检索助手。根据用户查询，从候选案例中挑出语义相关的案例并打分。
评分标准：90-100 症状与根因几乎一致；60-89 同一类故障；30-59 弱相关；0-29 不相关。
只输出 JSON，不要解释，格式：{"results":[{"i":0,"score":85}]}，最多 10 条，按 score 降序。`

// Rank 返回 案例ID → 相关性分数（0-100）。空 map 表示模型认为没有相关案例。
func (r *Ranker) Rank(ctx context.Context, query string, cases []Case) (map[string]int, error) {
	if !r.Enabled() || len(cases) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var sb strings.Builder
	fmt.Fprintf(&sb, "用户查询：%s\n候选案例：\n", maskText(strings.TrimSpace(query)))
	for i, c := range cases {
		fmt.Fprintf(&sb, "[%d] app=%s mw=%s symptom=%s root=%s\n",
			i, maskAppID(c.AppID), maskText(clip(c.MiddlewareType, 40)),
			maskText(clip(c.Symptom, 120)), maskText(clip(c.RootCause, 120)))
	}

	content, err := r.chat(ctx, rankSystemPrompt, sb.String())
	if err != nil {
		return nil, err
	}
	results, err := parseRank(content)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(results))
	for _, it := range results {
		if it.Index < 0 || it.Index >= len(cases) {
			continue
		}
		score := it.Score
		if score < 0 {
			score = 0
		}
		if score > 100 {
			score = 100
		}
		out[cases[it.Index].ID] = score
	}
	return out, nil
}

type rankItem struct {
	Index int `json:"i"`
	Score int `json:"score"`
}

type rankResponse struct {
	Results []rankItem `json:"results"`
}

// parseRank 容忍 ```json 代码块与前后多余文本；解析失败返回错误，交由字面检索兜底。
func parseRank(content string) ([]rankItem, error) {
	s := strings.TrimSpace(content)
	if i := strings.Index(s, "{"); i >= 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 {
		s = s[:j+1]
	}
	var resp rankResponse
	if err := json.Unmarshal([]byte(s), &resp); err != nil {
		return nil, fmt.Errorf("parse rank json: %w", err)
	}
	return resp.Results, nil
}

func (r *Ranker) chat(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model":       r.model,
		"temperature": 0,
		"max_tokens":  800,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	// 部分网关不支持 response_format，400 时去掉该字段重试一次。
	content, err := r.doChat(ctx, body, true)
	if err != nil && strings.Contains(err.Error(), "400") {
		content, err = r.doChat(ctx, body, false)
	}
	return content, err
}

func (r *Ranker) doChat(ctx context.Context, body map[string]any, withJSONMode bool) (string, error) {
	payload := make(map[string]any, len(body)+1)
	for k, v := range body {
		payload[k] = v
	}
	if withJSONMode {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		log.Printf("[knowledge] llm rank http %d: %s", resp.StatusCode, clip(string(data), 200))
		return "", fmt.Errorf("llm http %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm empty choices")
	}
	return out.Choices[0].Message.Content, nil
}

var ipPattern = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)

// maskText 脱敏：IP 打码，避免运维网络信息出域。
func maskText(s string) string { return ipPattern.ReplaceAllString(s, "x.x.x.x") }

// maskAppID 应用名做稳定哈希映射，保留同一应用的可区分性。
func maskAppID(app string) string {
	if strings.TrimSpace(app) == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(app))
	return fmt.Sprintf("app_%08x", h.Sum32())
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
