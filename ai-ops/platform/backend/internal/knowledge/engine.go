package knowledge

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"aiops-platform/backend/internal/diagnose"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// Engine 知识库引擎：排障会话归档 + 关键词检索 + 相似案例推荐 + LLM 总结 enrich。
// ranker 非空时，带 q 的检索会先做字面召回，再交给大模型语义重排；
// enricher 复用 ranker 的 LLM 客户端，给 Case 做 AI 结构化总结（手动端点触发）。
type Engine struct {
	diag     *diagnose.Engine
	store    store.Store
	ranker   *Ranker
	enricher *Enricher
}

func New(de *diagnose.Engine, st store.Store, ranker *Ranker) *Engine {
	return &Engine{
		diag:     de,
		store:    st,
		ranker:   ranker,
		enricher: NewEnricher(ranker, st),
	}
}

// Enrich 是否可用（LLM_API_KEY 已注入）。
func (e *Engine) EnrichEnabled() bool { return e != nil && e.enricher != nil && e.enricher.Enabled() }

// EnrichOne 对单条 Case 做 LLM 总结并持久化。静默降级：失败返回 error，不阻塞 Reindex。
func (e *Engine) EnrichOne(ctx context.Context, caseID string) error {
	if e.enricher == nil || !e.enricher.Enabled() {
		return fmt.Errorf("LLM_API_KEY 未配置，enrich 不可用")
	}
	return e.enricher.EnrichOne(ctx, caseID)
}

// EnrichAll 遍历全部 Case 做 LLM 总结，返回 total/enriched/failed 计数。
func (e *Engine) EnrichAll(ctx context.Context, limit int) (total, enriched, failed int) {
	if e.enricher == nil || !e.enricher.Enabled() {
		return 0, 0, 0
	}
	return e.enricher.EnrichAll(ctx, limit)
}

// EnrichFresh 只处理还没 AI 总结过的 Case。
func (e *Engine) EnrichFresh(ctx context.Context) (total, enriched, failed int) {
	if e.enricher == nil || !e.enricher.Enabled() {
		return 0, 0, 0
	}
	return e.enricher.EnrichFresh(ctx)
}

// Reindex 全量归档：把 root_caused 会话与已标注会话写入 knowledge_case（幂等）。
func (e *Engine) Reindex() int {
	tickets := e.ticketIndex()
	n := 0
	for _, sess := range e.diag.List(0) {
		if sess.Status != "root_caused" && sess.Feedback == nil {
			continue
		}
		c := buildCase(sess, tickets[sess.ID])
		if err := e.store.Put("knowledge_case", c.ID, c); err == nil {
			n++
		}
	}
	return n
}

