package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"devops-automation/api/tprel/model"
	"devops-automation/api/tprel/tplog"
	"devops-automation/config"
	"devops-automation/global"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// =========================================================
// 运维关系拓扑：数据同步 / 解析规则 / 查询 / 拓扑聚合
// 对应文档《运维关系拓扑图方案》§4（数据获取）§5（定时同步）§6（展示）
// =========================================================

// MiddlewareRule 中间件解析规则（§4.4 解析规则表）
type MiddlewareRule struct {
	Keyword string // key 特征关键字
	Type    string // 中间件类型
	Project string // 默认中间件项目名
}

// middlewareRules key 特征规则表（Contains 子串匹配，对 key **大小写不敏感**，规则一律小写）：
//   - 规则**按顺序匹配、第一个命中即停**；同类型内按“精确 → 别名/简写 → 宽泛兜底”排列，
//     宽泛词（裸 mysql/redis/mongodb/rocketmq/... 等）放在同组靠后，避免提前吞掉其它别称（见《接口逻辑说明》§12 注意事项 2）。
//   - 覆盖 10 种中间件：mysql/redis/kafka/rocketmq/elasticsearch/mongodb/memcached/zookeeper/nacos/rabbitmq。
//   - 别名目标：真实 Apollo/Spring 配置里的常见简写与变体写法，例如 Kafka 的 bootstrap.servers(点) 与
//     bootstrap-servers(横线)、redisson 连接项、spring.data.mongo(不带 b)、memcache(不带 d)、zk.*、rabbit.*、裸 amqp 等，
//     避免因 key 写法差异漏匹配（能否成行还受“地址可提取”闸门约束，纯参数 key 不会产生伪关系）。
var middlewareRules = []MiddlewareRule{
	// —— mysql ——
	{Keyword: "spring.datasource", Type: "mysql", Project: "mysql"},
	{Keyword: "druid", Type: "mysql", Project: "mysql"}, // Druid 数据源（独立前缀时）
	{Keyword: "jdbc:mysql", Type: "mysql", Project: "mysql"},
	{Keyword: "mysql", Type: "mysql", Project: "mysql"}, // 宽泛兜底（mysql.* 等）
	// —— redis ——
	{Keyword: "spring.data.redis", Type: "redis", Project: "redis"},
	{Keyword: "spring.redis", Type: "redis", Project: "redis"},
	{Keyword: "redisson", Type: "redis", Project: "redis"}, // Redisson 连接配置（singleServerConfig.address 等）
	{Keyword: "redis", Type: "redis", Project: "redis"},    // 宽泛兜底（redis.* / rediss / 自定义 redis-cluster 等）
	// —— kafka ——
	{Keyword: "bootstrap-servers", Type: "kafka", Project: "kafka"}, // Spring 风格（横线）
	{Keyword: "bootstrap.servers", Type: "kafka", Project: "kafka"}, // Kafka 原生属性风格（点）
	{Keyword: "kafka", Type: "kafka", Project: "kafka"},             // 宽泛兜底（kafka.*）
	// —— rocketmq ——
	{Keyword: "rocketmq.name-server", Type: "rocketmq", Project: "rocketmq"},
	{Keyword: "rocketmq", Type: "rocketmq", Project: "rocketmq"}, // 宽泛兜底（含 camel 风格 namesrvAddr 等）
	// —— elasticsearch ——
	{Keyword: "elasticsearch", Type: "elasticsearch", Project: "elasticsearch"}, // 覆盖 spring.elasticsearch.* / elasticsearch.*
	{Keyword: "es.cluster", Type: "elasticsearch", Project: "elasticsearch"},
	// —— mongodb ——
	{Keyword: "spring.data.mongodb", Type: "mongodb", Project: "mongodb"},
	{Keyword: "mongodb", Type: "mongodb", Project: "mongodb"},           // 宽泛兜底（mongodb.*）
	{Keyword: "spring.data.mongo", Type: "mongodb", Project: "mongodb"}, // 别名：不带 b（真实存在 spring.data.mongo.uri）
	{Keyword: "mongo", Type: "mongodb", Project: "mongodb"},             // 宽泛兜底（mongo.*；mongodb 已在前命中）
	// —— memcached ——
	{Keyword: "memcached", Type: "memcached", Project: "memcached"},
	{Keyword: "memcache", Type: "memcached", Project: "memcached"}, // 别名：不带 d（memcache.servers 常见）
	// —— zookeeper ——
	{Keyword: "zookeeper.address", Type: "zookeeper", Project: "zookeeper"},
	{Keyword: "zookeeper", Type: "zookeeper", Project: "zookeeper"}, // 宽泛兜底（zookeeper.servers/hosts）
	{Keyword: "zk.", Type: "zookeeper", Project: "zookeeper"},       // 简写别名（zk.address/servers；带点降低误伤）
	// —— nacos ——
	{Keyword: "nacos.server-addr", Type: "nacos", Project: "nacos"},
	{Keyword: "nacos", Type: "nacos", Project: "nacos"}, // 宽泛兜底（含 spring.cloud.nacos.*）
	// —— rabbitmq / amqp ——
	{Keyword: "spring.rabbitmq", Type: "rabbitmq", Project: "rabbitmq"},
	{Keyword: "rabbitmq", Type: "rabbitmq", Project: "rabbitmq"},
	{Keyword: "amqp", Type: "rabbitmq", Project: "rabbitmq"},   // 别名（amqp.host/amqp.uri；amqps 同样含 amqp）
	{Keyword: "rabbit", Type: "rabbitmq", Project: "rabbitmq"}, // 别名（rabbit.host 等）
}

// middlewareNsHints 中间件型 namespace 名单（小写）：仅当此类 namespace 404 缺失时，才做“应用未接入该中间件”的应用级提示。
// application/common/config/logging 等业务/公共空间不属于中间件，其缺失不参与提示（避免刷屏）。
var middlewareNsHints = map[string]bool{
	"datasource": true, "mysql": true, "redis": true, "kafka": true, "rocketmq": true,
	"mq": true, "elasticsearch": true, "mongodb": true, "middleware": true, "cache": true,
}

// multiAddrTypes 值为多个 host:port 列表的中间件类型
var multiAddrTypes = map[string]bool{"kafka": true, "rocketmq": true, "zookeeper": true, "nacos": true}

var hostPortRe = regexp.MustCompile(`([a-zA-Z0-9.\-_]+):(\d{2,5})`)

