package service

// 跳① 关系接口（appId ↔ 部署项目，`GET /thirdApi/apolloApp/list`）回归测试：
//   - 参数契约：固定 paginator=false，pageSize<=0 一律发 -1（不分页、一次取全量）
//   - 全量短路：上游 pageTotal 即总记录数时只发 1 次请求，不再按页数换算
//   - 分页兜底：上游确实分页（返回行数 < pageTotal）时按 pageSize 换算补齐
//   - 真实全量样本（docs/业务数据/apolloApp_list.json，1426 行）端到端校验字段口径（文件缺失时 Skip）
//   - 跳①×跳② join（joinDpTargets）与属性补充/过滤（attachAppAttrs / relationFilterApps / filterAttachRows / pageRows）
//
// 注意：上游 EasyOps 在生产内网，单测一律用 httptest 桩服务（同路径同参数）替代，不依赖外网。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"devops-automation/api/tprel/model"
	"devops-automation/config"
	"devops-automation/global"
)

// queryCapture 记录桩服务收到的全部查询参数（handler 在独立 goroutine，需加锁）
type queryCapture struct {
	mu   sync.Mutex
	vals []url.Values
}

func (c *queryCapture) add(v url.Values) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals = append(c.vals, v)
}

func (c *queryCapture) all() []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]url.Values, len(c.vals))
	copy(out, c.vals)
	return out
}

func (c *queryCapture) count() int { return len(c.all()) }

