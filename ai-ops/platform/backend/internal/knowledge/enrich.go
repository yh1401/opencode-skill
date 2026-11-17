package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/store"
)

// Enricher 案例 LLM 总结：把排障案例压缩成文本，让模型做结构化归纳。
// 调用失败时 Engine 静默降级，不影响 Reindex 与检索。
type Enricher struct {
	ranker *Ranker // 复用 Ranker 的 LLM 客户端：同一 baseURL / apiKey / model / timeout
	store  store.Store
}

// NewEnricher ranker 非 nil 才能 enrich；store 用于回写 Case。
func NewEnricher(ranker *Ranker, store store.Store) *Enricher {
	return &Enricher{ranker: ranker, store: store}
}

// Enabled 返回 LLM 客户端是否可用。
func (e *Enricher) Enabled() bool { return e != nil && e.ranker != nil && e.ranker.Enabled() }

const enrichSystemPrompt = `你是电信运维知识库的故障案例总结专家。根据输入的故障案例，输出严格 JSON：
{
  "summary": 一两句话总结故障经过与处置结果（≤60 字，中文）,
  "category": 根因分类，只能在以下选项里挑一个：基础设施 / 中间件 / 业务代码 / 配置变更 / 资源不足 / 外部依赖 / 待人工确认,
  "keywords": 3-6 个能代表该案例的关键词（英文技术名 + 关键动作词，如 ["rocketmq","broker","disk","扩容"]）,
  "confidence": 0-100 的整数，表达你对根因判断的置信度
}
只输出 JSON，不要解释。关键词不要重复，大小写不敏感。confidence 看证据链强弱：证据充分 80-100，部分证据 50-79，待人工确认 ≤50。`

// enrichOutput LLM 返回的 JSON schema（与 prompt 严格对齐）。
type enrichOutput struct {
	Summary    string   `json:"summary"`
	Category   string   `json:"category"`
	Keywords   []string `json:"keywords"`
	Confidence int      `json:"confidence"`
}

// 允许的 category 枚举（模型偶尔会输出近义词，这里做一次规整）。
var allowedCategories = map[string]string{
	"基础设施":   "基础设施",
	"硬件":      "基础设施",
	"host":     "基础设施",
	"network":  "基础设施",
	"网络":      "基础设施",
	"机器":      "基础设施",

	"中间件":    "中间件",
	"middleware": "中间件",
	"mq":       "中间件",
	"broker":   "中间件",
	"db":       "中间件",
	"mysql":    "中间件",
	"redis":    "中间件",
	"elasticsearch": "中间件",
	"缓存":      "中间件",

	"业务代码":  "业务代码",
	"业务":      "业务代码",
	"bug":      "业务代码",
	"code":     "业务代码",
	"接口":      "业务代码",
	"api":      "业务代码",

	"配置变更":  "配置变更",
	"配置":      "配置变更",
	"config":   "配置变更",
	"deployment": "配置变更",
	"发布":      "配置变更",

	"资源不足":  "资源不足",
	"resource": "资源不足",
	"cpu":      "资源不足",
	"memory":   "资源不足",
	"磁盘":      "资源不足",
	"disk":     "资源不足",
	"load":     "资源不足",
	"high load": "资源不足",

	"外部依赖":  "外部依赖",
	"依赖":      "外部依赖",
	"dependency": "外部依赖",
	"third":    "外部依赖",
	"三方":      "外部依赖",

	"待人工确认": "待人工确认",
	"unknown":  "待人工确认",
	"未确认":    "待人工确认",
	"待确认":    "待人工确认",
	"人工":      "待人工确认",
}

// normalizeCategory 把模型返回的 category 规整到允许枚举，匹配不上就置为 "待人工确认"。
func normalizeCategory(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "待人工确认"
	}
	if fixed, ok := allowedCategories[v]; ok {
		return fixed
	}
	// 宽松匹配：包含关系
	for key, fixed := range allowedCategories {
		if strings.Contains(v, strings.ToLower(key)) {
			return fixed
		}
	}
	return "待人工确认"
}

// EnrichOne 对单条 Case 做 LLM 总结并持久化。静默降级：失败返回 error，Case 不写入 AI 字段。
func (e *Enricher) EnrichOne(ctx context.Context, caseID string) error {
	if !e.Enabled() {
		return fmt.Errorf("llm enrich disabled (LLM_API_KEY empty)")
	}
	var c Case
	if !store.GetInto(e.store, "knowledge_case", caseID, &c) {
		return fmt.Errorf("case %s not found", caseID)
	}
	out, err := e.call(ctx, &c)
	if err != nil {
		return err
	}
	c.AISummary = out.Summary
	c.AICategory = out.Category
	c.AIKeywords = dedupKeywords(out.Keywords)
	if out.Confidence < 0 {
		out.Confidence = 0
	}
	if out.Confidence > 100 {
		out.Confidence = 100
	}
	c.AIConfidence = float64(out.Confidence)
	c.AIRebuildAt = time.Now()
	return e.store.Put("knowledge_case", c.ID, c)
}