// hostOnlyRe 纯主机名（不含端口/协议/路径），用于识别 host/port 分离配置中的主机项
var hostOnlyRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9.\-_]*$`)

// digitsRe 纯数字（端口/超时/线程池等数值配置，不能当作主机）
var digitsRe = regexp.MustCompile(`^\d+$`)

// portRe 从值中提取端口号
var portRe = regexp.MustCompile(`\d{2,5}`)

// defaultPorts 各中间件默认端口（host/port 分离配置且未显式配置 *.port 时兜底，
// 真实样例：spring.redis.host 有值但缺失 spring.redis.port 时用 6379）
var defaultPorts = map[string]string{
	"mysql": "3306", "redis": "6379", "mongodb": "27017", "rabbitmq": "5672",
	"kafka": "9092", "rocketmq": "9876", "elasticsearch": "9200",
	"memcached": "11211", "zookeeper": "2181", "nacos": "8848",
}

// hostKeySuffixes 主机型配置 key 的后缀（按长度降序，避免 .server 抢先匹配 .server-addr）。
// 这类 key 的 value 通常是「纯主机名」，端口在独立的 前缀.port 配置项中（真实 Apollo 常见写法）。
var hostKeySuffixes = []string{
	".cluster-nodes", ".server-addr", ".servers", ".address", ".server", ".nodes", ".host", ".addr",
}

// sensitiveKeyHints 敏感/非连接配置特征（命中则跳过解析，密码/账号等绝不落库，也避免无谓的规则匹配）
var sensitiveKeyHints = []string{
	"password", "passwd", "secret", "credential", "username", ".user",
	"access-key", "accesskey", "private-key", "token",
}

// isSensitiveKey 判断配置 key 是否为敏感/非连接项（密码、账号、密钥等）
func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, h := range sensitiveKeyHints {
		if strings.Contains(k, h) {
			return true
		}
	}
	return false
}

// combineHostPort 处理 host/port 分离配置（真实 Apollo 常见：spring.redis.host=纯域名，spring.redis.port=端口）。
// 当 key 为主机型后缀且 value 是纯主机名时，从同 namespace 的 kv 中查找「前缀.port」组合成 host:port；
// 找不到显式端口则用该中间件默认端口兜底。返回 nil 表示不适用（非主机型 key 或 value 非法）。
func combineHostPort(kv map[string]string, key, value, mwType string) []string {
	lowerKey := strings.ToLower(key)
	prefix := ""
	isHostKey := false
	for _, suf := range hostKeySuffixes {
		if strings.HasSuffix(lowerKey, suf) {
			prefix = key[:len(key)-len(suf)]
			isHostKey = true
			break
		}
	}
	if !isHostKey {
		return nil
	}
	// value 可能是逗号/分号分隔的主机列表（如集群节点），逐个校验
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
	var addrs []string
	seen := map[string]bool{}
	// 预先在 kv 中查找「前缀.port」（大小写不敏感）
	wantPortKey := strings.ToLower(prefix + ".port")
	explicitPort := ""
	for k, v := range kv {
		if strings.ToLower(k) == wantPortKey {
			explicitPort = strings.TrimSpace(v)
			break
		}
	}
	for _, p := range parts {
		host := strings.TrimSpace(p)
		host = strings.TrimPrefix(host, "http://")
		host = strings.TrimPrefix(host, "https://")
		if host == "" || strings.ContainsAny(host, ":/?") {
			continue // 含端口/路径/协议残留 → 交由 extractAddrs 处理，不在此组合
		}
		if digitsRe.MatchString(host) || !hostOnlyRe.MatchString(host) {
			continue // 纯数字（端口值）或非法主机名 → 跳过
		}
		port := ""
		if m := portRe.FindString(explicitPort); m != "" {
			port = m
		}
		if port == "" {
			port = defaultPorts[mwType]
		}
		if port == "" {
			continue
		}
		addr := host + ":" + port
		if !seen[addr] {
			seen[addr] = true
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

// valueRules value 兜底匹配专用规则（仅认明确的连接协议前缀，避免 key 型宽泛词误报）
//   - 匹配对 value **大小写不敏感**（value 统一小写后比对，Redis://、JDBC:mysql:// 等大写变体同样命中）
//   - 协议变体尽量收全：jdbc:mysql 前缀可覆盖 loadbalance/replication 等后缀；mongodb+srv(Atlas/云上)/amqps/rediss 等加密变体
//   - 如 kafka/rocketmq/nacos/es 的 value 只是裸 host:port 列表无协议标识，仅依赖 key 规则命中
var valueRules = []MiddlewareRule{
	{Keyword: "jdbc:mysql", Type: "mysql", Project: "mysql"}, // 覆盖 jdbc:mysql://、jdbc:mysql:loadbalance://、jdbc:mysql:replication://
	{Keyword: "mysql://", Type: "mysql", Project: "mysql"},
	{Keyword: "redis://", Type: "redis", Project: "redis"},
	{Keyword: "rediss://", Type: "redis", Project: "redis"}, // TLS 变体
	{Keyword: "mongodb://", Type: "mongodb", Project: "mongodb"},
	{Keyword: "mongodb+srv://", Type: "mongodb", Project: "mongodb"}, // Atlas/云 Mongo 变体
	{Keyword: "amqp://", Type: "rabbitmq", Project: "rabbitmq"},
	{Keyword: "amqps://", Type: "rabbitmq", Project: "rabbitmq"}, // TLS 变体
	{Keyword: "memcached://", Type: "memcached", Project: "memcached"},
	{Keyword: "memcache://", Type: "memcached", Project: "memcached"}, // 别名拼写
	{Keyword: "zookeeper://", Type: "zookeeper", Project: "zookeeper"},
}

// valueKeywords valueRules 关键字的小写预编译表（启动时一次转换，避免每个 value 匹配时重复 ToLower）
var valueKeywords = func() []string {
	ks := make([]string, len(valueRules))
	for i := range valueRules {
		ks[i] = strings.ToLower(valueRules[i].Keyword)
	}
	return ks
}()

// middlewareKeywords = middlewareRules 关键词的小写预编译表（与规则同序，启动一次转换；
// 匹配对 key 大小写不敏感，避免 Spring/自定义配置里出现 Spring.Redis、REDIS.HOST 等写法漏匹配）
var middlewareKeywords = func() []string {
	ks := make([]string, len(middlewareRules))
	for i := range middlewareRules {
		ks[i] = strings.ToLower(middlewareRules[i].Keyword)
	}
	return ks
}()

// matchRule 按优先级返回第一个命中的规则（key 大小写不敏感；规则顺序敏感，见 middlewareRules 注释）
func matchRule(key string) *MiddlewareRule {
	k := strings.ToLower(key)
	for i, kw := range middlewareKeywords {
		if kw != "" && strings.Contains(k, kw) {
			return &middlewareRules[i]
		}
	}
	return nil
}

// matchValueRule 按 value 内容识别中间件类型（双路径补漏：key 不标准但 value 含明确连接协议，
// 如 jdbc:mysql://、redis:// 等）。仅作为 key 匹配无命中时的兜底路径，使用独立 valueRules 防误报。
func matchValueRule(value string) *MiddlewareRule {
	v := strings.ToLower(value)
	for i, kw := range valueKeywords {
		if kw != "" && strings.Contains(v, kw) {
			return &valueRules[i]
		}
	}
	return nil
}

// extractAddrs 从配置 value 中提取连接地址（只留 host:port，丢弃用户名/密码）
func extractAddrs(value, mwType string) []string {
	matches := hostPortRe.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return nil
	}
	var addrs []string
	seen := map[string]bool{}
	collect := func(m []string) {
		addr := m[1] + ":" + m[2]
		if !seen[addr] {
			seen[addr] = true
			addrs = append(addrs, addr)
		}
	}
	if multiAddrTypes[mwType] {
		for _, m := range matches {
			collect(m)
		}
	} else {
		collect(matches[0])
	}
	return addrs
}

// ownerFor 应用 → PP 应用名 预计算（PP 维度暂以 appid_owner_map 配置 / 应用名兜底占位）。
//
//	Node（机房/地域）、EngProject（工程）、DepProject（部署项目）三个维度采集期不赋值、不落库，
//	由查询期的两跳关系（app_dp_relation → deploy_pinpoint_relation）作为 appId 组件的属性回填，
//	见 dp_relation.go attachAppAttrs（属性多值聚合为列表，不扩行）。
func ownerFor(app ApolloApp) config.AppidOwner {
	owner := config.AppidOwner{}
	if o, ok := global.GVA_CONFIG.Apollo.AppidOwnerMap[app.AppId]; ok {
		owner.PpApplication = o.PpApplication
	}
	if owner.PpApplication == "" {
		owner.PpApplication = app.Name
	}
	return owner
}

// extractDbName 从连接串 value 中提取库/数据库名（增强溯源字段，如 jdbc:mysql://host:13302/vcp_saas → vcp_saas；
// mongodb://host:27017/user_db → user_db）。仅取 host:port 后的第一个路径段，忽略 query/fragment 与纯数字段（redis db 下标）。
func extractDbName(value string) string {
	searchFrom := 0
	if idx := strings.Index(value, "://"); idx >= 0 {
		searchFrom = idx + 3
	}
	rest := value[searchFrom:]
	m := hostPortRe.FindStringIndex(rest)
	if m == nil || m[1] >= len(rest) || rest[m[1]] != '/' {
		return ""
	}
	seg := strings.SplitN(rest[m[1]+1:], "/", 2)[0]
	if i := strings.IndexAny(seg, "?#&"); i >= 0 {
		seg = seg[:i]
	}
	seg = strings.TrimSpace(seg)
	if seg == "" || digitsRe.MatchString(seg) {
		return ""
	}
	return seg
}

// parseItemsToRelations 解析单个 namespace 的配置项 → 关系记录（§4.4）
//
// 解析在 namespace 维度进行（而非逐 item 孤立判断）：
//  1. 先建 key→value 索引，过滤空 key 占位项（真实 Apollo 常见空行）；
//  2. 双路径匹配规则：key 特征优先，value 连接协议兜底；
//  3. 敏感项（password/secret/username 等）显式排除，绝不落库；
//  4. value 内联 host:port 直接提取；value 为纯主机名（host/port 分离）时
//     组合同 namespace 的「前缀.port」，缺失则用中间件默认端口兜底。
//  5. 从 value 连接串提取库名（middleware_db 增强字段，不参与合并/去重）。
//
// host 为来源 Apollo 实例（套）层归属元数据（含访问地址/secondProductId/appCount），
// app 同时携带应用名/部门/负责人（接口2 元数据），随关系行冗余落库（页面各层展示）
func parseItemsToRelations(app ApolloApp, host instanceMeta, env, ns, syncDate string, items []ApolloItem, owner config.AppidOwner) []model.OperationTopology {
	// 1. 建立 kv 索引（过滤空 key 占位项），供 host/port 分离组合时回查 .port
	kv := make(map[string]string, len(items))
	for _, it := range items {
		k := strings.TrimSpace(it.Key)
		if k == "" {
			continue // 空 key 占位项（真实样例 vcp-saas-jxc 含 5 个空行）
		}
		kv[k] = it.Value
	}
	rels := make([]model.OperationTopology, 0, 8)
	seen := map[string]bool{} // 本 namespace 内按 类型|地址 去重
	for key, value := range kv {
		// 3. 敏感/非连接项显式排除（密码、账号、密钥等绝不落库）
		if isSensitiveKey(key) {
			continue
		}
		// 2. 双路径融合解析：优先按 key 特征命中；key 不标准时按 value 内容兜底识别
		rule := matchRule(key)
		hitVia := "key"
		if rule == nil {
			rule = matchValueRule(value)
			hitVia = "value"
		}
		if rule == nil {
			continue
		}
		// 4a. value 内联 host:port 直接提取
		addrs := extractAddrs(value, rule.Type)
		// 4b. host/port 分离：value 为纯主机名时，组合同 namespace 的 .port / 默认端口
		if len(addrs) == 0 {
			if comb := combineHostPort(kv, key, value, rule.Type); len(comb) > 0 {
				addrs = comb
				hitVia = hitVia + "+port"
			}
		}
		if len(addrs) == 0 {
			continue
		}
		tplog.L().Debug("[operation_topology] 规则命中",
			zap.String("appId", app.AppId), zap.String("ns", ns),
			zap.String("key", key), zap.String("hitVia", hitVia),
			zap.String("type", rule.Type), zap.Strings("addrs", addrs))
		for _, addr := range addrs {
			rk := rule.Type + "|" + addr
			if seen[rk] {
				continue
			}
			seen[rk] = true
			// node/eng_project/dep_project：与 CMDB 映射关系未明确，暂不赋值（预留空值）
			rels = append(rels, model.OperationTopology{
				PpApplication:     owner.PpApplication,
				ApolloAppId:       app.AppId,
				ApolloAppName:     app.Name,
				AppDept:           app.Dept(),
				AppOwner:          app.Leader(),
				ApolloHostId:      host.hostId,
				ApolloHostName:    host.hostName,
				ApolloHostAddr:    host.hostAddr,
				SecondProductId:   host.secondProductId,
				HostAppCount:      host.appCount,
				Namespace:         ns,
				ConfigKey:         key,
				MiddlewareType:    rule.Type,
				ConnectAddr:       addr,
				MiddlewareDb:      extractDbName(value),
				MiddlewareProject: rule.Project,
				Env:               env,
				SyncDate:          syncDate,
				Source:            "apollo",
			})
		}
	}
	return rels
}

// =========================================================
// 总览块渲染：每次跑批在日志「开头（计划）/ 末尾（结果）」输出一块
// 对齐的总览，方便日后只查日志快速掌握整批规模、结果与耗时。
// 格式为纯文本对齐行 + 少量分隔线，避免过多装饰框。
// =========================================================

// padR 按显示宽度（中文按 1 列）右补空格，保证 CJK/ASCII 混排时对齐
func padR(s string, w int) string {
	if n := utf8.RuneCountInString(s); n >= w {
		return s
	} else {
		return s + strings.Repeat(" ", w-n)
	}
}

// appendKV 向总览块追加一行对齐的 label/value
func appendKV(b *strings.Builder, label, value string) {
	b.WriteString("  ")
	b.WriteString(padR(label, 14))
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}

// miniBar 按 count/total 占比生成一条可视化条形（便于一眼看出分布差异）
func miniBar(count, total, width int) string {
	if count <= 0 || total <= 0 {
		return strings.Repeat(" ", width)
	}
	n := count * width / total
	if n < 1 {
		n = 1
	}
	if n > width {
		n = width
	}
	return strings.Repeat("█", n) + strings.Repeat(" ", width-n)
}

// typeDistBlock 中间件类型分布子块（按关系数降序，含去重节点数 + 条形 + 占比）
// stats：关系数（行级，同一应用跨多实例会重复计）；nodeStats：去重节点数（类型+地址）；
// total = 关系总数，作条形/占比基准；两列口径不同，便于与图节点数对账。
func typeDistBlock(stats, nodeStats map[string]int, total int) string {
	if total <= 0 || len(stats) == 0 {
		return "  （无解析产物）\n"
	}
	types := make([]string, 0, len(stats))
	for t := range stats {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return stats[types[i]] > stats[types[j]] })
	b := strings.Builder{}
	for _, t := range types {
		cnt := stats[t]
		b.WriteString("  ")
		b.WriteString(padR(t, 14))
		b.WriteString(fmt.Sprintf("%7d", cnt))
		b.WriteString(fmt.Sprintf("  节点 %4d  ", nodeStats[t]))
		b.WriteString(miniBar(cnt, total, 20))
		b.WriteString(fmt.Sprintf(" %5.1f%%\n", float64(cnt)/float64(total)*100))
	}
	return b.String()
}

// samplesBlock 取最多 n 个示例拼成一行列表（用于新增/消失关系等明细预览）
func samplesBlock(items []string, n int) string {
	if len(items) == 0 {
		return "（无）"
	}
	if len(items) > n {
		return strings.Join(items[:n], ", ") + fmt.Sprintf(" 等 %d 条", len(items))
	}
	return strings.Join(items, ", ")
}

// instanceMeta 实例（套）层归属元数据，随每条关系行冗余落库（页面实例筛选/归属展示）。
// secondProductId 为上游数组字段的逗号分隔串（如 "705,532"）。
type instanceMeta struct {
	hostId          int
	hostName        string
	hostAddr        string // 上游 host 字段（实例访问地址）
	secondProductId string
	appCount        int // 上游 appCount（该实例下应用总数）
}

// scanTask 一次扫描任务：套 × 环境 × 应用 × namespace
// owner 在组装阶段预计算（按 appId 一次），避免每个 (app×ns×env) 任务在 worker 内重复映射
type scanTask struct {
	host  instanceMeta
	env   string
	app   ApolloApp
	ns    string
	owner config.AppidOwner
}

// syncMu 防止定时任务与手动 /sync 并发执行（同日重复 DELETE+INSERT 竞争）
var syncMu sync.Mutex

// =========================================================
// 异步同步任务（POST /sync 提交后立即返回，GET /sync/status 轮询结果）
//   背景：全量同步为分钟级长任务，走 HTTP 网关（LVS/Tengine 默认几十秒超时）必然 504；
//   故改为“提交即返回 + 轮询状态”，彻底绕开网关超时。
//   执行体仍走 SyncOperationTopology（内部 syncMu 与 cron 天然互斥，不会双跑）。
// =========================================================

// SyncStats 本次同步的规模/统计（运行中实时更新，完成态即最终值；供前端在同步弹窗内展示总体数据）
//   - 运行中：hosts/apps 在任务组装后即确定；middleware/relations 随解析实时增长
//   - 完成后：hosts/apps/middleware/relations 与 result（hostCount/appCount/typeStats 总数/insertCount）一致
type SyncStats struct {
	Hosts      int `json:"hosts"`      // 扫描的 Apollo 套总数（同 result.hostCount）
	Apps       int `json:"apps"`       // 扫描的应用总数（任务组装后即确定，同 result.appCount）
	Middleware int `json:"middleware"` // 已识别中间件节点数（按 类型+地址 去重，随解析实时增长）
	Relations  int `json:"relations"`  // 已解析去重后的关系（应用↔中间件实例连接）条数（随解析实时增长）
}

// SyncTaskState 异步同步任务状态快照（/sync 与 /sync/status 共用）
type SyncTaskState struct {
	Running   bool              `json:"running"`   // 是否有任务在执行/排队
	TriggerBy string            `json:"triggerBy"` // 当前/最近任务触发方式
	StartTime string            `json:"startTime"` // 任务开始时间（空=空闲）
	EndTime   string            `json:"endTime"`   // 最近一次任务结束时间（空=尚未完成过）
	CostMs    int64             `json:"costMs"`    // 最近任务耗时 ms（running 中表示已执行时长）
	Error     string            `json:"error"`     // 最近一次任务错误信息（空=成功）
	StageText string            `json:"stageText"` // 运行中阶段文案（如 "正在扫描配置 456/1077"）；空闲/完成=空
	Percent   int               `json:"percent"`   // 运行中完成度 0~100；-1=当前阶段无定量进度（前端走不确定进度动画）
	Stats     *SyncStats        `json:"stats"`     // 本次任务规模/统计（实时→最终；尚无统计时为空）
	Result    *model.SyncResult `json:"result"`    // 最近一次成功任务的完整结果（失败时为空）
}

// syncTaskStore 当前异步同步任务状态（进程内单例，重启后清空）
type syncTaskStore struct {
	mu        sync.Mutex
	running   bool
	triggerBy string
	startTime time.Time
	endTime   time.Time
	errText   string
	stageText string
	percent   int
	stats     SyncStats
	statsSet  bool
	last      *model.SyncResult
}

var currentSyncTask = &syncTaskStore{}

const syncTimeFmt = "2006-01-02 15:04:05"

// snapshotLocked 构造对外状态快照（调用方需已持锁）
func (s *syncTaskStore) snapshotLocked() SyncTaskState {
	st := SyncTaskState{
		Running:   s.running,
		TriggerBy: s.triggerBy,
		StartTime: s.startTime.Format(syncTimeFmt),
		StageText: s.stageText,
		Percent:   s.percent,
		Error:     s.errText,
		Result:    s.last,
	}
	if s.statsSet {
		copied := s.stats
		st.Stats = &copied
	}
	if s.running {
		st.CostMs = time.Since(s.startTime).Milliseconds()
	} else if !s.endTime.IsZero() {
		st.EndTime = s.endTime.Format(syncTimeFmt)
		st.CostMs = s.endTime.Sub(s.startTime).Milliseconds()
	}
	return st
}

// setProgress 采集阶段进度上报（由 run 注入 syncOperationTopology 的 worker 回调触发；非本次任务期写入直接忽略）
func (s *syncTaskStore) setProgress(stage string, percent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.stageText = stage
	s.percent = percent
}

// setStats 采集规模/统计上报（由 run 注入 syncOperationTopology 的 collector 回调触发；非本次任务期写入直接忽略）
func (s *syncTaskStore) setStats(st SyncStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.stats = st
	s.statsSet = true
}

// StartSyncTask 提交一次异步同步并立即返回；
// started=false 表示已有任务在执行/排队，本次未提交。
func StartSyncTask(trigger string) (started bool, state SyncTaskState) {
	s := currentSyncTask
	s.mu.Lock()
	if s.running {
		state = s.snapshotLocked()
		s.mu.Unlock()
		return false, state
	}
	s.running = true
	s.triggerBy = trigger
	s.startTime = time.Now()
	s.endTime = time.Time{}
	s.errText = ""
	s.stageText = "任务已提交，等待执行"
	s.percent = 0
	s.stats = SyncStats{}
	s.statsSet = false
	s.last = nil
	state = s.snapshotLocked()
	s.mu.Unlock()

	go s.run(trigger)
	return true, state
}

// SyncTaskStatus 当前异步同步任务状态（/sync/status 轮询用）
func SyncTaskStatus() SyncTaskState {
	s := currentSyncTask
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// run 后台执行体：调用 syncOperationTopology（内部 syncMu 与 cron 互斥，此处阻塞等待即排队）
// 通过进度回调把采集阶段实时写入任务状态，供 /sync/status 轮询展示。
func (s *syncTaskStore) run(trigger string) {
	defer func() {
		if p := recover(); p != nil {
			tplog.L().Error("[operation_topology] 异步同步任务 panic", zap.Any("panic", p))
			s.mu.Lock()
			s.errText = fmt.Sprintf("panic: %v", p)
			s.running = false
			s.endTime = time.Now()
			s.stageText = ""
			s.percent = 0
			s.mu.Unlock()
		}
	}()
	result, err := syncOperationTopology([]string{trigger}, s.setProgress, s.setStats)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endTime = time.Now()
	if err != nil {
		s.errText = err.Error()
	} else {
		s.last = &result
	}
	// 任务结束清空进度字段（result 为最终成果，stageText/percent 不再有意义）
	s.stageText = ""
	s.percent = 0
	s.running = false
}

// SyncOperationTopology 全量同步一次（cron 定时/启动自检等同步调用入口，无进度上报）
//
//	接口1 查套 → 并发接口2 查应用 → 并发接口3 抓配置解析 → 映射组装 → 事务落库
//	trigger 可选：标注本次触发方式（cron定时/手动同步等），用于日志总览块的“触发方式”
func SyncOperationTopology(trigger ...string) (model.SyncResult, error) {
	return syncOperationTopology(trigger, nil, nil)
}

// syncProgressFn 同步阶段进度回调：stage=阶段文案，percent=0~100（-1=阶段无定量进度）
type syncProgressFn func(stage string, percent int)

// syncStatsFn 同步规模/统计回调（collector 每收一批上报一次，运行中实时）
type syncStatsFn func(st SyncStats)

// syncOperationTopology 全量同步实现（SyncOperationTopology 的公共内核）
// onProgress 非 nil 时（异步任务场景）在关键阶段实时回调进度；onStats 非 nil 时上报规模统计；
// cron/启动自检传 nil 不影响性能。
func syncOperationTopology(trigger []string, onProgress syncProgressFn, onStats syncStatsFn) (model.SyncResult, error) {
	syncStart := time.Now()
	triggerBy := "手动/未知"
	if len(trigger) > 0 && strings.TrimSpace(trigger[0]) != "" {
		triggerBy = strings.TrimSpace(trigger[0])
	}
	startFmt := syncStart.Format("2006-01-02 15:04:05.000")
	syncMu.Lock()
	defer syncMu.Unlock()

	// 进度上报辅助：onProgress 为空（cron/启动自检）时不产生任何额外开销
	report := func(stage string, percent int) {
		if onProgress != nil {
			onProgress(stage, percent)
		}
	}
	// 规模统计上报辅助（collector 每收一批调用一次）
	reportStats := func(st SyncStats) {
		if onStats != nil {
			onStats(st)
		}
	}

	result := model.SyncResult{}
	cfg := global.GVA_CONFIG.Apollo

	client := NewApolloClient(cfg)
	envs := cfg.Envs
	if len(envs) == 0 {
		envs = []string{"PRO"}
	}
	namespaces := cfg.ScanNamespaces
	if len(namespaces) == 0 {
		// 全量覆盖：常见中间件特征所在 namespace + 基础 namespace（双路径融合，尽力覆盖）
		namespaces = []string{"application", "datasource", "redis", "logging", "common",
			"kafka", "rocketmq", "mq", "elasticsearch", "mongodb", "mysql",
			"cache", "config", "middleware"}
	}
	maxNs := cfg.MaxNsPerApp
	if maxNs <= 0 {
		maxNs = 15 // 放开默认上限以覆盖扩展 namespace 列表
	}
	if len(namespaces) > maxNs {
		namespaces = namespaces[:maxNs]
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = 8
	}
	budget := time.Duration(cfg.ScanBudgetSec) * time.Second
	if budget <= 0 {
		budget = 60 * time.Second
	}
	syncDate := time.Now().Format("2006-01-02")

	tplog.L().Info("[operation_topology] 同步入口/同步开始",
		zap.String("syncDate", syncDate),
		zap.Any("envs", envs), zap.Any("namespaces", namespaces),
		zap.Int("workers", workers), zap.String("scanBudget", budget.String()),
		zap.String("baseUrl", cfg.BaseUrl))

	// 0. 跳① 关系全量同步（appId ↔ 部署项目）：与拓扑主表同批刷新（cron/手动共用本内核），
	//    接口全量分页 → 与库内比对，有变化才整表替换落库 app_dp_relation（无变化跳过）→ 回写镜像 → 失效查询期索引，
	//    使查询期只读「库/镜像」，不再依赖上游关系接口可用性。
	report("正在同步 appId↔部署项目 关系", -1)
	relStat := SyncAppDpRelation()
	result.RelationRows, result.RelationWrote, result.RelationErr, result.RelationMsg =
		relStat.Rows, relStat.Wrote, relStat.Err, relStat.Msg

	// 1. 接口1：查套列表
	hosts, err := client.GetHostList()
	if err != nil {
		tplog.L().Error("[operation_topology] 获取 Apollo 套列表失败", zap.Error(err))
		return result, err
	}
	result.HostCount = len(hosts)
	tplog.L().Info("[operation_topology] 获取套列表完成", zap.Int("count", len(hosts)))
	report("正在探测 Apollo 套可用性", -1)

	// 1.4. APOLLO_HOST_NAME 可选过滤：按名称模糊筛选待同步的套（对应参考 _select_target_host）
	hosts = client.FilterHostsByName(hosts)
	result.HostCount = len(hosts)

	// 1.5. 并发探活所有套（TTL 缓存，标记可用/不可用，跳过不可用套的无效等待）
	// 对应参考实现 _probe_hosts_concurrent：并发探活避免串行等待，预算超时的套标记不可用
	probe := client.ProbeHosts(hosts, 30*time.Second)
	unavail := 0
	for _, p := range probe {
		if !p.Available {
			unavail++
		}
	}
	if unavail > 0 {
		tplog.L().Warn("[operation_topology] 探活发现不可用套", zap.Int("total", len(hosts)), zap.Int("unavailable", unavail))
	}
	// —— 接口1（查套列表 → 并发探活）结果明细：同步了几个实例、各自可用性/规模，一眼核对 ——
	instB := strings.Builder{}
	instB.WriteString("========== [operation_topology] 本次同步 · 实例清单（接口1 查套+探活） ==========")
	instB.WriteString(fmt.Sprintf("  实例总数 %d（可用 %d / 不可用 %d）\n", len(hosts), len(hosts)-unavail, unavail))
	sortedHosts := make([]ApolloHost, len(hosts))
	copy(sortedHosts, hosts)
	sort.Slice(sortedHosts, func(i, j int) bool { return sortedHosts[i].Id < sortedHosts[j].Id })
	for _, h := range sortedHosts {
		avail := "可用"
		if len(probe) == 0 {
			avail = "可用(探活未启用)"
		} else if p, ok := probe[h.Id]; ok && !p.Available {
			avail = "不可用"
			if p.Error != "" {
				avail += "(" + p.Error + ")"
			}
		}
		instB.WriteString(fmt.Sprintf("  #%-6d  %-42s  应用规模 %-6d  %s  %s\n",
			h.Id, h.Name, h.AppCount, avail, h.Host))
	}
	instB.WriteString("===================================================================")
	tplog.L().Info(instB.String())
	report("正在拉取应用列表", -1)

	// 2. 并发获取可用套的应用列表并组装扫描任务
	// 对应参考实现 fetch_proxy_apps_first_available：可用套并发抓取，不可用套快跳
	appsByHost, skipped, okHosts := client.GetAppsConcurrent(hosts, probe)
	if skipped > 0 {
		tplog.L().Warn("[operation_topology] 跳过不可用套", zap.Int("skipped", skipped))
	}
	// —— 排除名单（exclude_app_ids）——
	//   跳过占位/无效测试应用（如 easyops-test-api*：其上游 meta server 是 fill-in 占位地址，
	//   每次拉取必 500，重试无意义，还会占用每轮“未完成应用”名额并触发 partial 保护），
	//   在组装任务前过滤，计数/owner/任务/日志一并剔除。
	if len(cfg.ExcludeAppIds) > 0 {
		ex := make(map[string]bool, len(cfg.ExcludeAppIds))
		for _, id := range cfg.ExcludeAppIds {
			id = strings.TrimSpace(id)
			if id != "" {
				ex[id] = true
			}
		}
		if len(ex) > 0 {
			excluded := 0
			for _, h := range hosts {
				apps := appsByHost[h.Id]
				if len(apps) == 0 {
					continue
				}
				kept := apps[:0]
				for _, app := range apps {
					if ex[app.AppId] {
						excluded++
						continue
					}
					kept = append(kept, app)
				}
				appsByHost[h.Id] = kept
			}
			if excluded > 0 {
				tplog.L().Warn("[operation_topology] 排除名单生效",
					zap.Int("excluded", excluded), zap.Strings("appIds", cfg.ExcludeAppIds))
			}
		}
	}
	var tasks []scanTask
	// owner 预计算：按 appId 只映射一次（同一 appId 即使出现在多个套也只用首次结果）
	owners := make(map[string]config.AppidOwner)
	// 实例层归属元数据：按 hostId 只拼一次（host/secondProductId/appCount 随关系行冗余落库）
	metas := make(map[int]instanceMeta, len(hosts))
	// 应用数只统计一次（与 env 无关），避免多 env 循环重复累加
	for _, h := range hosts {
		if apps, ok := appsByHost[h.Id]; ok {
			result.AppCount += len(apps)
			metas[h.Id] = instanceMeta{
				hostId:          h.Id,
				hostName:        h.Name,
				hostAddr:        h.Host,
				secondProductId: strings.Join(h.SecondProductId, ","),
				appCount:        h.AppCount,
			}
			for _, app := range apps {
				if _, seen := owners[app.AppId]; !seen {
					owners[app.AppId] = ownerFor(app)
				}
			}
		}
	}
	// —— ns 学习定向（同步优化 D+A 组合 · 零上游依赖）——
	//   无学习记录 → 固定名单全扫（首扫 bootstrap，建立基线）；有记录且未超期 → 命中集定向扫 targeted；
	//   超过 ns_guess_days 未全量 → 固定名单兜底重猜 recheck（防新增/改名 namespace 的漂移）。
	//   学习键 = hostId|env|appId（套 × 环境 × 应用 三元组），计数与回写均按三元组。
	learnOn := cfg.NsLearnEnable == nil || *cfg.NsLearnEnable
	guessDays := cfg.NsGuessDays
	if guessDays < 0 {
		guessDays = 0
	}
	var nsTargeted, nsBootstrap, nsRecheck int
	// 本批跑过全名单（首扫/兜底）的三元组：批末刷新 lastGuessAt
	guessedKeys := map[string]bool{}
	// 命中/404 登记（worker 侧并发写，批末合并回写学习文件）
	var learnMu sync.Mutex
	hitNs := map[string]map[string]bool{}
	rmNs := map[string]map[string]bool{}
	addNsSet := func(m map[string]map[string]bool, key, ns string) {
		set := m[key]
		if set == nil {
			set = map[string]bool{}
			m[key] = set
		}
		set[ns] = true
	}
	// planNs 决定单个三元组本次扫描的 ns 名单与模式（fixed/bootstrap/targeted/recheck）
	planNs := func(host instanceMeta, env string, app ApolloApp) ([]string, string) {
		if !learnOn {
			return namespaces, "fixed"
		}
		key := nsLearnKey(host.hostId, env, app.AppId)
		ent := nsLearner.get(key)
		switch {
		case ent == nil:
			guessedKeys[key] = true
			nsBootstrap++
			return namespaces, "bootstrap"
		case nsGuessDue(ent.LastGuessAt, guessDays, syncDate):
			guessedKeys[key] = true
			nsRecheck++
			return namespaces, "recheck"
		default:
			nsTargeted++
			return ensureApplication(ent.Namespaces), "targeted"
		}
	}
	for _, env := range envs {
		for _, h := range hosts {
			apps, ok := appsByHost[h.Id]
			if !ok || len(apps) == 0 {
				continue
			}
			for _, app := range apps {
				nsList, _ := planNs(metas[h.Id], env, app)
				if len(nsList) > maxNs {
					nsList = nsList[:maxNs]
				}
				for _, ns := range nsList {
					tasks = append(tasks, scanTask{host: metas[h.Id], env: env, app: app, ns: ns, owner: owners[app.AppId]})
				}
			}
		}
	}
	nsStrategyText := "固定名单猜测（ns 学习未启用）"
	if learnOn {
		nsStrategyText = fmt.Sprintf("学习定向 targeted=%d / 首扫 bootstrap=%d / 兜底重猜 recheck=%d",
			nsTargeted, nsBootstrap, nsRecheck)
	}
	// —— 含全名单猜测（首扫 bootstrap / 兜底重猜 recheck）时自动放大扫描预算 ——
	//   全猜任务量 = 应用 × 全部猜测 ns，远超定向扫描；默认 1m 预算内跑不完会触发
	//   “预算截断 → 整批保护不落盘”，首扫还可能把半成品当基线（线上 462 半成品即因此产生）。
	//   定向阶段（targeted）回归配置小预算，互不影响。
	if learnOn && (nsBootstrap > 0 || nsRecheck > 0) {
		boost := 5 * time.Minute
		if budget < boost {
			tplog.L().Warn("[operation_topology] 含全名单猜测任务，扫描预算自动放大",
				zap.Int("bootstrap", nsBootstrap), zap.Int("recheck", nsRecheck),
				zap.String("from", budget.String()), zap.String("to", boost.String()))
			budget = boost
		}
	}
	result.AvailableHosts = okHosts
	result.NamespaceCount = len(tasks)
	// 扫描任务总数为进度分母（worker 每完成一次 namespace 请求回调一次真实进度）
	progressTotal := len(tasks)
	report(fmt.Sprintf("扫描任务就绪，共 %d 项", progressTotal), 0)
	// 规模统计初值（apps/hosts 组装后即确定；middleware/relations 待 collector 实时累计）
	reportStats(SyncStats{Hosts: len(hosts), Apps: result.AppCount})
	tplog.L().Info("[operation_topology] 扫描任务组装完成",
		zap.Int("hosts", len(hosts)), zap.Int("availableHosts", okHosts), zap.Int("apps", result.AppCount), zap.Int("tasks", len(tasks)),
		zap.String("nsStrategy", nsStrategyText))

	// —— 本次跑批 · 执行计划总览（日志开头一屏，便于快速定位本批边界）——
	planB := strings.Builder{}
	planB.WriteString("========== [operation_topology] 本次同步 · 执行计划总览 ==========")
	appendKV(&planB, "同步日期", syncDate)
	appendKV(&planB, "触发方式", triggerBy)
	appendKV(&planB, "数据源", cfg.BaseUrl)
	appendKV(&planB, "扫描环境", strings.Join(envs, ","))
	appendKV(&planB, "扫描空间", strings.Join(namespaces, ","))
	appendKV(&planB, "并发参数", fmt.Sprintf("workers=%d  budget=%s  (maxNsPerApp=%d)", workers, budget.String(), maxNs))
	appendKV(&planB, "执行规模", fmt.Sprintf("hosts=%d(可用%d/不可用%d)  apps=%d  tasks=%d",
		len(hosts), okHosts, len(hosts)-okHosts, result.AppCount, len(tasks)))
	appendKV(&planB, "ns扫描策略", nsStrategyText)
	appendKV(&planB, "开始时间", startFmt)
	planB.WriteString("=============================================")
	tplog.L().Info(planB.String())

	// —— 接口2（并发拉应用 → 组装 scanTask）结果明细：到应用级，核对每个实例下同步了哪些应用 ——
	appB := strings.Builder{}
	appB.WriteString("========== [operation_topology] 本次同步 · 应用清单（接口2 拉应用+组装任务） ==========")
	appB.WriteString(fmt.Sprintf("  可用实例 %d / 应用总数 %d / 扫描任务 %d（=应用 × namespace %d × env %d）\n",
		okHosts, result.AppCount, len(tasks), len(namespaces), len(envs)))
	sortedHosts = sortedHosts[:0]
	for _, h := range hosts {
		if _, ok := appsByHost[h.Id]; ok {
			sortedHosts = append(sortedHosts, h)
		}
	}
	sort.Slice(sortedHosts, func(i, j int) bool { return sortedHosts[i].Id < sortedHosts[j].Id })
	for _, h := range sortedHosts {
		apps := appsByHost[h.Id]
		sortedApps := make([]ApolloApp, len(apps))
		copy(sortedApps, apps)
		sort.Slice(sortedApps, func(i, j int) bool { return sortedApps[i].AppId < sortedApps[j].AppId })
		appB.WriteString(fmt.Sprintf("  实例 #%-6d  %-42s  应用 %d 个\n", h.Id, h.Name, len(sortedApps)))
		for _, app := range sortedApps {
			appB.WriteString(fmt.Sprintf("    -  %-42s %s\n", app.AppId, app.Name))
		}
	}
	appB.WriteString("===================================================================")
	tplog.L().Info(appB.String())

	// 3. 接口3：并发抓取 + 解析（预算内 partial 容错）
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	taskCh := make(chan scanTask)
	relCh := make(chan []model.OperationTopology, workers)
	var reqCount atomic.Int32
	var unfinMu sync.Mutex
	unfinishedSet := map[string]bool{}
	// 中间件缺失登记：appId → 缺失的「中间件型 namespace」（该 ns 404 不存在 = 未接入对应中间件），批末统一应用级提示
	var missMu sync.Mutex
	missNames := map[string]string{}
	missNs := map[string][]string{}
	// 已实际执行过拉取的应用登记（任意 ns 有返回即算，404 亦算）：
	// 供批末计算“无中间件应用”清单（仅已扫过的应用才有资格判定，预算截断未执行的不算）
	var execMu sync.Mutex
	execApps := map[string]string{}

	var wg sync.WaitGroup
	// B7：单请求耗时聚合（worker 本地累计 ms，结束前一次性汇总，热路径不加锁）
	var costSumMs, costMaxMs int64
	var costMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var sumMs, maxMs int64
			// 无论 ctx 提前退出还是正常消费完都汇总（防止 return 跳过统计）
			defer func() {
				if maxMs > 0 {
					costMu.Lock()
					costSumMs += sumMs
					if maxMs > costMaxMs {
						costMaxMs = maxMs
					}
					costMu.Unlock()
				}
			}()
			for t := range taskCh {
				if ctx.Err() != nil {
					return
				}
				start := time.Now()
				items, err := client.GetNamespace(t.host.hostId, t.env, t.app.AppId, "default", t.ns)
				cost := time.Since(start).Milliseconds()
				reqCount.Add(1)
				// 执行登记：该应用本次至少完成了一次 namespace 拉取（结果成败另计），
				// 用于批末“无中间件应用”判定（预算截断未执行的应用不会登记，避免误报“无中间件”）
				execMu.Lock()
				if _, ok := execApps[t.app.AppId]; !ok {
					execApps[t.app.AppId] = t.app.Name
				}
				execMu.Unlock()
				// 进度上报：已完成请求数/任务总数（partial 场景提前终止时 done<total，条按实际比例走）
				if n := int(reqCount.Load()); progressTotal > 0 {
					report(fmt.Sprintf("正在扫描配置 %d/%d", n, progressTotal), n*100/progressTotal)
				}
				sumMs += cost
				if cost > maxMs {
					maxMs = cost
				}
				if err != nil {
					// 404 = namespace 不存在（预期态）：中间件型 ns 登记“缺失提示”，不视为扫描失败、不计未完成应用
					if errors.Is(err, errHTTP404) {
						if middlewareNsHints[strings.ToLower(t.ns)] {
							missMu.Lock()
							if _, ok := missNs[t.app.AppId]; !ok {
								missNames[t.app.AppId] = t.app.Name
							}
							missNs[t.app.AppId] = append(missNs[t.app.AppId], t.ns)
							missMu.Unlock()
						}
						// ns 学习：404 = 该 ns 已不存在 → 批末从学习集移除（自愈“上次命中现已删除”的漂移）
						if learnOn {
							learnMu.Lock()
							addNsSet(rmNs, nsLearnKey(t.host.hostId, t.env, t.app.AppId), t.ns)
							learnMu.Unlock()
						}
						continue
					}
					tplog.L().Warn("[operation_topology] 扫描失败", zap.String("appId", t.app.AppId), zap.String("ns", t.ns), zap.String("env", t.env), zap.Error(err))
					unfinMu.Lock()
					unfinishedSet[t.app.AppId] = true
					unfinMu.Unlock()
					continue
				}
				// ns 学习：查询成功（200，含空空间）= 该 ns 真实存在 → 批末并入学习命中集
				if learnOn {
					learnMu.Lock()
					addNsSet(hitNs, nsLearnKey(t.host.hostId, t.env, t.app.AppId), t.ns)
					learnMu.Unlock()
				}
				rels := parseItemsToRelations(t.app, t.host, t.env, t.ns, syncDate, items, t.owner)
				if len(rels) > 0 {
					relCh <- rels
				}
				// 解析摘要：体现 输入(items) → 输出(关系) 的出入参，便于过程回溯。
				// 输入为空(如 namespace 不存在/空空间)时跳过，避免 404 空结果刷 INFO（真实环境占比最高）
				if len(items) > 0 || len(rels) > 0 {
					tplog.L().Info("[operation_topology] 解析摘要",
						zap.String("appId", t.app.AppId), zap.String("name", t.app.Name),
						zap.String("ns", t.ns), zap.String("env", t.env),
						zap.String("host", t.host.hostName),
						zap.Int("items", len(items)), zap.Int("rels", len(rels)),
						zap.Int64("costMs", cost))
					if len(rels) > 0 && len(rels) <= 5 {
						for _, r := range rels {
							tplog.L().Debug("[operation_topology] 关系明细",
								zap.String("appId", r.ApolloAppId), zap.String("key", r.ConfigKey),
								zap.String("type", r.MiddlewareType), zap.String("addr", r.ConnectAddr))
						}
					}
				}
			}
		}()
	}
	go func() {
		// defer close 保证无论 ctx 是否取消都会关闭任务通道，
		// 否则 worker 阻塞在 range taskCh 永不退出 → wg.Wait 永不完成 → collector 死锁
		defer close(taskCh)
		for _, t := range tasks {
			select {
			case <-ctx.Done():
				return
			case taskCh <- t:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(relCh)
	}()

	// collector：聚合 + 去重（去重键与快照对比共用 relationKey，避免两处格式漂移）
	// 同时维护实时规模统计（中间件按 类型+地址 去重），每收一批回调一次 → /sync/status 轮询可见
	var all []model.OperationTopology
	dedup := map[string]bool{}
	mwSeen := map[string]bool{}
	for rels := range relCh {
		for _, r := range rels {
			k := relationKey(r)
			if !dedup[k] {
				dedup[k] = true
				all = append(all, r)
			}
			mwSeen[r.MiddlewareType+"|"+r.ConnectAddr] = true
		}
		reportStats(SyncStats{Hosts: len(hosts), Apps: result.AppCount, Middleware: len(mwSeen), Relations: len(all)})
	}
	result.RequestCount = int(reqCount.Load())
	// 请求耗时聚合结果先存局部变量，末尾“结果总览”块复用展示
	var avgMs, maxMs int64
	if n := int64(reqCount.Load()); n > 0 {
		// B7：单请求耗时汇总（请求全部结束后读取，costMu 已无并发）
		avgMs = costSumMs / n
		maxMs = costMaxMs
		tplog.L().Info("[operation_topology] 扫描耗时统计",
			zap.Int64("requests", n),
			zap.Int64("avgMs", avgMs),
			zap.Int64("maxMs", maxMs))
	}
	// 解析产物按中间件类型维度汇总（去重后），直观展示本次扫描解析出哪些中间件/多少关系
	typeStats := map[string]int{}
	for _, r := range all {
		typeStats[r.MiddlewareType]++
	}
	tplog.L().Info("[operation_topology] 解析产物按类型统计",
		zap.Any("typeStats", typeStats), zap.Int("total", len(typeStats)))

	// —— 中间件缺失提示：每个缺失了「中间件型 namespace」的应用一条 Warn（ns 404 不存在 = 该应用未接入对应中间件，
	//    或使用了自定义命名空间），供排查“没扫到中间件是正常还是漏扫”。非中间件型空间缺失不参与提示。——
	if len(missNs) > 0 {
		mk := make([]string, 0, len(missNs))
		for k := range missNs {
			mk = append(mk, k)
		}
		sort.Strings(mk)
		for _, k := range mk {
			sort.Strings(missNs[k])
			tplog.L().Warn("[operation_topology] 本次同步 · 中间件缺失（namespace 不存在=未接入对应中间件）",
				zap.String("appId", k),
				zap.String("name", missNames[k]),
				zap.String("missingNs", strings.Join(missNs[k], ", ")))
		}
	}

	// —— ns 学习回写（D+A 组合）：合并本批命中集/移除 404 + 刷新首扫/兜底日期；
	//    学习状态独立于关系快照，失败仅告警不影响本次同步结果 ——
	if learnOn {
		nsLearner.merge(hitNs, rmNs, guessedKeys, syncDate)
		if err := nsLearner.flush(); err != nil {
			tplog.L().Warn("[operation_topology] ns 学习状态回写失败", zap.Error(err))
		}
	}

	// —— 入库（直写文件）中间件明细：逐个「类型+地址」列出（含去重依赖应用数），核对落盘了什么 ——
	mwApps := map[string]map[string]bool{}
	for _, r := range all {
		k := r.MiddlewareType + "|" + r.ConnectAddr
		if mwApps[k] == nil {
			mwApps[k] = map[string]bool{}
		}
		mwApps[k][r.ApolloAppId] = true
	}
	// 类型 × 去重节点数（节点 = 类型+地址 去重，341 口径；关系数含实例重复，两列口径不同）
	typeNodes := map[string]int{}
	for k := range mwApps {
		typeNodes[k[:strings.IndexByte(k, '|')]]++
	}
	mwB := strings.Builder{}
	mwB.WriteString("========== [operation_topology] 本次同步 · 入库中间件清单（类型+地址 去重） ==========")
	mwB.WriteString(fmt.Sprintf("  去重中间件节点共 %d 个（本次将随 %d 条关系一并落盘）\n", len(mwApps), len(all)))
	mwKeys := make([]string, 0, len(mwApps))
	for k := range mwApps {
		mwKeys = append(mwKeys, k)
	}
	sort.Slice(mwKeys, func(i, j int) bool {
		ti := mwKeys[i][:strings.IndexByte(mwKeys[i], '|')]
		tj := mwKeys[j][:strings.IndexByte(mwKeys[j], '|')]
		if ti != tj {
			return ti < tj
		}
		ai := mwKeys[i][strings.IndexByte(mwKeys[i], '|')+1:]
		aj := mwKeys[j][strings.IndexByte(mwKeys[j], '|')+1:]
		return ai < aj
	})
	for _, k := range mwKeys {
		idx := strings.IndexByte(k, '|')
		typ, addr := k[:idx], k[idx+1:]
		mwB.WriteString(fmt.Sprintf("  %-14s %-40s  依赖应用 %d\n", typ, addr, len(mwApps[k])))
	}
	mwB.WriteString("===================================================================")
	tplog.L().Info(mwB.String())
	unfinMu.Lock()
	for id := range unfinishedSet {
		result.UnfinishedApps = append(result.UnfinishedApps, id)
	}
	unfinMu.Unlock()
	sort.Strings(result.UnfinishedApps)

	// —— 无中间件应用清单（状态接口 + 日志双重体现）——
	//   口径：本次已实际执行过拉取（execApps）但未解析出任何中间件关系行（rowApps）的应用；
	//   排除“未完成应用”（拉取失败，原因另报）与“未执行应用”（预算截断未轮到，无法判定），
	//   避免把“没扫到”误报成“没有”。产出随 /sync/status 的 result 返回，并按应用逐行落日志。
	rowApps := map[string]bool{}
	for _, r := range all {
		rowApps[r.ApolloAppId] = true
	}
	unfinMap := make(map[string]bool, len(result.UnfinishedApps))
	for _, id := range result.UnfinishedApps {
		unfinMap[id] = true
	}
	noMw := make([]model.NoMwApp, 0, len(execApps))
	for id, name := range execApps {
		if !rowApps[id] && !unfinMap[id] {
			noMw = append(noMw, model.NoMwApp{AppId: id, AppName: name})
		}
	}
	sort.Slice(noMw, func(i, j int) bool { return noMw[i].AppId < noMw[j].AppId })
	result.NoMiddlewareApps = noMw
	result.NoMiddlewareCount = len(noMw)
	if len(noMw) > 0 {
		nb := strings.Builder{}
		nb.WriteString("========== [operation_topology] 本次同步 · 无中间件应用清单（已扫描未发现中间件关系） ==========")
		nb.WriteString(fmt.Sprintf("  共 %d 个（不含未完成/未执行应用）\n", len(noMw)))
		for _, a := range noMw {
			nb.WriteString(fmt.Sprintf("  %-40s %s\n", a.AppId, a.AppName))
		}
		nb.WriteString("===================================================================")
		tplog.L().Info(nb.String())
	}

	if ctx.Err() != nil || len(result.UnfinishedApps) > 0 {
		result.Partial = true
	}
	result.InsertCount = len(all)

	// 4. 与上次快照对比（§5 日志要求：新增/消失关系）
	//    上一份快照从数据源路由读取（当前 file 模式 = 本地文件，只保留最近一次；
	//    prevDate 为空表示首次直写，无历史可对比）
	r := currentRouter()
	prevDate, prev := r.PreviousSnapshot(syncDate)
	result.NewRelations, result.LostRelations = diffRelations(prev, all)
	tplog.L().Info("[operation_topology] 解析完成",
		zap.Int("relations", len(all)), zap.Int("requests", result.RequestCount),
		zap.Bool("partial", result.Partial), zap.Int("new", len(result.NewRelations)), zap.Int("lost", len(result.LostRelations)))
	report("正在对比并生成快照", 100)

	// 5. 快照落盘（§3 快照策略 · 当前阶段唯一生效落点 = 本地文件）
	//    直写 data/operation_topology.json：原子替换当日快照，并按 sync_date 保留最近 N 份
	//    （topology_snapshot_keep，默认 3）供前后对比，更早的桶清理（不无限堆积）；
	//    数据库为后续能力，代码入口保留在 ReplaceSnapshot（已注入 DB 连接才镜像落库，同口径保留 N 份）。
	//    “新数据就绪前不影响旧数据”：采集期间文件/内存始终是旧快照（查询稳定），
	//    仅在整批解析完成时一次性原子替换（tmp+rename）；部分完成或空产物绝不顶替好数据。
	//    keptRows/keptDate：本次未落盘时，被保留的现有快照规模（供末尾总览明示）。
	//    carriedRows：方式 B 继承行数——执行完整但个别应用失败时，把失败应用的上版快照行并入本次落盘，
	//    避免“个别失败→整批不落盘→快照长期停在旧版”的数据断层（曾因 4 个应用持续失败导致 1418 行连续无法落盘）。
	var keptRows int64
	var keptDate string
	var wroteSnapshot bool
	var carriedRows int
	writeRows := all
	if len(all) > 0 {
		// 分档落盘决策：
		//   ① 预算截断（ctx 超时/存在未执行任务）→ 解析不完整不可信，本地已有旧快照则保护不覆盖；
		//      从未有过快照时仍强制落盘（首次兜底，避免一直空库）。
		//   ② 任务全部执行（可含少量失败应用）→ 整批落盘；失败应用旧行并入本次（方式 B 继承）。
		//   ③ 全部解析为空 → 保持旧快照（本 if 外 else 分支）。
		budgetCut := ctx.Err() != nil || result.RequestCount < result.NamespaceCount
		protect := false
		if budgetCut {
			if prevDate != "" && len(prev) > 0 {
				protect = true
				keptDate, keptRows = prevDate, int64(len(prev))
			} else if n := int64(r.SnapshotCount(syncDate)); n > 0 {
				protect = true
				keptDate, keptRows = syncDate, n
			}
			// 无旧快照 → protect 保持 false：部分数据也先落盘（首次兜底）
		} else if len(result.UnfinishedApps) > 0 {
			writeRows = mergeUnfinishedRows(all, prev, result.UnfinishedApps, syncDate)
			carriedRows = len(writeRows) - len(all)
			if carriedRows > 0 {
				tplog.L().Warn("[operation_topology] 失败应用继承旧快照行",
					zap.Int("unfinished", len(result.UnfinishedApps)), zap.Int("carried", carriedRows))
			}
		}
		if !protect {
			if err := r.ReplaceSnapshot(writeRows); err != nil {
				tplog.L().Error("[operation_topology] 直写本地快照失败", zap.Error(err))
				return result, err
			}
			wroteSnapshot = true
			tplog.L().Info("[operation_topology] 快照直写结束",
				zap.Int("writeRows", len(writeRows)), zap.Int("carried", carriedRows))
		} else {
			tplog.L().Warn("[operation_topology] 本次同步部分完成，保留现有快照不覆盖",
				zap.String("keptDate", keptDate), zap.Int64("kept", keptRows), zap.Int("parsed", len(all)))
		}
	} else {
		// 本次同步无解析结果：不覆盖文件（保留当日已有快照，快照语义不中断）。
		// 但此时 InsertCount=0 与文件当日实际行数不一致，需日志明示，避免前端误读
		keptDate = syncDate
		keptRows = int64(r.SnapshotCount(syncDate))
		if keptRows > 0 {
			tplog.L().Warn("[operation_topology] 本次同步无解析结果，保留文件当日已有快照",
				zap.String("syncDate", syncDate), zap.Int64("kept", keptRows))
		}
	}
	// —— 组装“本次同步整体效果”（随 /sync 返回，供前端一次展示；与日志总览一致）——
	result.TriggerBy = triggerBy
	result.StartTime = startFmt
	result.EndTime = time.Now().Format("2006-01-02 15:04:05.000")
	result.CostMs = time.Since(syncStart).Milliseconds()
	result.AvgRequestMs = avgMs
	result.MaxRequestMs = maxMs
	result.TotalRelations = len(all)
	result.TypeStats = typeStats
	result.NodeStats = typeNodes
	result.NsTargeted = nsTargeted
	result.NsBootstrap = nsBootstrap
	result.NsRecheck = nsRecheck
	result.NewCount = len(result.NewRelations)
	result.LostCount = len(result.LostRelations)
	result.PrevDate = prevDate
	result.KeptRows = keptRows
	result.ResultText = "成功"
	if result.Partial {
		result.ResultText = fmt.Sprintf("部分完成（未执行任务 %d / 未完成应用 %d）",
			result.NamespaceCount-result.RequestCount, len(result.UnfinishedApps))
		if wroteSnapshot && carriedRows > 0 {
			result.ResultText += fmt.Sprintf("；已落盘（%d 个未完成应用继承旧快照 %d 行）",
				len(result.UnfinishedApps), carriedRows)
		} else if !wroteSnapshot && keptRows > 0 {
			result.ResultText += fmt.Sprintf("；已保留现有快照 %s %d 条，本次未覆盖", keptDate, keptRows)
		}
	}
	// 关系同步异常（上游拉取失败 / 关系表缺失或写库失败）不阻断主表同步，但必须在结果中显式标注
	if result.RelationErr {
		result.ResultText += "（关系同步异常）"
	}
	tplog.L().Info("[operation_topology] 同步完成",
		zap.Int("insert", result.InsertCount),
		zap.Int("hosts", result.HostCount), zap.Int("apps", result.AppCount), zap.Int("ns", result.NamespaceCount),
		zap.Int("requests", result.RequestCount), zap.Bool("partial", result.Partial),
		zap.Int("new", len(result.NewRelations)), zap.Int("lost", len(result.LostRelations)),
		zap.String("syncDate", syncDate), zap.Int64("totalMs", time.Since(syncStart).Milliseconds()))

	// —— 本次跑批 · 结果总览（日志末尾一屏：规模/结果/耗时/分布，方便日后只查日志排障）——
	sumB := strings.Builder{}
	sumB.WriteString("========== [operation_topology] 本次同步 · 结果总览 ==========")
	appendKV(&sumB, "同步日期", syncDate)
	appendKV(&sumB, "触发方式", triggerBy)
	appendKV(&sumB, "开始时间", startFmt)
	appendKV(&sumB, "执行耗时", fmt.Sprintf("%d ms", time.Since(syncStart).Milliseconds()))
	status := "成功"
	if result.Partial {
		status = fmt.Sprintf("部分完成(未执行任务%d / 未完成应用%d)",
			result.NamespaceCount-result.RequestCount, len(result.UnfinishedApps))
	}
	if result.RelationErr {
		status += "（关系同步异常）"
	}
	appendKV(&sumB, "执行结果", status)
	appendKV(&sumB, "扫描规模", fmt.Sprintf("hosts=%d(可用%d/不可用%d)  apps=%d  tasks=%d",
		len(hosts), okHosts, len(hosts)-okHosts, result.AppCount, result.NamespaceCount))
	appendKV(&sumB, "配置请求", fmt.Sprintf("实际 %d/计划 %d  未执行 %d   avg=%dms  max=%dms",
		result.RequestCount, result.NamespaceCount, result.NamespaceCount-result.RequestCount, avgMs, maxMs))
	appendKV(&sumB, "ns扫描策略", nsStrategyText)
	if wroteSnapshot {
		note := fmt.Sprintf("%d 条解析", len(all))
		if carriedRows > 0 {
			note = fmt.Sprintf("%d 条解析 + 失败应用继承 %d 行", len(all), carriedRows)
		}
		appendKV(&sumB, "快照落盘", fmt.Sprintf("%s → 已原子替换本地快照（同步期间旧版数据保持可查）", note))
	} else if keptRows > 0 {
		note := "0 条"
		if len(all) > 0 {
			note = fmt.Sprintf("%d 条解析", len(all))
		}
		appendKV(&sumB, "快照落盘", fmt.Sprintf("%s（部分完成/无产物 → 保留现有快照 %s %d 条）", note, keptDate, keptRows))
	} else {
		appendKV(&sumB, "快照落盘", "0 条（无解析产物）")
	}
	appendKV(&sumB, "关系同步", relationSyncText(result.RelationRows, result.RelationWrote, result.RelationErr, result.RelationMsg))
	if prevDate == "" {
		appendKV(&sumB, "快照对比", "无历史（首次直写）")
	} else {
		appendKV(&sumB, "快照对比", fmt.Sprintf("vs %s   新增 %d / 消失 %d", prevDate,
			len(result.NewRelations), len(result.LostRelations)))
		appendKV(&sumB, "新增示例", samplesBlock(result.NewRelations, 3))
		appendKV(&sumB, "消失示例", samplesBlock(result.LostRelations, 3))
	}
	if len(result.UnfinishedApps) > 0 {
		appendKV(&sumB, "未完成应用", samplesBlock(result.UnfinishedApps, 5))
	}
	if result.NoMiddlewareCount > 0 {
		ids := make([]string, len(result.NoMiddlewareApps))
		for i, a := range result.NoMiddlewareApps {
			ids[i] = a.AppId
			if a.AppName != "" {
				ids[i] += "（" + a.AppName + "）"
			}
		}
		appendKV(&sumB, "无中间件应用", fmt.Sprintf("%d 个（已扫描未发现中间件关系）：%s",
			len(ids), samplesBlock(ids, 5)))
	}
	sumB.WriteString(fmt.Sprintf("  —— 中间件类型分布（关系 %d 行含实例重复 · 去重节点 %d 个，节点按类型+地址）——\n", len(all), len(mwApps)))
	sumB.WriteString(typeDistBlock(typeStats, typeNodes, len(all)))
	sumB.WriteString("=============================================")
	tplog.L().Info(sumB.String())
	return result, nil
}

// relationKey 关系唯一键（用于收集去重 + 快照对比，与表唯一索引 uk_snapshot_relation 一致）
// 实例（套）为顶层维度：同一应用即使跨多套实例出现相同中间件关系，也按来源实例分别保留，
// 保证页面「Apollo 实例 → 应用」归属边完整（uk_snapshot_relation = sync_date+appid+host_id+env+type+addr）
func relationKey(r model.OperationTopology) string {
	hostPart := r.ApolloHostName
	if hostPart == "" {
		hostPart = fmt.Sprintf("#%d", r.ApolloHostId)
	}
	return r.ApolloAppId + "|" + hostPart + "|" + r.Env + "|" + r.MiddlewareType + "|" + r.ConnectAddr
}

// diffRelations 两次快照关系差集
func diffRelations(prev, cur []model.OperationTopology) (newR, lostR []string) {
	// 预分配：map 容量按快照行数提示，避免扩容
	pk := make(map[string]bool, len(prev))
	for _, r := range prev {
		pk[relationKey(r)] = true
	}
	ck := make(map[string]bool, len(cur))
	for _, r := range cur {
		ck[relationKey(r)] = true
	}
	for k := range ck {
		if !pk[k] {
			newR = append(newR, k)
		}
	}
	for k := range pk {
		if !ck[k] {
			lostR = append(lostR, k)
		}
	}
	sort.Strings(newR)
	sort.Strings(lostR)
	return
}

// mergeUnfinishedRows 落盘方式 B：本批任务全部执行但个别应用失败时，
// 将其上版快照行并入本次待落盘集合（同步日期统一改写为当前批），防止
// “个别应用失败 → 整批不落盘 → 快照长期停在旧版”的数据断层。
// 仅在行键（relationKey）不在本次产物中时才继承，避免与本次成功行重复。
func mergeUnfinishedRows(all, prev []model.OperationTopology, unfinished []string, syncDate string) []model.OperationTopology {
	if len(prev) == 0 || len(unfinished) == 0 {
		return all
	}
	unfin := make(map[string]bool, len(unfinished))
	for _, id := range unfinished {
		unfin[id] = true
	}
	seen := make(map[string]bool, len(all)+len(prev))
	for _, r := range all {
		seen[relationKey(r)] = true
	}
	out := all
	for _, r := range prev {
		if unfin[r.ApolloAppId] && !seen[relationKey(r)] {
			seen[relationKey(r)] = true
			r.SyncDate = syncDate
			out = append(out, r)
		}
	}
	return out
}

// LatestSyncDate 最新快照日期（按配置默认源；请求需指定数据源时用 LatestSyncDateOf）
func LatestSyncDate(env string) string {
	r := currentRouter()
	if r == nil {
		return ""
	}
	v, _ := r.LatestSyncDateOf(env, "")
	return v
}

// expandDepNodeOn 部署点展开判定：未传（nil）或显式 true → 展开（默认）；显式 false → 折叠。
// 需求方口径「部署项目 + 节点 是应用节点的唯一条件」，故默认展开；需折叠时才显式传 false。
func expandDepNodeOn(v *bool) bool { return v == nil || *v }

// appKeyOf 应用节点的合并键 / 图节点 id 后缀。
//
//	expandDepNode=false（折叠模式）：以 apollo_app_id 为主（拓扑主维度），缺失时回退部署项目名；
//	  node/eng_project/dep_project/pp_application 均为该 appId 的属性，不参与合并。
//	expandDepNode=true（部署点展开模式）：appId|部署项目|节点 —— 「部署项目 + 节点」是应用节点的
//	  唯一条件，一个部署点一个节点。
//
// 展开模式仍保留 appId 前缀：实测有 6 个部署项目被两个 appId 共用（如 bms-member-service-dep 同属
// bms-market-service 与 bms-member-service），纯「部署项目|节点」会把两个不同应用错并成一个节点。
// 图（app:{appKey}）与详情（/detail?id=app:{appKey}）共用同一口径，保证两处 id 一致。
func appKeyOf(r model.OperationTopology, expandDepNode bool) string {
	if r.ApolloAppId != "" {
		if !expandDepNode || (r.DepProject == "" && r.Node == "") {
			return r.ApolloAppId
		}
		return r.ApolloAppId + "|" + r.DepProject + "|" + r.Node
	}
	return r.DepProject
}

// GetOperationTopologyList 关系列表查询（§6.1 /list）
// 返回 source（db|file）本次活跃数据源（供日志/本地验证展示；原 X-Data-Source 响应头已下线）
func GetOperationTopologyList(req model.OperationTopologyListReq) (list []model.OperationTopology, total int64, source string, err error) {
	r := currentRouter()
	if r == nil {
		return nil, 0, sourceDB, nil // 数据源均未初始化（无 DB 且无文件），返回空列表而非报错
	}
	if req.SyncDate == "" {
		req.SyncDate, _ = r.LatestSyncDateOf(req.Env, req.Source)
	}
	f := RowFilter{
		SyncDate: req.SyncDate, Env: req.Env, MiddlewareType: req.MiddlewareType,
		ApolloAppId: req.ApolloAppId, ApolloAppName: req.ApolloAppName,
		ApolloHostName: req.ApolloHostName, AppDept: req.AppDept, AppOwner: req.AppOwner,
		ConnectAddr: req.ConnectAddr, Source: req.Source,
	}
	// appId 属性条件（部署项目/工程项目/节点/PP 应用）：拓扑表这几列为采集期留空，真实来源是
	// 跳① 关系（appId ↔ 部署项目）×跳② DPR，先翻译成 apollo_app_id 集合下推；关系链不可用时
	// 回退原始列过滤（保持旧行为）。属性条件只能在关联后复核（DB/文件无法按内存关联属性过滤），
	// 故仍取全量候选 → 属性补充 → 复核 → 内存分页；
	// 属性补充不改变行数（行粒度恒为 appId × 中间件），total = 复核后的关系行数
	attr := appAttrQuery{
		DepProject: splitAttrCSV(req.DepProject), EngProject: splitAttrCSV(req.EngProject),
		Node: splitAttrCSV(req.Node), PpApplication: splitAttrCSV(req.PpApplication),
	}
	filterApplied := false
	if apps, ok := relationFilterApps(attr); ok {
		f.ApolloAppIdIn, filterApplied = apps, true
	} else {
		// 关系链不可用时回退原始列过滤（仅下推首值，最佳努力）
		f.DepProject, f.Node = firstOf(attr.DepProject), firstOf(attr.Node)
	}
	f.Page, f.PageSize = 1, -1
	q, s, err := r.ListRows(f)
	if err != nil {
		return nil, 0, s, err
	}
	// 关联 跳①×跳②：折叠模式（expandDepNode 显式 false）补充为 appId 属性（多值聚合为列表，不扩行）；
	// 展开模式（默认展开）按部署点扩行（每部署点一行）
	expand := expandDepNodeOn(req.ExpandDepNode)
	var rows []model.OperationTopology
	if expand {
		rows = expandByDeploySlot(q.Rows)
	} else {
		rows = attachAppAttrs(q.Rows)
	}
	if filterApplied {
		if expand {
			rows = filterSlotRows(rows, attr) // 部署点粒度复核
		} else {
			rows = filterAttachRows(rows, attr) // appId 粒度复核
		}
	}
	return pageRows(rows, req.Page, req.PageSize), int64(len(rows)), s, nil
}

// GetMiddlewareTypes 中间件类型枚举（§6.1 /middlewareTypes；source 请求级数据源覆盖）
func GetMiddlewareTypes(source string) ([]string, string) {
	r := currentRouter()
	if r == nil {
		return nil, sourceDB
	}
	return r.TypesOf(source)
}

// EnsureOperationTopologyIndex 确保每日快照唯一索引 uk_snapshot_relation 的列集合为
// (sync_date, apollo_app_id, apollo_host_id, env, middleware_type, connect_addr)，
// 让实例(套)成为快照行唯一身份的一环（同一应用跨多套实例的相同关系可分别保留）。
//
// gorm AutoMigrate 只会按模型创建缺失索引，不会改造同名的旧版索引；
// 该函数把旧版（不含 apollo_host_id）的唯一索引重建为含实例归属的新索引。
// 需在 AutoMigrate 之后、服务开始同步/查询之前调用（主项目与本地测试服务均调用）。
func EnsureOperationTopologyIndex(db *gorm.DB) error {
	const table = "operation_topology"
	const index = "uk_snapshot_relation"
	expect := map[string]bool{
		"sync_date": true, "apollo_app_id": true, "apollo_host_id": true,
		"env": true, "middleware_type": true, "connect_addr": true,
	}
	var cols []string
	if err := db.Raw("SELECT column_name FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ? ORDER BY seq_in_index", table, index).Scan(&cols).Error; err != nil {
		return err
	}
	matched := func(cols []string) bool {
		if len(cols) != len(expect) {
			return false
		}
		got := make(map[string]bool, len(cols))
		for _, c := range cols {
			got[c] = true
		}
		for c := range expect {
			if !got[c] {
				return false
			}
		}
		return true
	}
	if len(cols) > 0 && matched(cols) {
		return nil // 已含实例归属列，无需处理
	}
	if len(cols) > 0 {
		tplog.L().Warn("[operation_topology] 重建快照唯一索引（旧版不含 apollo_host_id）", zap.Strings("oldColumns", cols))
		if err := db.Exec("ALTER TABLE `" + table + "` DROP INDEX `" + index + "`").Error; err != nil {
			return err
		}
	}
	sql := "ALTER TABLE `" + table + "` ADD UNIQUE INDEX `" + index +
		"` (sync_date, apollo_app_id, apollo_host_id, env, middleware_type, connect_addr)"
	if err := db.Exec(sql).Error; err != nil {
		return err
	}
	tplog.L().Info("[operation_topology] 快照唯一索引就绪（含 apollo_host_id，实例为顶层维度）")
	return nil
}

