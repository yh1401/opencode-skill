package inspect

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// Scheduler 巡检定时调度器（S2）：分钟级 tick，按任务 cron 表达式触发。
// 不引入第三方 cron 依赖，支持标准 5 段表达式：分 时 日 月 周。
type Scheduler struct {
	eng  *Engine
	st   store.Store
	cfg  config.Config
	last map[string]int64 // taskID → 已触发的 unix 分钟
	bad  map[string]string
}

func NewScheduler(eng *Engine, st store.Store, cfg config.Config) *Scheduler {
	return &Scheduler{eng: eng, st: st, cfg: cfg, last: map[string]int64{}, bad: map[string]string{}}
}

// Start 启动调度循环，阻塞直到 ctx cancel。
func (s *Scheduler) Start(ctx context.Context) {
	if !s.cfg.InspectCronEnabled {
		return
	}
	tick := s.cfg.InspectCronTick
	if tick <= 0 {
		tick = 20 * time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.Tick(now)
		}
	}
}

// Tick 执行一次到期检查（导出便于测试与手动触发）。
func (s *Scheduler) Tick(now time.Time) int {
	tasks := store.ListInto[domain.InspectionTask](s.st, "inspection_task")
	minute := now.Unix() / 60
	started := 0
	for _, task := range tasks {
		if !task.Enabled || strings.TrimSpace(task.Cron) == "" {
			continue
		}
		ok, err := cronMatch(task.Cron, now)
		if err != nil {
			// 同一个表达式只记录一次错误，避免每 tick 刷日志
			if s.bad[task.ID] != err.Error() {
				s.bad[task.ID] = err.Error()
				fmt.Printf("[inspect-cron] 任务 %s cron 非法：%v\n", task.ID, err)
			}
			continue
		}
		if !ok {
			continue
		}
		if s.last[task.ID] == minute {
			continue // 同一分钟只触发一次
		}
		if s.hasRunningRun(task.ID) {
			continue // 上一轮还没跑完，跳过本轮
		}
		s.last[task.ID] = minute
		s.eng.Start(task, "cron")
		started++
	}
	return started
}

func (s *Scheduler) hasRunningRun(taskID string) bool {
	for _, run := range store.ListInto[domain.InspectionRun](s.st, "inspection_run") {
		if run.TaskID == taskID && run.Status == "running" {
			return true
		}
	}
	return false
}

// ValidateCron 校验 5 段 cron 表达式语法，供任务保存时前置拦截。
// 空表达式表示不启用定时调度，视为合法。
func ValidateCron(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("cron 需要 5 段（分 时 日 月 周），实际 %d 段", len(fields))
	}
	bounds := [][3]int{{0, 59, 0}, {0, 23, 0}, {1, 31, 0}, {1, 12, 0}, {0, 7, 1}}
	for i, f := range fields {
		b := bounds[i]
		if _, err := fieldMatch(f, b[0], b[0], b[1], b[2] == 1); err != nil {
			return err
		}
	}
	return nil
}

// cronMatch 判断 5 段 cron 表达式是否命中给定时刻。
func cronMatch(expr string, t time.Time) (bool, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return false, fmt.Errorf("需要 5 段（分 时 日 月 周），实际 %d 段", len(fields))
	}
	checks := []struct {
		field string
		value int
		min   int
		max   int
		dow   bool
	}{
		{fields[0], t.Minute(), 0, 59, false},
		{fields[1], t.Hour(), 0, 23, false},
		{fields[2], t.Day(), 1, 31, false},
		{fields[3], int(t.Month()), 1, 12, false},
		{fields[4], int(t.Weekday()), 0, 7, true},
	}
	for _, c := range checks {
		hit, err := fieldMatch(c.field, c.value, c.min, c.max, c.dow)
		if err != nil {
			return false, err
		}
		if !hit {
			return false, nil
		}
	}
	return true, nil
}

// fieldMatch 支持 *、*/n、a、a-b、a-b/n、逗号列表，以及日/周字段的 ?。
func fieldMatch(field string, value, min, max int, dow bool) (bool, error) {
	field = strings.TrimSpace(field)
	if field == "" || field == "?" || field == "*" {
		return true, nil
	}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(strings.TrimSpace(part[i+1:]))
			if err != nil || n <= 0 {
				return false, fmt.Errorf("非法步长 %q", part)
			}
			step = n
			part = strings.TrimSpace(part[:i])
		}
		lo, hi := min, max
		switch {
		case part == "*" || part == "?":
			// 保持全范围
		case strings.Contains(part, "-"):
			ab := strings.SplitN(part, "-", 2)
			a, err1 := strconv.Atoi(strings.TrimSpace(ab[0]))
			b, err2 := strconv.Atoi(strings.TrimSpace(ab[1]))
			if err1 != nil || err2 != nil {
				return false, fmt.Errorf("非法区间 %q", part)
			}
			lo, hi = a, b
		default:
			v, err := strconv.Atoi(part)
			if err != nil {
				return false, fmt.Errorf("非法数字 %q", part)
			}
			lo, hi = v, v
		}
		if lo < min || hi > max {
			return false, fmt.Errorf("取值超出范围 %q（%d-%d）", part, min, max)
		}
		if hi < lo {
			return false, fmt.Errorf("区间起止颠倒 %q", part)
		}
		for v := lo; v <= hi; v += step {
			if v == value || (dow && v%7 == value%7) {
				return true, nil
			}
		}
	}
	return false, nil
}