// EnrichAll 遍历全部 Case 做总结。failed 计数，silent degradation。
func (e *Enricher) EnrichAll(ctx context.Context, limit int) (total, enriched, failed int) {
	if !e.Enabled() {
		return 0, 0, 0
	}
	cases := store.ListInto[Case](e.store, "knowledge_case")
	total = len(cases)
	if limit > 0 && len(cases) > limit {
		cases = cases[:limit]
	}
	for _, c := range cases {
		if ctx.Err() != nil {
			break
		}
		if err := e.EnrichOne(ctx, c.ID); err != nil {
			failed++
			log.Printf("[knowledge] enrich case=%s failed: %v", c.ID, err)
			continue
		}
		enriched++
	}
	return total, enriched, failed
}

// EnrichFresh 只处理还没 AI 总结过的 Case（AIRebuildAt 为零值）。
func (e *Enricher) EnrichFresh(ctx context.Context) (total, enriched, failed int) {
	if !e.Enabled() {
		return 0, 0, 0
	}
	cases := store.ListInto[Case](e.store, "knowledge_case")
	for _, c := range cases {
		if ctx.Err() != nil {
			break
		}
		total++
		if !c.AIRebuildAt.IsZero() {
			continue // 已经 enrich 过
		}
		if err := e.EnrichOne(ctx, c.ID); err != nil {
			failed++
			log.Printf("[knowledge] enrich case=%s failed: %v", c.ID, err)
			continue
		}
		enriched++
	}
	return total, enriched, failed
}

// call 构造 prompt 并解析模型 JSON 输出。
func (e *Enricher) call(ctx context.Context, c *Case) (*enrichOutput, error) {
	user := buildEnrichPrompt(c)
	content, err := e.ranker.Chat(ctx, enrichSystemPrompt, user)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}
	var out enrichOutput
	if err := parseEnrichJSON(content, &out); err != nil {
		return nil, fmt.Errorf("parse enrich json: %w", err)
	}
	// 规整
	out.Summary = strings.TrimSpace(out.Summary)
	out.Category = normalizeCategory(out.Category)
	out.Keywords = dedupKeywords(out.Keywords)
	if out.Confidence <= 0 {
		out.Confidence = 0
	}
	return &out, nil
}

// buildEnrichPrompt 把 Case 压缩成 prompt。Evidence 每条只保留 No/Title/Status/Error，跳过原始查询。
func buildEnrichPrompt(c *Case) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "【故障案例】\n应用: %s\n中间件: %s\n主机: %s\n症状: %s\n根因: %s\n排障状态: %s\n置信度: %.0f\n",
		strOrDash(c.AppID), strOrDash(c.MiddlewareType), strOrDash(c.Addr),
		strOrDash(c.Symptom), strOrDash(c.RootCause), c.Status, c.Confidence)
	if c.Steps > 0 {
		fmt.Fprintf(&sb, "排障步骤数: %d\n", c.Steps)
	}
	if len(c.Evidence) > 0 {
		sb.WriteString("排障证据链摘要:\n")
		limit := len(c.Evidence)
		if limit > 8 {
			limit = 8 // 最多 8 条，控制 prompt 体积
		}
		for i := 0; i < limit; i++ {
			ev := c.Evidence[i]
			errClip := clip(ev.Error, 80)
			fmt.Fprintf(&sb, "  [%d] %s（%s）%s\n", ev.No, clip(ev.Title, 60), ev.Status, errClip)
		}
	}
	if c.TicketID != "" {
		fmt.Fprintf(&sb, "关联工单: %s（状态=%s）\n", c.TicketID, c.TicketStatus)
	}
	if c.Reviewed && c.Accurate {
		sb.WriteString("人工标注: 已确认准确的根因\n")
	}
	return sb.String()
}

func strOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// parseEnrichJSON 去包裹 + 容忍空对象；解析失败返回 error（静默降级由上层处理）。
func parseEnrichJSON(content string, out *enrichOutput) error {
	s := strings.TrimSpace(content)
	if i := strings.Index(s, "{"); i >= 0 {
		s = s[i:]
	} else {
		return fmt.Errorf("no json object in model response")
	}
	if j := strings.LastIndex(s, "}"); j >= 0 {
		s = s[:j+1]
	}
	return json.Unmarshal([]byte(s), out)
}

// dedupKeywords 去重 + 去空 + 排序。
func dedupKeywords(kw []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(kw))
	for _, k := range kw {
		k = strings.TrimSpace(k)
		if k == "" || seen[strings.ToLower(k)] {
			continue
		}
		seen[strings.ToLower(k)] = true
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func clipEnrich(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return strings.ReplaceAll(strings.ReplaceAll(string(r), "\n", " "), "\r", "")
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(r[:n]), "\n", " "), "\r", "") + "…"
}
