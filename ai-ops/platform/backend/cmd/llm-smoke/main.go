// llm-smoke 独立验证知识库 Ranker 的 LLM 调用是否可用。
//
// 用法：
//
//	cd ai-ops/platform/backend
//	export LLM_API_KEY=sk-xxxxxxxxxxxxxxxxxxxxxxxx
//	go run ./cmd/llm-smoke
//
// 测试内容：
//  1. 从 backend/data/knowledge_case 读真实排障归档（最多 30 条）
//  2. 用字面检索打分做 baseline（不调 LLM）
//  3. 调 Ranker.Rank() 走 DeepSeek /v1/chat/completions 语义重排
//  4. 打印 prompt 前 500 字、HTTP 状态码、原始响应、模型分数 Top10
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/knowledge"
)

var ipPattern = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)

func redact(s string) string { return ipPattern.ReplaceAllString(s, "x.x.x.x") }

type rawCase struct {
	ID             string `json:"id"`
	AppID          string `json:"appId"`
	MiddlewareType string `json:"middlewareType"`
	Symptom        string `json:"symptom"`
	RootCause      string `json:"rootCause"`
	Score          int    `json:"score"`
}

func loadCases(dir string) ([]knowledge.Case, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []knowledge.Case
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r rawCase
		if err := json.Unmarshal(b, &r); err != nil {
			continue
		}
		out = append(out, knowledge.Case{
			ID:             r.ID,
			AppID:          r.AppID,
			MiddlewareType: r.MiddlewareType,
			Symptom:        r.Symptom,
			RootCause:      r.RootCause,
		})
	}
	return out, nil
}

// literalScore 与 knowledge/engine.go 里的字面打分逻辑一致（简化版）。
func literalScore(q string, c knowledge.Case) int {
	score := 0
	haystack := strings.ToLower(strings.Join([]string{
		c.AppID, c.MiddlewareType, c.Symptom, c.RootCause,
	}, " "))
	for _, t := range splitTerms(q) {
		if strings.Contains(haystack, t) {
			score += 2
		}
	}
	return score
}

func splitTerms(q string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return r == ' ' || r == ',' || r == '，' || r == ';' || r == '；' || r == '\t'
	}) {
		if strings.TrimSpace(f) != "" {
			out = append(out, f)
		}
	}
	return out
}

