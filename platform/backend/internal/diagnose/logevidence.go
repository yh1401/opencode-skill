package diagnose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LogStat 日志报告中的一项统计（错误类型 / 高频错误类 / 错误模式）。
type LogStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// LogIssue 关键错误分析中的一条问题。
type LogIssue struct {
	Name     string `json:"name"`
	Desc     string `json:"desc,omitempty"`
	Count    int    `json:"count"`
	Severity string `json:"severity,omitempty"`
}

// LogEvidence 日志分析服务产出的关键证据（不依赖整份报告原文）。
type LogEvidence struct {
	Path       string
	TaskID     string
	Files      int
	Lines      int
	Errors     int
	Warnings   int
	TopTypes   []LogStat
	TopClasses []LogStat
	KeyIssues  []LogIssue
	Patterns   []LogStat
}

// logClient 调用外部「日志文件分析服务」（FastAPI，默认 :8000）：
// POST /api/process-from-path 提交路径 → 轮询 /api/task/{id} → 解析综合报告 markdown。
type logClient struct {
	baseURL string
	wait    time.Duration
	poll    time.Duration
	http    *http.Client
}

func newLogClient(baseURL string, wait, poll time.Duration) *logClient {
	if poll <= 0 {
		poll = time.Second
	}
	if wait <= 0 {
		wait = 30 * time.Second
	}
	return &logClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		wait:    wait,
		poll:    poll,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

type logsEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type logTaskCreated struct {
	TaskID    string `json:"task_id"`
	FileCount int    `json:"file_count"`
}

type logReport struct {
	Type       string `json:"type"`
	IsCombined bool   `json:"is_combined"`
	Content    string `json:"content"`
}

type logTaskStatus struct {
	TaskID  string      `json:"task_id"`
	Status  string      `json:"status"`
	Message string      `json:"message"`
	Error   string      `json:"error"`
	Reports []logReport `json:"reports"`
}

// Analyze 提交路径分析任务并等待结果；ctx 超时或服务不可用都会返回错误，由调用方降级为 skipped。
func (c *logClient) Analyze(ctx context.Context, path string) (*LogEvidence, error) {
	ctx, cancel := context.WithTimeout(ctx, c.wait)
	defer cancel()

	var created logTaskCreated
	if err := c.do(ctx, http.MethodPost, "/api/process-from-path", map[string]any{"path": path}, &created); err != nil {
		return nil, err
	}
	if created.TaskID == "" {
		return nil, fmt.Errorf("日志分析服务未返回 task_id")
	}

	for {
		var st logTaskStatus
		if err := c.do(ctx, http.MethodGet, "/api/task/"+created.TaskID, nil, &st); err != nil {
			return nil, err
		}
		switch st.Status {
		case "completed":
			ev := parseLogReports(st.Reports)
			ev.Path = path
			ev.TaskID = created.TaskID
			ev.Files = created.FileCount
			return ev, nil
		case "failed", "cancelled":
			return nil, fmt.Errorf("日志分析失败：%s", firstNonEmpty(st.Error, st.Message, st.Status))
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("日志分析超时（%s）", c.wait)
		case <-time.After(c.poll):
		}
	}
}

func (c *logClient) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "aiops-platform")
	req.Header.Set("X-User_Name", "AIOps 平台")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("日志分析服务不可用：%v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("读取日志分析响应失败：%v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("日志分析服务返回 %d：%s", resp.StatusCode, truncateText(string(raw), 200))
	}
	var env logsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("解析日志分析响应失败：%v", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("日志分析服务错误：%s", firstNonEmpty(env.Message, "code="+strconv.Itoa(env.Code)))
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

var (
	reLogTotalLines = regexp.MustCompile(`\*\*总行数\*\*\s*[:：]\s*([\d,]+)`)
	reLogTotalErrs  = regexp.MustCompile(`\*\*总错误数\*\*\s*[:：]\s*([\d,]+)`)
	reLogTotalWarns = regexp.MustCompile(`\*\*总警告数\*\*\s*[:：]\s*([\d,]+)`)
	reLogStatLine   = regexp.MustCompile(`^[-*]\s*(.+?)\s*[:：]\s*([\d,]+)\s*$`)
	reLogIssueHead  = regexp.MustCompile(`^#{3,}\s*(?:\d+[.、]\s*)?(.+?)\s*$`)
)

// parseLogReports 从综合 markdown 报告里抽取总量、TopN 与关键问题。
func parseLogReports(reports []logReport) *LogEvidence {
	ev := &LogEvidence{}
	md := pickMarkdown(reports)
	if md == "" {
		return ev
	}
	if len(md) > 2<<20 {
		md = md[:2<<20]
	}
	ev.Lines = firstInt(reLogTotalLines, md)
	ev.Errors = firstInt(reLogTotalErrs, md)
	ev.Warnings = firstInt(reLogTotalWarns, md)
	ev.TopTypes = parseLogStats(logSection(md, "错误类型统计"), 5)
	ev.TopClasses = parseLogStats(logSection(md, "高频错误类"), 5)
	ev.Patterns = parseLogStats(logSection(md, "错误模式识别"), 5)
	ev.KeyIssues = parseLogIssues(logSectionDeep(md, "关键错误分析"), 5)
	return ev
}

func pickMarkdown(reports []logReport) string {
	for _, r := range reports {
		if r.IsCombined && strings.Contains(strings.ToLower(r.Type), "markdown") && r.Content != "" {
			return r.Content
		}
	}
	for _, r := range reports {
		if strings.Contains(strings.ToLower(r.Type), "markdown") && r.Content != "" {
			return r.Content
		}
	}
	return ""
}

// logSection 返回标题行之后、下一个标题之前的正文行。
func logSection(md, heading string) []string {
	var out []string
	collecting := false
	for _, ln := range strings.Split(md, "\n") {
		t := strings.TrimSpace(ln)
		if !collecting {
			if strings.Contains(t, heading) && strings.HasPrefix(t, "#") {
				collecting = true
			}
			continue
		}
		if strings.HasPrefix(t, "#") {
			break
		}
		out = append(out, t)
	}
	return out
}

// logSectionDeep 与 logSection 类似，但允许小节内部出现更深的标题（如关键错误分析的 `#### N.`）。
func logSectionDeep(md, heading string) []string {
	var out []string
	collecting := false
	for _, ln := range strings.Split(md, "\n") {
		t := strings.TrimSpace(ln)
		if !collecting {
			if strings.Contains(t, heading) && strings.HasPrefix(t, "#") {
				collecting = true
			}
			continue
		}
		if strings.HasPrefix(t, "### ") || strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "# ") {
			break
		}
		out = append(out, t)
	}
	return out
}

