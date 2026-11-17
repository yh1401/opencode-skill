package service

import (
	"os"
	"path/filepath"
	"testing"
)

// ns_learn_store 纯逻辑单测（临时，验证 merge/flush/到期判定，不依赖网络）

func newTmpLearn(t *testing.T) *nsLearnStore {
	t.Helper()
	return &nsLearnStore{path: filepath.Join(t.TempDir(), "learned.json")}
}

func TestNsLearnMergeHitPersistAndRemove(t *testing.T) {
	s := newTmpLearn(t)
	today := "2026-09-09"
	key := "13|PRO|rule-engine"

	// 1) 首轮 bootstrap 命中 application+datasource，并标记 lastGuessAt=today
	s.merge(
		map[string]map[string]bool{key: {"application": true, "datasource": true}},
		nil,
		map[string]bool{key: true},
		today)
	e := s.get(key)
	if e == nil || len(e.Namespaces) != 2 || e.LastGuessAt != today {
		t.Fatalf("bootstrap 合并异常: entry=%+v", e)
	}
	if err := s.flush(); err != nil {
		t.Fatalf("flush 失败: %v", err)
	}
	if _, err := os.Stat(s.path); err != nil {
		t.Fatalf("学习文件未写出: %v", err)
	}

	// 2) 重新实例化（模拟重启），验证持久化可读
	s2 := &nsLearnStore{path: s.path}
	e2 := s2.get(key)
	if e2 == nil || len(e2.Namespaces) != 2 {
		t.Fatalf("持久化后读取异常: entry=%+v", e2)
	}

	// 3) 后续定向轮 404（datasource 已删）→ 应从集中移除，自愈漂移
	s2.merge(nil, map[string]map[string]bool{key: {"datasource": true}}, nil, today)
	e3 := s2.get(key)
	if e3 == nil || len(e3.Namespaces) != 1 || e3.Namespaces[0] != "application" {
		t.Fatalf("404 移除异常: entry=%+v", e3)
	}

	// 4) 再次 404（application 也删）：该条目曾全量过（lastGuessAt=today），
	//    保留空壳用于兜底计时（避免每日重复全名单），Namespaces 应为空
	s2.merge(nil, map[string]map[string]bool{key: {"application": true}}, nil, today)
	e4 := s2.get(key)
	if e4 == nil || len(e4.Namespaces) != 0 || e4.LastGuessAt != today {
		t.Fatalf("曾全量的空条目应保留(兜底计时), got %+v", e4)
	}

	// 5) 从未全量(无 lastGuessAt)的纯命中条目被 404 清空 → 应整体清理，不残留垃圾
	key2 := "15|PRO|ghost-service"
	s2.merge(map[string]map[string]bool{key2: {"redis": true}}, nil, nil, today)
	s2.merge(nil, map[string]map[string]bool{key2: {"redis": true}}, nil, today)
	if e5 := s2.get(key2); e5 != nil {
		t.Fatalf("从未全量的空条目未清理: %+v", e5)
	}
}

func TestNsGuessDue(t *testing.T) {
	today := "2026-09-09"
	cases := []struct {
		last string
		days int
		want bool
	}{
		{"", 7, true},            // 从未全量 → 到期
		{"2026-09-09", 7, false}, // 今天刚全量 → 未到期
		{"2026-09-02", 7, true},  // 7 天前 → 到期
		{"2026-09-05", 7, false}, // 4 天前 → 未到期
		{"2026-09-02", 0, false}, // guessDays<=0 禁用兜底
		{"bad-date", 7, true},    // 格式异常 → 按到期自愈
	}
	for _, c := range cases {
		if got := nsGuessDue(c.last, c.days, today); got != c.want {
			t.Fatalf("nsGuessDue(%q,%d) = %v, want %v", c.last, c.days, got, c.want)
		}
	}
}

func TestEnsureApplication(t *testing.T) {
	if got := ensureApplication([]string{"datasource", "redis"}); len(got) != 3 || got[0] != "application" {
		t.Fatalf("缺 application 应补到首位: %v", got)
	}
	if got := ensureApplication([]string{"application", "redis"}); len(got) != 2 {
		t.Fatalf("已有 application 不应重复: %v", got)
	}
}

func TestNsLearnKey(t *testing.T) {
	if got := nsLearnKey(13, "PRO", "rule-engine"); got != "13|PRO|rule-engine" {
		t.Fatalf("key 异常: %s", got)
	}
}