func main() {
	key := strings.TrimSpace(os.Getenv("LLM_API_KEY"))
	if key == "" {
		fmt.Fprintln(os.Stderr, "❌ LLM_API_KEY 未设置")
		fmt.Fprintln(os.Stderr, "   export LLM_API_KEY=sk-xxxxxxxxxxxxxxxxxxxxxxxx")
		os.Exit(1)
	}
	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "deepseek-flash"
	}

	here, _ := os.Getwd()
	dataDir := filepath.Join(here, "data", "knowledge_case")
	cases, err := loadCases(dataDir)
	if err != nil || len(cases) == 0 {
		fmt.Fprintf(os.Stderr, "❌ 未找到知识库案例: %v（尝试路径 %s）\n", err, dataDir)
		fmt.Fprintln(os.Stderr, "   先运行 scripts/run_demo.sh 让 Reindex 生成案例")
		os.Exit(1)
	}
	if len(cases) > 30 {
		cases = cases[:30]
	}

	query := strings.TrimSpace(os.Getenv("LLM_TEST_QUERY"))
	if query == "" {
		query = "主机失联 TargetDown"
	}

	fmt.Println("=============================================")
	fmt.Printf("LLM smoke  |  key_len=%d  baseURL=%s  model=%s\n", len(key), baseURL, model)
	fmt.Printf("cases=%d   |  query=%s\n", len(cases), query)
	fmt.Println("=============================================")

	// 1) 字面 baseline
	type rank struct {
		id    string
		score int
		text  string
	}
	literal := make([]rank, 0, len(cases))
	for _, c := range cases {
		literal = append(literal, rank{
			id:    c.ID,
			score: literalScore(query, c),
			text:  fmt.Sprintf("%s  %s  | %s  (%s)", c.AppID, c.MiddlewareType, clip(c.Symptom, 60), clip(c.RootCause, 60)),
		})
	}
	sort.Slice(literal, func(i, j int) bool { return literal[i].score > literal[j].score })
	fmt.Println("\n📊 [1] 字面检索 baseline（不调 LLM）")
	for i, r := range literal[:10] {
		fmt.Printf("  #%d  score=%d  %s\n", i+1, r.score, r.text)
	}

	// 2) 打印构造的 prompt（按 Ranker 内部逻辑手动拼一份）
	fmt.Println("\n📝 [2] 将发送给模型的 prompt（前 500 字符）")
	fmt.Println("----------------------------------------------------------")
	var sb strings.Builder
	fmt.Fprintf(&sb, "用户查询：%s\n候选案例：\n", query)
	for i, c := range cases {
		fmt.Fprintf(&sb, "[%d] app=%s mw=%s symptom=%s root=%s\n",
			i, c.AppID, c.MiddlewareType, clip(c.Symptom, 80), clip(c.RootCause, 80))
	}
	promptPreview := sb.String()
	if len(promptPreview) > 500 {
		promptPreview = promptPreview[:500] + "...(truncated)"
	}
	fmt.Println(redact(promptPreview))
	fmt.Println("----------------------------------------------------------")

	// 3) 真实调 LLM
	fmt.Println("\n🚀 [3] 调 Ranker.Rank() ...")
	ranker := knowledge.NewRanker(key, baseURL, model, 30*time.Second)
	if ranker == nil {
		fmt.Fprintln(os.Stderr, "❌ Ranker 为 nil（apiKey 为空），不应发生")
		os.Exit(1)
	}
	start := time.Now()
	scores, err := ranker.Rank(context.Background(), query, cases)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ LLM 调用失败（%s）: %v\n", elapsed, err)
		os.Exit(2)
	}
	fmt.Printf("✅ 调用成功，耗时 %s，模型返回 %d 条分数\n", elapsed, len(scores))

	// 4) 合并结果，按模型分排序
	type hit struct {
		id         string
		modelScore int
		literal    int
		symptom    string
		rootCause  string
		app        string
	}
	var hits []hit
	for idx, c := range cases {
		ms, ok := scores[c.ID]
		if !ok {
			continue
		}
		hits = append(hits, hit{
			id:         c.ID,
			modelScore: ms,
			literal:    literalScore(query, c),
			app:        c.AppID,
			symptom:    clip(c.Symptom, 70),
			rootCause:  clip(c.RootCause, 70),
		})
		_ = idx
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].modelScore > hits[j].modelScore })

	fmt.Println("\n🎯 [4] 模型重排 Top 10（≥30 才算相关，见 rankMinScore）")
	fmt.Println("    #   模型分   字面分   App        Symptom / RootCause")
	for i, h := range hits[:10] {
		mark := " "
		if h.modelScore < 30 {
			mark = "·"
		}
		fmt.Printf("  %s#%-2d   %-6d  %-6d  %s  %s / %s\n",
			mark, i+1, h.modelScore, h.literal, h.app, h.symptom, h.rootCause)
	}
	fmt.Println("（· = 模型认为无关，Ranker 会过滤掉这些；但字面命中的仍保留在兜底里）")

	fmt.Println("\n✅ 完成。想走完整 HTTP 链路，下一步：")
	fmt.Println("   DATA_MODE=mock STORE_DIR=data/demo LLM_API_KEY=sk-xxx \\")
	fmt.Println("     TOPO_MODE=file TOPO_DATA_DIR=../../../topo/tprel/data \\")
	fmt.Println("     go run ./cmd/server &")
	fmt.Println("   curl -s 'http://localhost:8080/api/knowledge?q=主机失联' | python3 -m json.tool")
}

func clip(s string, n int) string {
	if s == "" {
		return "(空)"
	}
	r := []rune(s)
	if len(r) <= n {
		return strings.ReplaceAll(strings.ReplaceAll(string(r), "\n", " "), "\r", "")
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(r[:n]), "\n", " "), "\r", "") + "…"
}