func parseLogStats(lines []string, limit int) []LogStat {
	out := make([]LogStat, 0, limit)
	for _, ln := range lines {
		m := reLogStatLine.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		name := strings.Trim(strings.TrimSpace(m[1]), "*` ")
		out = append(out, LogStat{Name: name, Count: atoiTrim(m[2])})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// parseLogIssues 解析「关键错误分析」下的 `#### N. 名称` + 描述/次数/严重程度。
func parseLogIssues(lines []string, limit int) []LogIssue {
	out := make([]LogIssue, 0, limit)
	var cur *LogIssue
	flush := func() {
		if cur != nil && cur.Name != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "####") {
			m := reLogIssueHead.FindStringSubmatch(t)
			flush()
			if m == nil {
				continue
			}
			cur = &LogIssue{Name: strings.Trim(strings.TrimSpace(m[1]), "*` ")}
			continue
		}
		if cur == nil {
			continue
		}
		if v, ok := logBulletValue(t, "描述"); ok {
			cur.Desc = v
		} else if v, ok := logBulletValue(t, "出现次数"); ok {
			cur.Count = atoiTrim(v)
		} else if v, ok := logBulletValue(t, "严重程度"); ok {
			cur.Severity = v
		}
	}
	flush()
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// logBulletValue 解析 `- **描述**: xxx` 形式的行。
func logBulletValue(line, key string) (string, bool) {
	body := strings.TrimSpace(line)
	if strings.HasPrefix(body, "-") || strings.HasPrefix(body, "*") {
		body = strings.TrimSpace(body[1:])
	}
	prefix := "**" + key + "**"
	if !strings.HasPrefix(body, prefix) {
		return "", false
	}
	body = strings.TrimSpace(strings.TrimPrefix(body, prefix))
	body = strings.TrimSpace(strings.TrimLeft(body, ":："))
	return body, body != ""
}

func firstInt(re *regexp.Regexp, s string) int {
	if m := re.FindStringSubmatch(s); m != nil {
		return atoiTrim(m[1])
	}
	return 0
}

func atoiTrim(s string) int {
	n, _ := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(s), ",", ""))
	return n
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