// relationStub 关系接口桩：返回固定响应体，并记录请求参数
func relationStub(t *testing.T, capture *queryCapture, resp any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.add(r.URL.Query())
		if r.URL.Path != "/thirdApi/apolloApp/list" {
			t.Errorf("请求路径不符：得到 %s", r.URL.Path)
		}
		if cookie := r.Header.Get("Cookie"); cookie == "" {
			t.Errorf("缺少 Cookie sessionId 鉴权头")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// GetAppDpRelations：pageSize<=0 一律发 -1（-1 = 不分页、一次取全量），paginator 固定 false
func TestGetAppDpRelationsFullPullContract(t *testing.T) {
	t.Setenv("APOLLO_HOST_API_BASE", "") // 防外部环境变量覆盖桩地址
	items := []AppDpRelationRow{
		{Id: 1, ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1"},
		{Id: 2, ApolloAppId: "app-2", ApolloAppEnv: "DEV", DeployProjectName: "dep-b", Node: "n2"},
	}
	capture := &queryCapture{}
	ts := relationStub(t, capture, map[string]any{
		"code": "success", "list": items, "pageTotal": len(items), // pageTotal = 总记录数（上游口径）
	})
	defer ts.Close()

	client := NewApolloClient(config.Apollo{BaseUrl: ts.URL, RequestTimeout: 5})
	rows, total, err := client.GetAppDpRelations("/thirdApi/apolloApp/list", 1, -1)
	if err != nil {
		t.Fatalf("全量拉取失败：%v", err)
	}
	if len(rows) != len(items) || total != len(items) {
		t.Errorf("期望 %d 行 / total=%d，得到 %d 行 / total=%d", len(items), len(items), len(rows), total)
	}
	q := capture.all()[0]
	if q.Get("paginator") != "false" || q.Get("pageIndex") != "1" || q.Get("pageSize") != "-1" {
		t.Errorf("参数契约不符：paginator=%s pageIndex=%s pageSize=%s（期望 false / 1 / -1）",
			q.Get("paginator"), q.Get("pageIndex"), q.Get("pageSize"))
	}

	// pageSize=0（配置缺省）同样应发 -1；负值以外的 <=0 一律按不分页处理
	if _, _, err := client.GetAppDpRelations("/thirdApi/apolloApp/list", 1, 0); err != nil {
		t.Fatalf("pageSize=0 调用失败：%v", err)
	}
	if got := capture.all()[1].Get("pageSize"); got != "-1" {
		t.Errorf("pageSize=0 应回退为 -1，实际发送 %s", got)
	}
}

// GetAppDpRelations：上游 code != success 应报错（不静默返回空集）
func TestGetAppDpRelationsUpstreamError(t *testing.T) {
	t.Setenv("APOLLO_HOST_API_BASE", "")
	capture := &queryCapture{}
	ts := relationStub(t, capture, map[string]any{"code": "fail", "message": "boom", "list": []any{}, "pageTotal": 0})
	defer ts.Close()

	client := NewApolloClient(config.Apollo{BaseUrl: ts.URL, RequestTimeout: 5})
	if _, _, err := client.GetAppDpRelations("/thirdApi/apolloApp/list", 1, -1); err == nil {
		t.Fatal("上游返回 fail 时应报错，实际未报错")
	}
}

// fetchRelationDTOs：真实全量样本（1426 行、pageTotal=1426）应一次取全，字段口径逐列对齐
func TestFetchRelationDTOsRealFullExport(t *testing.T) {
	t.Setenv("APOLLO_HOST_API_BASE", "")
	body, err := os.ReadFile(filepath.Join("..", "docs", "业务数据", "apolloApp_list.json"))
	if err != nil {
		t.Skipf("真实全量样本缺失，跳过：%v", err)
	}
	capture := &queryCapture{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.add(r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer ts.Close()

	cfg := config.Apollo{
		BaseUrl: ts.URL, RelationApiPath: "/thirdApi/apolloApp/list",
		RelationApiPageSize: -1, RequestTimeout: 10,
	}
	items, err := fetchRelationDTOs(cfg)
	if err != nil {
		t.Fatalf("全量拉取失败：%v", err)
	}
	if len(items) != 1426 {
		t.Errorf("期望 1426 行全量，得到 %d 行", len(items))
	}
	if capture.count() != 1 {
		t.Errorf("pageSize=-1 应一次取全量（1 次请求），实际请求 %d 次", capture.count())
	}
	if q := capture.all()[0]; q.Get("paginator") != "false" || q.Get("pageSize") != "-1" {
		t.Errorf("参数契约不符：paginator=%s pageSize=%s", q.Get("paginator"), q.Get("pageSize"))
	}

	// 首行字段口径（真实抓包首行，锁定 json tag 与上游字段一致）
	first := items[0]
	if first.Id != 1721 || first.ApolloAppId != "coastline-adapter" ||
		first.ApolloAppEnv != "PRO" || first.DeployProjectName != "coastline-adapter-lite-dep-dep" ||
		first.Node != "guangzhou4d-prod" || first.ApolloHostId != 14 || first.DelFlag {
		t.Errorf("首行解析口径不符：%+v", first)
	}
	// 落库模型转换：bool → 0/1、时间字符串 → *time.Time
	rec := toAppDpRelation(first)
	if rec.DelFlag != 0 || rec.IsReleased != 0 || rec.CreateTime == nil ||
		rec.CreateTime.Format("2006-01-02 15:04:05") != "2026-09-16 22:03:54" {
		t.Errorf("表模型转换不符：%+v", rec)
	}
	if rec.TableName() != "app_dp_relation" {
		t.Errorf("表名不符：%s", rec.TableName())
	}
	// 降维：del_flag=true 的行不参与查询期关联
	kept := 0
	for _, it := range items {
		if !it.DelFlag {
			kept++
		}
	}
	rows, err := fetchRelationRows(cfg) // 简化口径（不过滤空 appId/部署项目），用于与 kept 对照
	if err != nil {
		t.Fatalf("关系行降维失败：%v", err)
	}
	if len(rows) != kept {
		t.Errorf("降维应过滤逻辑删除行：期望 %d 行，得到 %d 行", kept, len(rows))
	}
}

// fetchRelationDTOs：上游确实分页（返回行数 < pageTotal）时按配置 pageSize 换算页数补齐
func TestFetchRelationDTOsUpstreamPaged(t *testing.T) {
	t.Setenv("APOLLO_HOST_API_BASE", "")
	const total = 100
	all := make([]AppDpRelationRow, 0, total)
	for i := 0; i < total; i++ {
		all = append(all, AppDpRelationRow{
			Id: int64(i + 1), ApolloAppId: fmt.Sprintf("app-%03d", i),
			ApolloAppEnv: "PRO", DeployProjectName: "dep-x", Node: "n1",
		})
	}
	capture := &queryCapture{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		capture.add(q)
		page, _ := strconv.Atoi(q.Get("pageIndex"))
		size, _ := strconv.Atoi(q.Get("pageSize"))
		if size <= 0 { // 上游忽略 -1 时的兜底页大小（模拟真分页的上游）
			size = 20
		}
		start := (page - 1) * size
		if start > total {
			start = total
		}
		end := start + size
		if end > total {
			end = total
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "success", "list": all[start:end], "pageTotal": total,
		})
	}))
	defer ts.Close()

	cfg := config.Apollo{
		BaseUrl: ts.URL, RelationApiPath: "/thirdApi/apolloApp/list",
		RelationApiPageSize: 50, RelationApiMaxPages: 200, RequestTimeout: 10,
	}
	items, err := fetchRelationDTOs(cfg)
	if err != nil {
		t.Fatalf("分页补齐失败：%v", err)
	}
	if len(items) != total {
		t.Errorf("期望补齐 %d 行，得到 %d 行", total, len(items))
	}
	if capture.count() != 2 {
		t.Errorf("pageSize=50 + pageTotal=100 期望 2 次请求，实际 %d 次", capture.count())
	}
	if q := capture.all()[1]; q.Get("pageIndex") != "2" {
		t.Errorf("第二页 pageIndex 应为 2，实际 %s", q.Get("pageIndex"))
	}
}

// joinDpTargets：同 (appId, 环境, 部署项目) 跨 node 去重；按 部署项目 展开跳② 多 PP 应用；DPR 未命中仅留部署项目
func TestJoinDpTargetsDedupAndExpand(t *testing.T) {
	savedOwners, savedAt, savedOK := pinpointOwners, pinpointLoadedAt, pinpointOK
	defer func() { pinpointOwners, pinpointLoadedAt, pinpointOK = savedOwners, savedAt, savedOK }()
	pinpointOwners = map[string][]model.PinpointOwner{
		"dep-a": {
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n1", PpApplication: "pp-2"},
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n1", PpApplication: "pp-1"},
		},
		"dep-b": {{DepProject: "dep-b", EgProject: "eg-b", Node: "n2", PpApplication: "pp-3"}},
	}
	pinpointLoadedAt, pinpointOK = time.Now(), true

	rows := []relationRow{
		{ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1"},
		{ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1"}, // 完全相同 → 去重
		{ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n9"}, // 同部署项目跨 node → 保留为两个 (部署项目,节点) 组合
		{ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-b", Node: "n2"},
		{ApolloAppId: "app-1", ApolloAppEnv: "DEV", DeployProjectName: "dep-c", Node: "n3"}, // DPR 未命中
		{ApolloAppId: "", ApolloAppEnv: "PRO", DeployProjectName: "dep-a"},                  // 无 appId → 跳过
		{ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: ""},                  // 无部署项目 → 跳过
	}
	idx := joinDpTargets(rows)
	if len(idx) != 2 {
		t.Fatalf("期望 2 个索引键（app-1|PRO、app-1|DEV），得到 %d：%+v", len(idx), idx)
	}
	pro := idx[relationKeyOf("app-1", "PRO")]
	if len(pro) != 5 {
		t.Fatalf("app-1|PRO 期望 5 个目标（dep-a 两个节点各两个 PP + dep-b 一个），得到 %d：%+v", len(pro), pro)
	}
	if pro[0].DepProject != "dep-a" || pro[0].Node != "n1" || pro[0].PpApplication != "pp-1" ||
		pro[1].Node != "n1" || pro[1].PpApplication != "pp-2" ||
		pro[2].Node != "n9" || pro[2].PpApplication != "pp-1" ||
		pro[3].Node != "n9" || pro[3].PpApplication != "pp-2" ||
		pro[4].DepProject != "dep-b" || pro[4].PpApplication != "pp-3" {
		t.Errorf("目标顺序应为 部署项目有序 + 节点有序 + PP 应用有序：%+v", pro)
	}
	if pro[4].Node != "n2" || pro[4].EgProject != "eg-b" {
		t.Errorf("dep-b 目标四列应同源：%+v", pro[4])
	}
	dev := idx[relationKeyOf("app-1", "DEV")]
	if len(dev) != 1 || dev[0].DepProject != "dep-c" || dev[0].PpApplication != "" {
		t.Errorf("DPR 未命中时应仅保留部署项目：%+v", dev)
	}
	if joinDpTargets(nil) != nil {
		t.Error("空输入应返回 nil")
	}
}

// attachAppAttrs / relationFilterApps / filterAttachRows / pageRows：属性补充与过滤下推
func TestAttachAndFilterRows(t *testing.T) {
	savedIdx, savedAt, savedOK, savedSrc, savedRows := relationIndex, relationLoadedAt, relationOK, relationSource, relationRows
	savedTTL := global.GVA_CONFIG.Apollo.RelationApiTTL
	defer func() {
		relationIndex, relationLoadedAt, relationOK, relationSource, relationRows = savedIdx, savedAt, savedOK, savedSrc, savedRows
		global.GVA_CONFIG.Apollo.RelationApiTTL = savedTTL
	}()

	global.GVA_CONFIG.Apollo.RelationApiTTL = 60
	relationIndex = map[string][]dpTarget{
		relationKeyOf("app-1", "PRO"): {
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n1", PpApplication: "pp-1"},
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n1", PpApplication: "pp-2"},
		},
		// app-3：不同维度由**不同属性项**满足（部署项目 dep-x 在第 1 项、节点 n4 在第 2 项），
		// 用于验证下推按 appId 聚合属性列表（与 attachAppAttrs 并集口径一致），不做「同项同时成立」的误剔除。
		relationKeyOf("app-3", "PRO"): {
			{DepProject: "dep-x", EgProject: "eg-3", Node: "n3", PpApplication: "pp-x"},
			{DepProject: "dep-y", EgProject: "eg-3", Node: "n4", PpApplication: "pp-y"},
		},
	}
	relationLoadedAt, relationOK, relationSource, relationRows = time.Now(), true, sourceDB, 2

	rows := []model.OperationTopology{
		{ApolloAppId: "app-1", Env: "PRO", PpApplication: "app-1"}, // 命中 → 属性补充（多值聚合）
		{ApolloAppId: "app-1", Env: "DEV"},                         // 环境不匹配 → 原样保留
		{ApolloAppId: "app-2", Env: "PRO"},                         // 无关系 → 原样保留
	}
	out := attachAppAttrs(rows)
	// 行数/行序不变：关联数据只是 appId 组件的属性，绝不按属性组合扩维关系数据集
	if len(out) != 3 {
		t.Fatalf("属性补充不应改变行数（期望 3 行），得到 %d：%+v", len(out), out)
	}
	if out[0].DepProject != "dep-a" || out[0].EngProject != "eg-a" || out[0].Node != "n1" || out[0].PpApplication != "pp-1" {
		t.Errorf("单值列应取属性首值：%+v", out[0])
	}
	if !equalStrings(out[0].DepProjects, []string{"dep-a"}) || !equalStrings(out[0].EngProjects, []string{"eg-a"}) {
		t.Errorf("部署项目/工程项目应聚合为属性列表：%+v", out[0])
	}
	if !equalStrings(out[0].Nodes, []string{"n1"}) {
		t.Errorf("节点应聚合为属性列表：%+v", out[0])
	}
	if !equalStrings(out[0].PpApplications, []string{"pp-1", "pp-2"}) {
		t.Errorf("同一 appId 的多个 PP 应用应存为多值（不扩行）：%+v", out[0])
	}
	if out[1].Env != "DEV" || out[1].DepProjects != nil || out[1].PpApplications != nil {
		t.Errorf("环境不匹配行应原样保留（不补属性、不丢行）：%+v", out[1])
	}
	if out[2].ApolloAppId != "app-2" || out[2].PpApplications != nil {
		t.Errorf("无关系行应原样保留（不补属性、不丢行）：%+v", out[2])
	}
	// 入参不被修改：数据源可能返回底层快照切片的子切片，原地改写会污染内存快照
	if rows[0].DepProjects != nil || rows[0].PpApplications != nil {
		t.Errorf("attachAppAttrs 不应修改入参：%+v", rows[0])
	}

	// 过滤下推：appId 属性（部署项目模糊、工程项目/节点/PP 应用精确）→ appId 集合
	if apps, ok := relationFilterApps(appAttrQuery{DepProject: []string{"dep-a"}}); !ok || len(apps) != 1 || apps[0] != "app-1" {
		t.Errorf("部署项目下推不符：ok=%v apps=%v", ok, apps)
	}
	if apps, ok := relationFilterApps(appAttrQuery{DepProject: []string{"dep-"}, Node: []string{"n1"}}); !ok || len(apps) != 1 || apps[0] != "app-1" {
		t.Errorf("部署项目+节点下推不符：ok=%v apps=%v", ok, apps)
	}
	if apps, ok := relationFilterApps(appAttrQuery{EngProject: []string{"eg-a"}}); !ok || len(apps) != 1 || apps[0] != "app-1" {
		t.Errorf("工程项目下推不符：ok=%v apps=%v", ok, apps)
	}
	if apps, ok := relationFilterApps(appAttrQuery{PpApplication: []string{"pp-2"}}); !ok || len(apps) != 1 || apps[0] != "app-1" {
		t.Errorf("PP 应用下推不符：ok=%v apps=%v", ok, apps)
	}
	if apps, ok := relationFilterApps(appAttrQuery{EngProject: []string{"eg-x"}}); !ok || len(apps) != 0 {
		t.Errorf("工程项目无命中应返回空集合（ok=true）：ok=%v apps=%v", ok, apps)
	}
	if apps, ok := relationFilterApps(appAttrQuery{EngProject: []string{"eg-x", "eg-a"}}); !ok || len(apps) != 1 || apps[0] != "app-1" {
		t.Errorf("维度内多值（OR）有一个命中即应命中 app-1：ok=%v apps=%v", ok, apps)
	}
	// 多条件在「同一 appId 的属性列表」内分别匹配（并集口径，与复核一致）：
	// eg-a 命中而 pp-9 不命中 → 该 appId 被剔除；app-3 的 eg-3 亦不命中
	if apps, ok := relationFilterApps(appAttrQuery{EngProject: []string{"eg-a"}, PpApplication: []string{"pp-9"}}); !ok || len(apps) != 0 {
		t.Errorf("同一 appId 内未全部命中的条件不应命中：ok=%v apps=%v", ok, apps)
	}
	// 跨属性项（不同维度由不同属性项满足）应命中：app-3 的 dep-x（第 1 项）+ n4（第 2 项）
	if apps, ok := relationFilterApps(appAttrQuery{DepProject: []string{"dep-x"}, Node: []string{"n4"}}); !ok || len(apps) != 1 || apps[0] != "app-3" {
		t.Errorf("跨属性项的 appId 级并集下推应命中 app-3：ok=%v apps=%v", ok, apps)
	}
	if apps, ok := relationFilterApps(appAttrQuery{Node: []string{"n9"}}); !ok || len(apps) != 0 {
		t.Errorf("节点无命中应返回空集合（ok=true）：ok=%v apps=%v", ok, apps)
	}
	if _, ok := relationFilterApps(appAttrQuery{}); ok {
		t.Error("无条件时应返回 ok=false，保持旧过滤行为")
	}

	// 复核粒度是 appId：命中的 appId 的全部关系行都保留（不按属性组合切分关系数据集）
	if got := filterAttachRows(out, appAttrQuery{DepProject: []string{"dep-a"}}); len(got) != 1 {
		t.Errorf("按部署项目复核应保留 1 行（app-1|PRO），得到 %d", len(got))
	}
	if got := filterAttachRows(out, appAttrQuery{DepProject: []string{"dep-"}}); len(got) != 1 {
		t.Errorf("部署项目模糊复核应保留 1 行，得到 %d", len(got))
	}
	if got := filterAttachRows(out, appAttrQuery{Node: []string{"n1"}}); len(got) != 1 {
		t.Errorf("按节点复核应保留 1 行，得到 %d", len(got))
	}
	if got := filterAttachRows(out, appAttrQuery{EngProject: []string{"eg-a"}}); len(got) != 1 {
		t.Errorf("按工程项目复核应保留 1 行，得到 %d", len(got))
	}
	if got := filterAttachRows(out, appAttrQuery{PpApplication: []string{"pp-2"}}); len(got) != 1 {
		t.Errorf("按 PP 应用复核应保留 1 行（多值列表中命中即保留），得到 %d", len(got))
	}
	if got := filterAttachRows(out, appAttrQuery{PpApplication: []string{"pp-1"}}); len(got) != 1 {
		t.Errorf("按 PP 应用首值复核应保留 1 行，得到 %d", len(got))
	}
	if got := filterAttachRows(out, appAttrQuery{EngProject: []string{"eg-a", "eg-x"}}); len(got) != 1 {
		t.Errorf("维度内多值复核（OR）应保留 1 行，得到 %d", len(got))
	}
	if got := filterAttachRows(out, appAttrQuery{DepProject: []string{"dep-a"}, Node: []string{"n2"}}); len(got) != 0 {
		t.Errorf("部署项目与节点不同时命中应剔除全部行，得到 %d", len(got))
	}
	if got := filterAttachRows(out, appAttrQuery{DepProject: []string{"dep-b"}}); len(got) != 0 {
		t.Errorf("未命中部署项目应返回 0 行，得到 %d", len(got))
	}

	// 复核与下推口径一致：跨属性项（各维度分别命中不同属性值）应保留该 appId 的行
	unionRow := model.OperationTopology{
		ApolloAppId: "app-3", Env: "PRO",
		DepProjects: []string{"dep-x", "dep-y"}, EngProjects: []string{"eg-3"},
		Nodes: []string{"n3", "n4"}, PpApplications: []string{"pp-x", "pp-y"},
	}
	if got := filterAttachRows([]model.OperationTopology{unionRow}, appAttrQuery{DepProject: []string{"dep-x"}, Node: []string{"n4"}}); len(got) != 1 {
		t.Errorf("跨属性项的多条件复核应保留该行（下推/复核同口径），得到 %d", len(got))
	}

	// 内存分页：<0 全量、0 取默认 20
	if got := pageRows(out, 1, 2); len(got) != 2 {
		t.Errorf("第 1 页应 2 行，得到 %d", len(got))
	}
	if got := pageRows(out, 2, 2); len(got) != 1 {
		t.Errorf("第 2 页应 1 行（总 3 行），得到 %d", len(got))
	}
	if got := pageRows(out, 9, 2); len(got) != 0 {
		t.Errorf("越界页应返回空，得到 %d", len(got))
	}
	if got := pageRows(out, 1, -1); len(got) != len(out) {
		t.Errorf("pageSize<0 应取全量 %d 行，得到 %d", len(out), len(got))
	}
	if got := pageRows(out, 1, 0); len(got) != len(out) {
		t.Errorf("pageSize=0 走默认 20（>= 总行数）应返回全部 %d 行，得到 %d", len(out), len(got))
	}
}

// 属性辅助函数：多值去重排序、列表并集、单值取首、属性命中判定（含未补充属性时的回退）
func TestAppAttrHelpers(t *testing.T) {
	if got := dedupSorted(nil); got != nil {
		t.Errorf("空输入应返回 nil：%v", got)
	}
	if got := dedupSorted([]string{"b", "a", "b", "", "a"}); !equalStrings(got, []string{"", "a", "b"}) {
		t.Errorf("去重升序不符：%v", got)
	}

	if got := unionSorted(nil, []string{"b"}); !equalStrings(got, []string{"b"}) {
		t.Errorf("nil ∪ b 应为 [b]：%v", got)
	}
	if got := unionSorted([]string{"a"}, nil); !equalStrings(got, []string{"a"}) {
		t.Errorf("a ∪ nil 应为 [a]：%v", got)
	}
	if got := unionSorted([]string{"a", "c"}, []string{"b", "c"}); !equalStrings(got, []string{"a", "b", "c"}) {
		t.Errorf("并集去重升序不符：%v", got)
	}

	s := "keep"
	setFirst(&s, nil)
	if s != "keep" {
		t.Errorf("列表为空时应保持原值，得到 %q", s)
	}
	setFirst(&s, []string{"first", "second"})
	if s != "first" {
		t.Errorf("应取列表首项，得到 %q", s)
	}

	if !attrHit(nil, "prod-dep-a", []string{"dep-a"}, true) {
		t.Error("列表为空时应回退单值列做模糊匹配")
	}
	if attrHit(nil, "prod-dep-a", []string{"dep-x"}, true) {
		t.Error("单值列未命中应返回 false")
	}
	if !attrHit([]string{"n1", "n2"}, "n1", []string{"n2", "nX"}, false) {
		t.Error("列表内精确命中（多值 OR）应返回 true")
	}
	if attrHit([]string{"n1", "n2"}, "n1", []string{"n3"}, false) {
		t.Error("列表与单值列均未命中应返回 false")
	}
	if !attrHit([]string{"n1", "n2"}, "n1", nil, false) {
		t.Error("空 wanted（不参与筛选）应恒命中")
	}
}

// equalStrings 逐项比较字符串切片（nil 与空切片视为不等，便于校验「未补充属性」）
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// relationRowsEqual：多重集比较（行序无关），时间列不参与，业务列/行数有差异即判定不等
func TestRelationRowsEqual(t *testing.T) {
	t0 := time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local)
	t1 := time.Date(2026, 9, 17, 10, 0, 0, 0, time.Local)
	base := func() []model.AppDpRelation {
		return []model.AppDpRelation{
			{Id: 1, ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1", CreateTime: &t0, UpdateTime: &t0},
			{Id: 2, ApolloAppId: "app-2", ApolloAppEnv: "DEV", DeployProjectName: "dep-b", Node: "n2", CreateTime: &t0, UpdateTime: &t0},
		}
	}

	tests := []struct {
		name     string
		incoming []model.AppDpRelation
		want     bool
	}{
		{
			name:     "完全一致",
			incoming: base(),
			want:     true,
		},
		{
			name: "行序不同视为同一批",
			incoming: []model.AppDpRelation{
				{Id: 2, ApolloAppId: "app-2", ApolloAppEnv: "DEV", DeployProjectName: "dep-b", Node: "n2"},
				{Id: 1, ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1"},
			},
			want: true,
		},
		{
			name: "仅时间列刷新不算变化",
			incoming: []model.AppDpRelation{
				{Id: 1, ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1", CreateTime: &t1, UpdateTime: &t1},
				{Id: 2, ApolloAppId: "app-2", ApolloAppEnv: "DEV", DeployProjectName: "dep-b", Node: "n2", CreateTime: &t1, UpdateTime: &t1},
			},
			want: true,
		},
		{
			name: "行数不同判不等",
			incoming: []model.AppDpRelation{
				{Id: 1, ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1"},
			},
			want: false,
		},
		{
			name: "业务字段变化判不等",
			incoming: []model.AppDpRelation{
				{Id: 1, ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a-改", Node: "n1"},
				{Id: 2, ApolloAppId: "app-2", ApolloAppEnv: "DEV", DeployProjectName: "dep-b", Node: "n2"},
			},
			want: false,
		},
	}
	existing := base()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := relationRowsEqual(existing, tc.incoming); got != tc.want {
				t.Errorf("relationRowsEqual = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// replaceRelationDB：仅在数据有变化时「清空 + 重写」，无变化跳过（表内不产生无谓膨胀）
func TestReplaceRelationDBOnlyRewritesOnChange(t *testing.T) {
	db := snapshotTestDB(t, &model.AppDpRelation{})
	relationMu.Lock()
	prev := relationDB
	relationDB = db
	relationMu.Unlock()
	t.Cleanup(func() {
		relationMu.Lock()
		relationDB = prev
		relationMu.Unlock()
	})

	t0 := time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local)
	rows := []model.AppDpRelation{
		{Id: 1, ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1", CreateTime: &t0},
		{Id: 2, ApolloAppId: "app-2", ApolloAppEnv: "DEV", DeployProjectName: "dep-b", Node: "n2", CreateTime: &t0},
	}
	total := func() int64 {
		var n int64
		db.Model(&model.AppDpRelation{}).Count(&n)
		return n
	}

	// 首次同步：库内为空 → 写入
	n, wrote, unchanged, errMsg := replaceRelationDB(rows)
	if n != 2 || !wrote || unchanged || errMsg != "" {
		t.Fatalf("首次落库 = (rows=%d wrote=%v unchanged=%v err=%q)，期望 (2 true false \"\")", n, wrote, unchanged, errMsg)
	}
	if got := total(); got != 2 {
		t.Fatalf("首次落库后表内 %d 行，期望 2", got)
	}

	// 数据无变化（行序打乱 + 仅时间戳刷新）→ 跳过替换，表内数据保持原样
	reordered := []model.AppDpRelation{
		{Id: 2, ApolloAppId: "app-2", ApolloAppEnv: "DEV", DeployProjectName: "dep-b", Node: "n2", UpdateTime: &t0},
		{Id: 1, ApolloAppId: "app-1", ApolloAppEnv: "PRO", DeployProjectName: "dep-a", Node: "n1", UpdateTime: &t0},
	}
	n, wrote, unchanged, errMsg = replaceRelationDB(reordered)
	if n != 2 || wrote || !unchanged || errMsg != "" {
		t.Fatalf("无变化 = (rows=%d wrote=%v unchanged=%v err=%q)，期望 (2 false true \"\")", n, wrote, unchanged, errMsg)
	}

	// 数据有变化 → 清空重写，表内只剩新数据
	changed := []model.AppDpRelation{
		{Id: 3, ApolloAppId: "app-3", ApolloAppEnv: "PRO", DeployProjectName: "dep-c", Node: "n3"},
	}
	n, wrote, unchanged, errMsg = replaceRelationDB(changed)
	if n != 1 || !wrote || unchanged || errMsg != "" {
		t.Fatalf("有变化 = (rows=%d wrote=%v unchanged=%v err=%q)，期望 (1 true false \"\")", n, wrote, unchanged, errMsg)
	}
	var got []model.AppDpRelation
	if err := db.Order("id").Find(&got).Error; err != nil {
		t.Fatalf("读取关系表失败: %v", err)
	}
	if len(got) != 1 || got[0].ApolloAppId != "app-3" || got[0].DeployProjectName != "dep-c" {
		t.Errorf("替换后表内数据不符（应只剩本次写入行）：%+v", got)
	}

	// 无 DB 连接 / 空输入：正常降级，不报异常
	relationMu.Lock()
	relationDB = nil
	relationMu.Unlock()
	if n, wrote, unchanged, errMsg := replaceRelationDB(changed); n != 0 || wrote || unchanged || errMsg != "" {
		t.Errorf("无 DB 连接 = (rows=%d wrote=%v unchanged=%v err=%q)，期望全零值", n, wrote, unchanged, errMsg)
	}
	relationMu.Lock()
	relationDB = db
	relationMu.Unlock()
	if n, wrote, unchanged, errMsg := replaceRelationDB(nil); n != 0 || wrote || unchanged || errMsg != "" {
		t.Errorf("空输入 = (rows=%d wrote=%v unchanged=%v err=%q)，期望全零值", n, wrote, unchanged, errMsg)
	}
}

// replaceRelationDB：关系表缺失时视为异常（需运维先执行建表脚本），不静默通过
func TestReplaceRelationDBMissingTable(t *testing.T) {
	db := snapshotTestDB(t) // 不建 app_dp_relation 表
	relationMu.Lock()
	prev := relationDB
	relationDB = db
	relationMu.Unlock()
	t.Cleanup(func() {
		relationMu.Lock()
		relationDB = prev
		relationMu.Unlock()
	})

	n, wrote, unchanged, errMsg := replaceRelationDB([]model.AppDpRelation{{Id: 1, ApolloAppId: "app-1"}})
	if n != 0 || wrote || unchanged || errMsg == "" {
		t.Errorf("表缺失 = (rows=%d wrote=%v unchanged=%v err=%q)，期望 err 非空且未写入", n, wrote, unchanged, errMsg)
	}
}

// deploySlots：按「部署项目 × 节点」聚成部署点，工程项目取首值、PP 应用保留全量
func TestDeploySlotsGroupByDepAndNode(t *testing.T) {
	targets := map[string][]dpTarget{
		relationKeyOf("app-1", "PRO"): {
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n1", PpApplication: "pp-1"},
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n1", PpApplication: "pp-2"}, // 同部署点：PP 多值聚合
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n2", PpApplication: "pp-3"}, // 同部署项目、不同节点 → 新部署点
		},
		relationKeyOf("app-2", "PRO"): {
			{DepProject: "dep-b", Node: "n9"}, // DPR 未命中：无 PP
		},
	}
	slots := deploySlots(targets)

	key := relationKeyOf("app-1", "PRO")
	if len(slots[key]) != 2 {
		t.Fatalf("app-1|PRO 应聚出 2 个部署点（n1、n2），得到 %d：%+v", len(slots[key]), slots[key])
	}
	s0 := slots[key][0]
	if s0.DepProject != "dep-a" || s0.Node != "n1" || s0.EgProject != "eg-a" {
		t.Errorf("部署点①单值不符：%+v", s0)
	}
	if !equalStrings(s0.PpApps, []string{"pp-1", "pp-2"}) {
		t.Errorf("同节点下 PP 应用应去重升序聚合：%v", s0.PpApps)
	}
	if s1 := slots[key][1]; s1.Node != "n2" || !equalStrings(s1.PpApps, []string{"pp-3"}) {
		t.Errorf("部署点②（n2）不符：%+v", s1)
	}

	if app2 := slots[relationKeyOf("app-2", "PRO")]; len(app2) != 1 || app2[0].PpApps != nil {
		t.Errorf("DPR 未命中的部署点应保留、PP 列表为空：%+v", app2)
	}
}

// expandByDeploySlot：「appId × 中间件」行 → 「(appId, 部署项目, 节点) × 中间件」行（部署点展开，默认不扩行属性清空）
func TestExpandByDeploySlot(t *testing.T) {
	savedIdx, savedAt, savedOK, savedSrc, savedRows := relationIndex, relationLoadedAt, relationOK, relationSource, relationRows
	savedTTL := global.GVA_CONFIG.Apollo.RelationApiTTL
	defer func() {
		relationIndex, relationLoadedAt, relationOK, relationSource, relationRows = savedIdx, savedAt, savedOK, savedSrc, savedRows
		global.GVA_CONFIG.Apollo.RelationApiTTL = savedTTL
	}()
	global.GVA_CONFIG.Apollo.RelationApiTTL = 60
	relationIndex = map[string][]dpTarget{
		relationKeyOf("app-1", "PRO"): {
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n1", PpApplication: "pp-1"},
			{DepProject: "dep-a", EgProject: "eg-a", Node: "n2", PpApplication: "pp-3"},
		},
	}
	relationLoadedAt, relationOK, relationSource, relationRows = time.Now(), true, sourceDB, 2

	// 两行同 appId×中间件（不同中间件）：展开后应「每个中间件 × 每个部署点」
	rows := []model.OperationTopology{
		{ApolloAppId: "app-1", Env: "PRO", MiddlewareType: "mysql", ConnectAddr: "db:3306"},
		{ApolloAppId: "app-1", Env: "PRO", MiddlewareType: "redis", ConnectAddr: "r:6379"},
	}
	out := expandByDeploySlot(rows)
	// 行序：外层遍历原行（中间件），内层遍历部署点
	if len(out) != 4 {
		t.Fatalf("展开后应为 4 行（2 中间件 × 2 部署点），得到 %d", len(out))
	}
	if out[0].Node != "n1" || out[0].ConnectAddr != "db:3306" || out[0].DepProject != "dep-a" {
		t.Errorf("第 1 行应为 (MySQL × n1)：%+v", out[0])
	}
	if out[1].Node != "n2" || out[1].ConnectAddr != "db:3306" {
		t.Errorf("第 2 行应为 (MySQL × n2)：%+v", out[1])
	}
	if out[2].ConnectAddr != "r:6379" || out[2].Node != "n1" {
		t.Errorf("第 3 行应为 (Redis × n1)：%+v", out[2])
	}
	// 展开后单值收敛、多值列表清空；PP 应用仍为部署点下列表、首值取首
	if out[0].Nodes != nil || out[0].DepProjects != nil || out[0].EngProjects != nil {
		t.Errorf("展开后多值列表应清空（前端改读单值列）：%+v", out[0])
	}
	if !equalStrings(out[0].PpApplications, []string{"pp-1"}) || out[0].PpApplication != "pp-1" {
		t.Errorf("展开后 PP 应用应为该部署点列表：%+v", out[0])
	}

	// 无部署点可用的行原样保留（不丢行）
	orphan := []model.OperationTopology{{ApolloAppId: "app-9", Env: "PRO", MiddlewareType: "mysql", ConnectAddr: "db:3306"}}
	if got := expandByDeploySlot(orphan); len(got) != 1 || got[0].ApolloAppId != "app-9" {
		t.Errorf("无部署点的行应原样保留：%+v", got)
	}
}

// filterSlotRows：展开模式下按「部署点」复核（只保留命中的那个部署点的行）
func TestFilterSlotRows(t *testing.T) {
	// 展开后的行已是部署点粒度，字段为单值
	rows := []model.OperationTopology{
		{ApolloAppId: "app-1", DepProject: "dep-a", Node: "n1", EngProject: "eg-a", PpApplications: []string{"pp-1"}, PpApplication: "pp-1"},
		{ApolloAppId: "app-1", DepProject: "dep-a", Node: "n2", EngProject: "eg-a", PpApplications: []string{"pp-3"}, PpApplication: "pp-3"},
	}
	if got := filterSlotRows(rows, appAttrQuery{DepProject: []string{"dep-a"}}); len(got) != 2 {
		t.Errorf("部署项目命中应保留该 appId 全部部署点（2），得到 %d", len(got))
	}
	if got := filterSlotRows(rows, appAttrQuery{Node: []string{"n1"}}); len(got) != 1 || got[0].Node != "n1" {
		t.Errorf("节点精确命中应只保留 n1 部署点（1），得到 %d", len(got))
	}
	// 与 filterAttachRows（appId 粒度）区别：这里同一 appId 的其它部署点行会被剔除
	if got := filterSlotRows(rows, appAttrQuery{Node: []string{"n1"}}); len(got) != 1 {
		t.Errorf("展开模式按节点筛选应剔除其它部署点（仅保留 1），得到 %d", len(got))
	}
	if got := filterSlotRows(rows, appAttrQuery{PpApplication: []string{"pp-9"}}); len(got) != 0 {
		t.Errorf("PP 未命中应不留行，得到 %d", len(got))
	}
}

// appKeyOf + expandDepNodeOn：两种节点粒度口径（默认展开 = appId|部署项目|节点）
func TestAppKeyOfModes(t *testing.T) {
	// expandDepNodeOn：nil / true → 展开；false → 折叠
	if !expandDepNodeOn(nil) {
		t.Error("nil（不传）应默认展开")
	}
	tf, ft := true, false
	if !expandDepNodeOn(&tf) {
		t.Error("显式 true 应展开")
	}
	if expandDepNodeOn(&ft) {
		t.Error("显式 false 应折叠")
	}

	full := model.OperationTopology{ApolloAppId: "app-1", DepProject: "dep-a", Node: "n1"}
	if got := appKeyOf(full, false); got != "app-1" {
		t.Errorf("折叠模式应取 appId：%s", got)
	}
	if got := appKeyOf(full, true); got != "app-1|dep-a|n1" {
		t.Errorf("展开模式应取 appId|部署项目|节点：%s", got)
	}
	// 属性缺失回退 appId（不拼接空段）
	if got := appKeyOf(model.OperationTopology{ApolloAppId: "app-1"}, true); got != "app-1" {
		t.Errorf("展开但无部署点时回退 appId：%s", got)
	}
	// 不同 appId 共用同一部署项目时不应产生相同节点键
	other := model.OperationTopology{ApolloAppId: "app-2", DepProject: "dep-shared", Node: "n1"}
	if full2 := appKeyOf(model.OperationTopology{ApolloAppId: "app-1", DepProject: "dep-shared", Node: "n1"}, true); full2 == appKeyOf(other, true) {
		t.Errorf("不同 appId 的展开键不应相同：%s == %s", full2, appKeyOf(other, true))
	}
}