// GetGraph 拓扑图聚合（§6.2 /graph）
//
//	应用节点按 dep_project 合并；中间件节点按 类型+地址 合并（防拓扑爆炸）
//	source（db|file）写入响应体 dataSource 字段（原 X-Data-Source 响应头已下线）
func GetGraph(q model.GraphQuery) (resp model.GraphResp, source string) {
	// Nodes/Edges/ApolloHosts 初始化为空切片（非 nil）：无数据时返回 [] 而非 null，便于前端统一判空
	resp = model.GraphResp{
		SyncDate:    q.SyncDate,
		Env:         q.Env,
		Nodes:       []model.GraphNode{},
		Edges:       []model.GraphEdge{},
		ApolloHosts: []model.GraphHost{},
	}
	// 所有返回路径统一把活跃数据源写入返回体 dataSource
	defer func() { resp.DataSource = source }()
	r := currentRouter()
	if r == nil {
		return resp, sourceDB
	}
	if q.SyncDate == "" {
		q.SyncDate, source = r.LatestSyncDateOf(q.Env, q.Source)
	}
	if q.SyncDate == "" {
		return resp, source // 无任何快照（DB 与文件均无数据）
	}
	resp.SyncDate = q.SyncDate
	f := RowFilter{SyncDate: q.SyncDate, Env: q.Env, MiddlewareType: q.MiddlewareType, Source: q.Source}
	// appId 属性过滤与 list 同口径：经 跳①×跳② 翻译为 apollo_app_id 集合下推，属性补充后按 appId 复核
	// （部署项目模糊 / 工程项目·节点·PP 应用精确）
	attr := appAttrQuery{
		DepProject: splitAttrCSV(q.DepProject), EngProject: splitAttrCSV(q.EngProject),
		Node: splitAttrCSV(q.Node), PpApplication: splitAttrCSV(q.PpApplication),
	}
	filterApplied := false
	if apps, ok := relationFilterApps(attr); ok {
		f.ApolloAppIdIn, filterApplied = apps, true
	} else {
		// 关系链不可用时回退原始列过滤（仅下推首值，最佳努力）
		f.DepProject, f.Node = firstOf(attr.DepProject), firstOf(attr.Node)
	}
	rows, source, err := r.QueryAll(f)
	if err != nil {
		return resp, source
	}
	// 关联 跳①×跳②：折叠模式（expandDepNode 显式 false）补充为 appId 属性（多值聚合为列表，不扩行）；
	// 展开模式（默认展开）按部署点扩行（一个 (appId, 部署项目, 节点) 一行 → 一个应用节点）
	expand := expandDepNodeOn(q.ExpandDepNode)
	if expand {
		rows = expandByDeploySlot(rows)
	} else {
		rows = attachAppAttrs(rows)
	}
	if filterApplied {
		if expand {
			rows = filterSlotRows(rows, attr) // 部署点粒度复核
		} else {
			rows = filterAttachRows(rows, attr) // appId 粒度复核
		}
	}

	appIdx := map[string]int{} // app:{apollo_app_id}（缺失回退 dep_project）→ nodes 下标
	mwIdx := map[string]int{}  // mw:{type}:{addr} → nodes 下标
	edgeSet := map[string]bool{}

	for _, r := range rows {
		// 应用节点：合并键见 appKeyOf —— 折叠模式 = appId（属性多值聚合为列表，属性不参与节点合并）；
		// 展开模式 = appId|部署项目|节点（一个部署点一个节点，多值列表字段已清空）。
		// 节点标题优先应用名称（ApolloAppName），缺失时回退合并键
		appKey := appKeyOf(r, expand)
		appTitle := r.ApolloAppName
		if appTitle == "" {
			appTitle = appKey
		}
		appId := "app:" + appKey
		if idx, ok := appIdx[appId]; ok {
			// 同一 appId 的多行（跨环境/多实例）：属性取并集（属性属 appId 级，不因多值分裂节点）
			nd := &resp.Nodes[idx]
			nd.DepProjects = unionSorted(nd.DepProjects, r.DepProjects)
			nd.EngProjects = unionSorted(nd.EngProjects, r.EngProjects)
			nd.PpApps = unionSorted(nd.PpApps, r.PpApplications)
			nd.Nodes = unionSorted(nd.Nodes, r.Nodes)
			setFirst(&nd.DepProject, nd.DepProjects)
			nd.DeployProjectName = nd.DepProject
			setFirst(&nd.EngProject, nd.EngProjects)
			setFirst(&nd.PpApp, nd.PpApps)
			setFirst(&nd.Node, nd.Nodes)
		} else {
			appIdx[appId] = len(resp.Nodes)
			resp.Nodes = append(resp.Nodes, model.GraphNode{
				Id:                appId,
				Category:          "application",
				Name:              appTitle,
				Label:             r.ApolloAppId,
				Node:              r.Node,
				EngProject:        r.EngProject,
				DepProject:        r.DepProject,
				DeployProjectName: r.DepProject,
				PpApp:             r.PpApplication,
				Nodes:             r.Nodes,
				EngProjects:       r.EngProjects,
				DepProjects:       r.DepProjects,
				PpApps:            r.PpApplications,
				ApolloAppId:       r.ApolloAppId,
				ApolloAppName:     r.ApolloAppName,
				AppDept:           r.AppDept,
				AppOwner:          r.AppOwner,
				ApolloHosts:       []model.GraphHost{},
			})
		}
		// 应用节点归属聚合：同一应用可能来自多套 Apollo 实例，去重保留（携带 host 访问地址与应用总数）
		if r.ApolloHostName != "" {
			ai := appIdx[appId]
			dup := false
			for _, h := range resp.Nodes[ai].ApolloHosts {
				if h.Id == r.ApolloHostId {
					dup = true
					break
				}
			}
			if !dup {
				resp.Nodes[ai].ApolloHosts = append(resp.Nodes[ai].ApolloHosts, model.GraphHost{
					Id:       r.ApolloHostId,
					Name:     r.ApolloHostName,
					Host:     r.ApolloHostAddr,
					AppCount: r.HostAppCount,
				})
			}
		}
		// 中间件节点
		mwId := "mw:" + r.MiddlewareType + ":" + r.ConnectAddr
		if _, ok := mwIdx[mwId]; !ok {
			mwIdx[mwId] = len(resp.Nodes)
			resp.Nodes = append(resp.Nodes, model.GraphNode{
				Id:        mwId,
				Category:  "middleware",
				Name:      r.MiddlewareType + ":" + r.ConnectAddr,
				Label:     r.ConnectAddr,
				MwType:    r.MiddlewareType,
				Addr:      r.ConnectAddr,
				MwProject: r.MiddlewareProject,
				DepCount:  0,
			})
		}
		// 边（去重）
		ek := appId + "->" + mwId
		if !edgeSet[ek] {
			edgeSet[ek] = true
			resp.Edges = append(resp.Edges, model.GraphEdge{Source: appId, Target: mwId, Kind: "depend"})
		}
	}
	// 汇总中间件被依赖数与应用清单
	depMap := map[string]map[string]bool{}
	for _, e := range resp.Edges {
		if depMap[e.Target] == nil {
			depMap[e.Target] = map[string]bool{}
		}
		depMap[e.Target][e.Source] = true
	}
	for i := range resp.Nodes {
		if resp.Nodes[i].Category != "middleware" {
			continue
		}
		srcs := depMap[resp.Nodes[i].Id]
		resp.Nodes[i].DepCount = len(srcs)
		for s := range srcs {
			resp.Nodes[i].AppIds = append(resp.Nodes[i].AppIds, s)
		}
		sort.Strings(resp.Nodes[i].AppIds)
	}
	// 汇总本次快照出现的 Apollo 实例列表（供前端实例筛选下拉；按 id 排序稳定展示）。
	// 同一实例的全量元数据（name/host/appCount）已在应用节点归属处去重保存，这里按 id 收集一次
	hostMap := map[int]model.GraphHost{}
	for _, nd := range resp.Nodes {
		if nd.Category != "application" {
			continue
		}
		for _, h := range nd.ApolloHosts {
			if _, ok := hostMap[h.Id]; !ok {
				hostMap[h.Id] = h
			}
		}
	}
	hostOrder := make([]int, 0, len(hostMap))
	for id := range hostMap {
		hostOrder = append(hostOrder, id)
	}
	sort.Ints(hostOrder)
	for _, id := range hostOrder {
		resp.ApolloHosts = append(resp.ApolloHosts, hostMap[id])
	}
	// 规模汇总：与上述各 list 字段同一过滤口径，供前端直接展示“总体数据”
	for _, nd := range resp.Nodes {
		switch nd.Category {
		case "application":
			resp.Counts.Application++
		case "middleware":
			resp.Counts.Middleware++
		}
	}
	resp.Counts.ApolloHosts = len(resp.ApolloHosts)
	resp.Counts.Edges = len(resp.Edges)
	resp.Counts.Nodes = len(resp.Nodes)
	return resp, source
}

