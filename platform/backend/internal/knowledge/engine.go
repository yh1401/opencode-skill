package knowledge

import (
	"sort"
	"strings"

	"aiops-platform/backend/internal/diagnose"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// Engine 知识库引擎：排障会话归档 + 关键词检索 + 相似案例推荐。
type Engine struct {
	diag  *diagnose.Engine
	store store.Store
}

func New(de *diagnose.Engine, st store.Store) *Engine {
	return &Engine{diag: de, store: st}
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
	filtered := make([]Case, 0, len(out))
	for _, c := range out {
		if appID != "" && c.AppID != appID {
			continue
		}
		score := scoreCase(c, terms, appID)
		if len(terms) > 0 && score == 0 {
			continue
		}
		c.Score = score
		filtered = append(filtered, c)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Score != filtered[j].Score {
			return filtered[i].Score > filtered[j].Score
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
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
func scoreCase(c Case, terms []string, appID string) int {
	score := 0
	if appID != "" && c.AppID == appID {
		score += 5
	}
	if len(terms) == 0 {
		return score
	}
	haystack := strings.ToLower(strings.Join([]string{
		c.Title, c.Symptom, c.RootCause, c.AppID, c.MiddlewareType, c.Addr, strings.Join(c.Keywords, " "),
	}, " "))
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