// List 关键字 / 应用过滤后的案例列表（按匹配度、时间倒序）。
func (e *Engine) List(q, appID string, limit int) []Case {
	e.Reindex()
	out := store.ListInto[Case](e.store, "knowledge_case")
	terms := splitTerms(q)
	// 语义重排开启时不按字面分淘汰，否则「换个说法」的查询在召回阶段就被丢光了。
	semantic := strings.TrimSpace(q) != "" && e.ranker.Enabled()
	filtered := make([]Case, 0, len(out))
	for _, c := range out {
		if appID != "" && c.AppID != appID {
			continue
		}
		score := scoreCase(c, terms, appID)
		if len(terms) > 0 && score == 0 && !semantic {
			continue
		}
		c.Score = score
		filtered = append(filtered, c)
	}
	sortCases(filtered)
	if semantic {
		ranked, err := e.rerank(q, filtered)
		switch {
		case err != nil:
			log.Printf("[knowledge] semantic rank failed, fallback to literal: %v", err)
			filtered = positiveOnly(filtered)
		case len(ranked) > 0:
			filtered = ranked
		default:
			// 模型认为都不相关时仍保留字面命中，避免关键词查询被语义判定清空。
			filtered = positiveOnly(filtered)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

// positiveOnly 只保留字面分 > 0 的案例（语义重排不可用时的兜底）。
func positiveOnly(cases []Case) []Case {
	kept := cases[:0]
	for _, c := range cases {
		if c.Score > 0 {
			kept = append(kept, c)
		}
	}
	return kept
}

// rerank 语义重排：字面分已排好序，取前 rankMaxCandidates 条交给大模型打分，
// 命中（≥ rankMinScore）按模型分重排。返回空切片表示模型认为都不相关。
func (e *Engine) rerank(q string, cases []Case) ([]Case, error) {
	candidates := cases
	if len(candidates) > rankMaxCandidates {
		candidates = candidates[:rankMaxCandidates]
	}
	scores, err := e.ranker.Rank(context.Background(), q, candidates)
	if err != nil {
		return nil, err
	}
	hits := make([]Case, 0, len(scores))
	for _, c := range candidates {
		score, ok := scores[c.ID]
		if !ok || score < rankMinScore {
			continue
		}
		c.Score = score
		hits = append(hits, c)
	}
	sortCases(hits)
	return hits, nil
}

func sortCases(cases []Case) {
	sort.Slice(cases, func(i, j int) bool {
		if cases[i].Score != cases[j].Score {
			return cases[i].Score > cases[j].Score
		}
		return cases[i].CreatedAt.After(cases[j].CreatedAt)
	})
}

// Similar 相似案例：同应用加权，按症状/根因关键词重合度排序。
func (e *Engine) Similar(appID, symptom string, limit int) []Case {
	q := strings.TrimSpace(symptom)
	if q == "" && appID == "" {
		return nil
	}
	return e.List(q, "", limit)
}

// Get 按会话 ID 取案例。
func (e *Engine) Get(id string) (*Case, bool) {
	var c Case
	if store.GetInto(e.store, "knowledge_case", id, &c) {
		return &c, true
	}
	return nil, false
}

// Stats 知识库概览。
func (e *Engine) Stats() Stats {
	e.Reindex()
	cases := store.ListInto[Case](e.store, "knowledge_case")
	s := Stats{Total: len(cases)}
	apps := map[string]int{}
	for _, c := range cases {
		if c.AppID != "" {
			apps[c.AppID]++
		}
		if c.Reviewed {
			s.Reviewed++
			if c.Accurate {
				s.Accurate++
			}
		}
	}
	s.Apps = len(apps)
	if s.Reviewed > 0 {
		s.Accuracy = float64(s.Accurate) * 100 / float64(s.Reviewed)
	}
	for id, n := range apps {
		s.TopApps = append(s.TopApps, AppCount{AppID: id, Count: n})
	}
	sort.Slice(s.TopApps, func(i, j int) bool {
		if s.TopApps[i].Count != s.TopApps[j].Count {
			return s.TopApps[i].Count > s.TopApps[j].Count
		}
		return s.TopApps[i].AppID < s.TopApps[j].AppID
	})
	if len(s.TopApps) > 5 {
		s.TopApps = s.TopApps[:5]
	}
	return s
}

// ticketIndex 会话 ID → 工单（只读存储，不触发懒同步）。
func (e *Engine) ticketIndex() map[string]domain.Ticket {
	out := map[string]domain.Ticket{}
	for _, t := range store.ListInto[domain.Ticket](e.store, "ticket") {
		if t.SourceType == "diagnosis" {
			out[t.SourceID] = t
		}
	}
	return out
}

func buildCase(sess domain.DiagnosisSession, t domain.Ticket) Case {
	c := Case{
		ID:             sess.ID,
		Title:          firstNonEmpty(sess.RootCause, sess.Symptom),
		AppID:          sess.AppID,
		MiddlewareType: sess.MiddlewareType,
		Addr:           sess.Addr,
		Symptom:        sess.Symptom,
		RootCause:      sess.RootCause,
		Confidence:     sess.Confidence,
		Status:         sess.Status,
		Steps:          len(sess.Steps),
		CreatedAt:      sess.CreatedAt,
		FinishedAt:     sess.FinishedAt,
	}
	for _, step := range sess.Steps {
		c.Evidence = append(c.Evidence, Evidence{
			No:      step.No,
			Source:  step.Source,
			Title:   step.Title,
			Status:  step.Status,
			Error:   step.Error,
			Latency: step.LatencyMs,
		})
	}
	if t.ID != "" {
		c.TicketID = t.ID
		c.TicketStatus = string(t.Status)
	}
	if sess.Feedback != nil {
		c.Reviewed = true
		c.Accurate = sess.Feedback.Accurate
		c.ActualRootCause = sess.Feedback.ActualRootCause
		if c.ActualRootCause != "" {
			c.RootCause = c.ActualRootCause
		}
	}
	c.Keywords = keywords(c)
	return c
}

// keywords 抽取检索词：应用 + 中间件 + 症状/根因中的英文数字片段（中文整句不做分词）。
func keywords(c Case) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] {
			return
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	add(c.AppID)
	add(c.MiddlewareType)
	for _, f := range strings.FieldsFunc(c.Symptom+" "+c.RootCause, func(r rune) bool {
		return r == ' ' || r == '，' || r == ',' || r == '；' || r == ';' || r == '/' || r == '|' || r == '。'
	}) {
		if len([]rune(f)) >= 2 {
			add(f)
		}
		if len(out) >= 10 {
			break
		}
	}
	return out
}

func splitTerms(q string) []string {
	out := []string{}
	for _, f := range strings.FieldsFunc(q, func(r rune) bool {
		return r == ' ' || r == ',' || r == '，' || r == ';' || r == '；' || r == '\t'
	}) {
		if strings.TrimSpace(f) != "" {
			out = append(out, strings.ToLower(strings.TrimSpace(f)))
		}
	}
	return out
}

// scoreCase 检索打分：应用命中权重最高，其次根因/症状/关键词包含。
// LLM enrich 过的 Case（aiSummary/aiKeywords 非空）额外把 AI 产出也纳入 haystack，
// 让模型总结的同义词/关联词（如 "broker 磁盘过高" 匹配 "RocketMQBrokerHighDisk"）也能被字面召回。
func scoreCase(c Case, terms []string, appID string) int {
	score := 0
	if appID != "" && c.AppID == appID {
		score += 5
	}
	if len(terms) == 0 {
		return score
	}
	haystackParts := []string{
		c.Title, c.Symptom, c.RootCause, c.AppID, c.MiddlewareType, c.Addr,
		strings.Join(c.Keywords, " "),
	}
	// AI 总结字段：enrich 过的 Case 这些字段非空，纳入召回；空字符串自动被 Join 忽略。
	haystackParts = append(haystackParts, c.AISummary, c.AICategory, strings.Join(c.AIKeywords, " "))
	haystack := strings.ToLower(strings.Join(haystackParts, " "))
	for _, t := range terms {
		if strings.Contains(haystack, t) {
			score += 2
		}
		if strings.Contains(strings.ToLower(c.AppID), t) {
			score += 3
		}
	}
	return score
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
