package changes

import (
	"fmt"
	"strings"
	"time"

	"aiops-platform/backend/internal/diagnose"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/topo"
)

// Engine 变更方案引擎：模板匹配 → 风险打分 → 方案存储。
type Engine struct {
	topo  topo.Provider
	diag  *diagnose.Engine
	store store.Store
}

func New(tp topo.Provider, de *diagnose.Engine, st store.Store) *Engine {
	return &Engine{topo: tp, diag: de, store: st}
}

// GeneratePlan 输入变更目标，套模板 + 风险打分 → 生成方案并存档。
func (e *Engine) GeneratePlan(tgt domain.ChangeTarget) (*domain.ChangePlan, error) {
	tpl, ok := GetTemplate(tgt.TemplateID)
	if !ok {
		return nil, fmt.Errorf("模板不存在: %s", tgt.TemplateID)
	}
	if err := e.validateTarget(&tgt); err != nil {
		return nil, err
	}

	risk := e.ValidateRisk(&tgt)

	plan := &domain.ChangePlan{
		ID:           store.NewID("change"),
		TemplateID:   tpl.ID,
		TemplateName: tpl.Name,
		Target:       tgt,
		Steps:        tpl.Steps,
		Risk:         risk,
		CreatedAt:    time.Now(),
		CreatedBy:    "default",
	}

	if err := e.store.Put("change_plan", plan.ID, plan); err != nil {
		return nil, fmt.Errorf("方案存档失败: %w", err)
	}
	return plan, nil
}

// validateTarget 输入校验：不同 targetType 需要不同字段。
func (e *Engine) validateTarget(tgt *domain.ChangeTarget) error {
	tgt.Env = strings.TrimSpace(tgt.Env)
	if tgt.Env == "" {
		tgt.Env = "PRO"
	}

	switch tgt.TargetType {
	case "middleware":
		if strings.TrimSpace(tgt.MwType) == "" || strings.TrimSpace(tgt.MwAddr) == "" {
			return fmt.Errorf("中间件升级模板需要同时提供 mwType 与 mwAddr")
		}
	case "app":
		if strings.TrimSpace(tgt.AppID) == "" {
			return fmt.Errorf("应用扩缩容/下线模板需要提供 appId")
		}
	case "apollo":
		if strings.TrimSpace(tgt.AppID) == "" {
			return fmt.Errorf("Apollo 配置变更模板需要提供 appId")
		}
	case "datacenter":
		// 机房迁移暂时只查全部应用的总影响面，不强制字段
	default:
		return fmt.Errorf("不支持的 targetType: %s", tgt.TargetType)
	}

	// 填充默认日期
	if tgt.DateToday == "" {
		if dates := e.topo.Dates(); len(dates) > 0 {
			tgt.DateToday = dates[len(dates)-1]
		}
	}
	if tgt.DatePrev == "" {
		if dates := e.topo.Dates(); len(dates) > 1 {
			tgt.DatePrev = dates[len(dates)-2]
		}
	}
	return nil
}

// ValidateRisk 风险打分：爆炸半径 × 0.4 + 快照漂移 × 0.3 + 历史故障 × 0.3。
func (e *Engine) ValidateRisk(tgt *domain.ChangeTarget) domain.RiskResult {
	res := domain.RiskResult{
		Level: "low",
	}

	// 1) 爆炸半径权重（0-0.4）
	var impact *domain.Impact
	switch tgt.TargetType {
	case "middleware":
		im, err := e.topo.ImpactByMiddleware(tgt.MwType, tgt.MwAddr, tgt.DateToday, tgt.Env)
		if err == nil {
			impact = im
		}
	case "app":
		im, err := e.topo.ImpactByApp(tgt.AppID, tgt.AppID, tgt.DateToday, tgt.Env, "expanded")
		if err == nil {
			impact = im
		}
	case "apollo":
		im, err := e.topo.ImpactByApp(tgt.AppID, tgt.AppID, tgt.DateToday, tgt.Env, "expanded")
		if err == nil {
			impact = im
		}
	case "datacenter":
		// 机房迁移：取全量拓扑的应用总数作为爆炸半径参考
		apps, _ := e.topo.Apps(tgt.DateToday, tgt.Env)
		res.ExplosionApps = len(apps)
	}
	if impact != nil {
		res.ExplosionApps = impact.TotalApps
		// 爆炸半径：≥10 满权重 0.4；线性下降
		if impact.TotalApps >= 10 {
			res.Score += 0.4
			res.Blockers = append(res.Blockers, fmt.Sprintf("影响应用 %d ≥ 10，请人工审批", impact.TotalApps))
		} else if impact.TotalApps > 0 {
			res.Score += float64(impact.TotalApps) / 10 * 0.4
		}
	}

	// 2) 快照漂移权重（0-0.3）
	if tgt.DatePrev != "" && tgt.DateToday != "" && tgt.DatePrev != tgt.DateToday {
		diff, err := e.topo.Diff(tgt.DatePrev, tgt.DateToday, tgt.Env)
		if err == nil {
			total := len(diff.Added) + len(diff.Removed)
			res.DriftRatio = float64(total) / 100.0
			if res.DriftRatio > 1 {
				res.DriftRatio = 1
			}
			res.Score += res.DriftRatio * 0.3
			if res.DriftRatio >= 0.5 {
				res.Blockers = append(res.Blockers, fmt.Sprintf("拓扑关系漂移 %.0f%%，建议先确认数据正确性", res.DriftRatio*100))
			}
		}
	}

	// 3) 历史故障权重（0-0.3）
	if tgt.AppID != "" && e.diag != nil {
		sessions := e.diag.List(200)
		cutoff := time.Now().AddDate(0, 0, -30)
		for _, s := range sessions {
			if s.AppID == tgt.AppID && s.CreatedAt.After(cutoff) {
				res.RecentFaults++
			}
		}
		if res.RecentFaults > 0 {
			// 近 30 天故障数 / 5 线性，≥5 满权重 0.3
			ratio := float64(res.RecentFaults) / 5.0
			if ratio > 1 {
				ratio = 1
			}
			res.Score += ratio * 0.3
			if res.RecentFaults >= 3 {
				res.Blockers = append(res.Blockers, fmt.Sprintf("近 30 天同应用已有 %d 次排障会话，故障频发", res.RecentFaults))
			}
		}
	}

	// 4) 封顶 + 等级判定
	if res.Score > 1 {
		res.Score = 1
	}
	if res.Score > 0.7 {
		res.Blockers = append(res.Blockers, fmt.Sprintf("风险评分 %.2f ≥ 0.7，禁止自动执行", res.Score))
		res.Level = "critical"
	} else if res.Score >= 0.5 {
		res.Level = "high"
	} else if res.Score >= 0.3 {
		res.Level = "medium"
	} else {
		res.Level = "low"
	}

	if len(res.Blockers) == 0 {
		res.Notes = "风险等级低，可按模板步骤执行"
	}
	return res
}

// GetPlan 按 ID 取方案。
func (e *Engine) GetPlan(id string) (*domain.ChangePlan, bool) {
	var plan domain.ChangePlan
	if store.GetInto(e.store, "change_plan", id, &plan) {
		return &plan, true
	}
	return nil, false
}

// ListPlans 列出全部方案（按创建时间倒序）。
func (e *Engine) ListPlans() []domain.ChangePlan {
	out := store.ListInto[domain.ChangePlan](e.store, "change_plan")
	// 时间倒序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