// GetNodeDetail 节点详情（§6.2 /detail）
// id 与图节点一致（app:{apollo_app_id} / mw:{middleware_type}:{connect_addr} / host:{apollo_host_id}），
// 详情含行级信息（库名、部门/负责人、关联应用/中间件/实例清单），前端详情面板直接渲染该结构，
// 无需再基于全量图数据在内存中推导。返回 source（db|file）本次活跃数据源（日志/本地验证用；原 X-Data-Source 响应头已下线）。
func GetNodeDetail(q model.NodeDetailQuery) (d model.NodeDetail, source string, err error) {
	d.Id = q.Id
	d.Env = q.Env
	if q.Id == "" {
		return d, "", fmt.Errorf("缺少节点 id")
	}
	r := currentRouter()
	if r == nil {
		return d, sourceDB, fmt.Errorf("无可用数据源（DB 与本地快照均未初始化）")
	}
	if q.SyncDate == "" {
		q.SyncDate, source = r.LatestSyncDateOf(q.Env, q.Source)
	}
	if q.SyncDate == "" {
		return d, source, fmt.Errorf("无可用快照（请先触发同步）")
	}
	d.SyncDate = q.SyncDate

	// 一次取当日该环境全量行，三种节点统一在内存聚合（DB/文件共用同一数据源路由；
	// 规模 = 单日单环境快照行数，与 graph 一次取数相当，避免详情对 DB 多次精查、
	// 对文件多次全扫的差异；被依赖应用数一次遍历预统计，无需额外查询）
	rows, source, err := r.QueryAll(RowFilter{SyncDate: q.SyncDate, Env: q.Env, Source: q.Source})
	if err != nil {
		return d, source, err
	}
	// 应用节点粒度：默认展开（部署点粒度 appId|部署项目|节点，需求方口径）；显式 false 才折叠为 appId。
	// app 节点 id 若自带 2 个「|」则必然为展开粒度（即使入参折叠，也按展开定位该部署点）——
	// 保证「图节点 id → 详情」两处口径一致（mw/host 节点 id 无此信息，只能由入参决定）。
	expand := expandDepNodeOn(q.ExpandDepNode)
	if strings.HasPrefix(q.Id, "app:") && strings.Count(strings.TrimPrefix(q.Id, "app:"), "|") == 2 {
		expand = true
	}
	// 展开模式按部署点扩行；折叠模式补充为 appId 属性（不扩行）
	if expand {
		rows = expandByDeploySlot(rows)
	} else {
		rows = attachAppAttrs(rows)
	}

	// 行 → 实例归属（d.ApolloHosts 按实例 id 去重合并）
	mergeHost := func(r model.OperationTopology) {
		if r.ApolloHostName == "" || hostIn(d.ApolloHosts, r.ApolloHostId) {
			return
		}
		d.ApolloHosts = append(d.ApolloHosts, model.GraphHost{
			Id: r.ApolloHostId, Name: r.ApolloHostName, Host: r.ApolloHostAddr, AppCount: r.HostAppCount,
		})
	}
	// 行 → 应用引用（d.Apps 按图节点 id app:{appKey} 合并，多套实例归属去重，属性多值取并集）
	mergeApp := func(r model.OperationTopology) {
		nodeId := "app:" + appKeyOf(r, expand)
		for i := range d.Apps {
			if d.Apps[i].NodeId != nodeId {
				continue
			}
			app := &d.Apps[i]
			app.DepProjects = unionSorted(app.DepProjects, r.DepProjects)
			app.EngProjects = unionSorted(app.EngProjects, r.EngProjects)
			app.PpApps = unionSorted(app.PpApps, r.PpApplications)
			app.Nodes = unionSorted(app.Nodes, r.Nodes)
			setFirst(&app.DepProject, app.DepProjects)
			setFirst(&app.EngProject, app.EngProjects)
			setFirst(&app.PpApp, app.PpApps)
			setFirst(&app.Node, app.Nodes)
			if r.ApolloHostName != "" && !hostIn(app.ApolloHosts, r.ApolloHostId) {
				app.ApolloHosts = append(app.ApolloHosts, model.GraphHost{
					Id: r.ApolloHostId, Name: r.ApolloHostName, Host: r.ApolloHostAddr, AppCount: r.HostAppCount,
				})
			}
			return
		}
		app := model.NodeDetailApp{
			NodeId: nodeId, ApolloAppId: r.ApolloAppId,
			ApolloAppName: r.ApolloAppName, AppDept: r.AppDept, AppOwner: r.AppOwner,
			EngProject: r.EngProject, DepProject: r.DepProject,
			DeployProjectName: r.DepProject,
			Node:              r.Node, PpApp: r.PpApplication,
			EngProjects: r.EngProjects, DepProjects: r.DepProjects,
			PpApps: r.PpApplications, Nodes: r.Nodes,
			ApolloHosts: []model.GraphHost{},
		}
		if r.ApolloHostName != "" {
			app.ApolloHosts = append(app.ApolloHosts, model.GraphHost{
				Id: r.ApolloHostId, Name: r.ApolloHostName, Host: r.ApolloHostAddr, AppCount: r.HostAppCount,
			})
		}
		d.Apps = append(d.Apps, app)
	}
	// 行 → 中间件引用（d.Middlewares 按 类型+地址 去重）
	mergeMw := func(r model.OperationTopology) {
		for _, m := range d.Middlewares {
			if m.MwType == r.MiddlewareType && m.Addr == r.ConnectAddr {
				return
			}
		}
		d.Middlewares = append(d.Middlewares, model.NodeDetailMw{
			MwType: r.MiddlewareType, Addr: r.ConnectAddr,
			MwDb: r.MiddlewareDb, MwProject: r.MiddlewareProject,
		})
	}
	// 全快照内 中间件(类型+地址) → 被依赖应用集合（去重 appid），一次遍历完成
	mwApps := map[string]map[string]bool{}
	for _, r0 := range rows {
		if r0.MiddlewareType == "" || r0.ConnectAddr == "" || r0.ApolloAppId == "" {
			continue
		}
		key := r0.MiddlewareType + ":" + r0.ConnectAddr
		if mwApps[key] == nil {
			mwApps[key] = map[string]bool{}
		}
		mwApps[key][r0.ApolloAppId] = true
	}

	switch {
	case strings.HasPrefix(q.Id, "app:"):
		key := strings.TrimPrefix(q.Id, "app:")
		// 节点 id 的 appKey 口径与 graph 一致（appKeyOf(r, expand)：折叠 = appId 优先；展开 = appId|部署项目|节点）
		sub := filterRows(rows, func(r0 model.OperationTopology) bool { return appKeyOf(r0, expand) == key })
		if len(sub) == 0 {
			return d, source, fmt.Errorf("未找到应用节点 %s（该快照下无此应用）", key)
		}
		d.Category = "application"
		d.ApolloAppId = sub[0].ApolloAppId
		d.ApolloAppName = sub[0].ApolloAppName
		d.AppDept = sub[0].AppDept
		d.AppOwner = sub[0].AppOwner
		// 应用属性（部署项目/工程项目/节点/PP 应用）为 appId 级多值：跨环境/多实例行取并集，单值字段取首值
		d.EngProject, d.DepProject = sub[0].EngProject, sub[0].DepProject
		d.Node, d.PpApp = sub[0].Node, sub[0].PpApplication
		d.EngProjects, d.DepProjects = sub[0].EngProjects, sub[0].DepProjects
		d.Nodes, d.PpApps = sub[0].Nodes, sub[0].PpApplications
		for _, r0 := range sub[1:] {
			d.EngProjects = unionSorted(d.EngProjects, r0.EngProjects)
			d.DepProjects = unionSorted(d.DepProjects, r0.DepProjects)
			d.Nodes = unionSorted(d.Nodes, r0.Nodes)
			d.PpApps = unionSorted(d.PpApps, r0.PpApplications)
		}
		setFirst(&d.EngProject, d.EngProjects)
		setFirst(&d.DepProject, d.DepProjects)
		d.DeployProjectName = d.DepProject
		setFirst(&d.Node, d.Nodes)
		setFirst(&d.PpApp, d.PpApps)
		d.Name = d.ApolloAppName
		if d.Name == "" {
			d.Name = key
		}
		sort.SliceStable(sub, func(i, j int) bool {
			if sub[i].MiddlewareType != sub[j].MiddlewareType {
				return sub[i].MiddlewareType < sub[j].MiddlewareType
			}
			return sub[i].ConnectAddr < sub[j].ConnectAddr
		})
		for _, r0 := range sub {
			mergeHost(r0)
			mergeMw(r0)
		}
		// 补各中间件在全快照的被依赖应用数（体现是否被多应用共享）
		for i := range d.Middlewares {
			d.Middlewares[i].DepCount = len(mwApps[d.Middlewares[i].MwType+":"+d.Middlewares[i].Addr])
		}

	case strings.HasPrefix(q.Id, "mw:"):
		rest := strings.TrimPrefix(q.Id, "mw:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) < 2 {
			return d, source, fmt.Errorf("中间件节点 id 不合法：%s", q.Id)
		}
		typ, addr := parts[0], parts[1]
		sub := filterRows(rows, func(r0 model.OperationTopology) bool {
			return r0.MiddlewareType == typ && r0.ConnectAddr == addr
		})
		if len(sub) == 0 {
			return d, source, fmt.Errorf("未找到中间件节点 %s（该快照下无此中间件）", typ+":"+addr)
		}
		d.Category = "middleware"
		d.MwType = typ
		d.Addr = addr
		d.MwDb = sub[0].MiddlewareDb
		d.MwProject = sub[0].MiddlewareProject
		d.Name = typ + ":" + addr
		sort.SliceStable(sub, func(i, j int) bool {
			if sub[i].ApolloAppId != sub[j].ApolloAppId {
				return sub[i].ApolloAppId < sub[j].ApolloAppId
			}
			return sub[i].ApolloHostId < sub[j].ApolloHostId
		})
		for _, r0 := range sub {
			mergeHost(r0)
			mergeApp(r0)
		}
		d.DepCount = len(d.Apps)

	case strings.HasPrefix(q.Id, "host:"):
		hid, err := strconv.Atoi(strings.TrimPrefix(q.Id, "host:"))
		if err != nil {
			return d, source, fmt.Errorf("实例节点 id 不合法：%s", q.Id)
		}
		sub := filterRows(rows, func(r0 model.OperationTopology) bool { return r0.ApolloHostId == hid })
		if len(sub) == 0 {
			return d, source, fmt.Errorf("未找到实例节点 %d（该快照下无此实例）", hid)
		}
		d.Category = "host"
		d.HostId = hid
		d.HostName = sub[0].ApolloHostName
		d.HostAddr = sub[0].ApolloHostAddr
		d.HostAppCount = sub[0].HostAppCount
		d.Name = d.HostName
		sort.SliceStable(sub, func(i, j int) bool {
			if sub[i].ApolloAppId != sub[j].ApolloAppId {
				return sub[i].ApolloAppId < sub[j].ApolloAppId
			}
			if sub[i].MiddlewareType != sub[j].MiddlewareType {
				return sub[i].MiddlewareType < sub[j].MiddlewareType
			}
			return sub[i].ConnectAddr < sub[j].ConnectAddr
		})
		for _, r0 := range sub {
			mergeApp(r0)
		}

	default:
		return d, source, fmt.Errorf("未知节点类型（需以 app:/mw:/host: 开头）：%s", q.Id)
	}
	return d, source, nil
}

// filterRows 按谓词过滤行集（保持原顺序）
func filterRows(rows []model.OperationTopology, keep func(model.OperationTopology) bool) []model.OperationTopology {
	out := make([]model.OperationTopology, 0, len(rows))
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// hostIn hosts 是否已包含指定 Apollo 实例 id
func hostIn(hosts []model.GraphHost, id int) bool {
	for _, h := range hosts {
		if h.Id == id {
			return true
		}
	}
	return false
}
