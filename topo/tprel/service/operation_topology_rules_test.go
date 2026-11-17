package service

import (
	"testing"

	m "devops-automation/api/tprel/model"
)

// 规则表回归测试：保证常见别名/简写/大小写变体能被识别，且不串类型、不过度兜底。
// 修改 middlewareRules/valueRules 顺序或关键词后必须保持本文件全部通过
//（顺序原则：同类型内“精确 → 别名 → 宽泛”，第一个命中即停）。

func wantType(t *testing.T, fn func(string) *MiddlewareRule, key string, want string) {
	t.Helper()
	r := fn(key)
	if r == nil {
		t.Errorf("%s 匹配规则失败：未命中（期望 %s）", key, want)
		return
	}
	if r.Type != want {
		t.Errorf("%s 误判类型：得到 %s（期望 %s）", key, r.Type, want)
	}
}

// key 路径：别名 / 简写 / 横点两种写法 / 大小写
func TestMatchRuleAliasesAndCase(t *testing.T) {
	// mysql
	wantType(t, matchRule, "spring.datasource.url", "mysql")
	wantType(t, matchRule, "spring.datasource.druid.url", "mysql")
	wantType(t, matchRule, "mysql.host", "mysql")
	// redis
	wantType(t, matchRule, "spring.redis.host", "redis")
	wantType(t, matchRule, "spring.data.redis.host", "redis")
	wantType(t, matchRule, "redisson.singleServerConfig.address", "redis")
	wantType(t, matchRule, "redis.host", "redis")
	wantType(t, matchRule, "custom.redis-cluster.nodes", "redis")
	// kafka：横线（Spring）与点（原生属性）两种写法
	wantType(t, matchRule, "spring.kafka.bootstrap-servers", "kafka")
	wantType(t, matchRule, "kafka.bootstrap.servers", "kafka")
	wantType(t, matchRule, "kafka.producer.bootstrap.servers", "kafka")
	// rocketmq：短横与 camel 风格
	wantType(t, matchRule, "rocketmq.name-server", "rocketmq")
	wantType(t, matchRule, "rocketmq.namesrvAddr", "rocketmq")
	// elasticsearch
	wantType(t, matchRule, "spring.elasticsearch.uris", "elasticsearch")
	wantType(t, matchRule, "es.cluster.nodes", "elasticsearch")
	// mongodb：带/不带 b
	wantType(t, matchRule, "spring.data.mongodb.uri", "mongodb")
	wantType(t, matchRule, "spring.data.mongo.uri", "mongodb")
	wantType(t, matchRule, "mongo.host", "mongodb")
	// memcached：带/不带 d
	wantType(t, matchRule, "memcached.servers", "memcached")
	wantType(t, matchRule, "memcache.servers", "memcached")
	// zookeeper：全称与 zk 简写
	wantType(t, matchRule, "zookeeper.address", "zookeeper")
	wantType(t, matchRule, "zookeeper.servers", "zookeeper")
	wantType(t, matchRule, "zk.servers", "zookeeper")
	// nacos
	wantType(t, matchRule, "nacos.server-addr", "nacos")
	wantType(t, matchRule, "spring.cloud.nacos.config.server-addr", "nacos")
	// rabbitmq：全称 / amqp / rabbit
	wantType(t, matchRule, "spring.rabbitmq.addresses", "rabbitmq")
	wantType(t, matchRule, "rabbitmq.host", "rabbitmq")
	wantType(t, matchRule, "amqp.host", "rabbitmq")
	wantType(t, matchRule, "rabbit.host", "rabbitmq")
	// 大小写变体
	wantType(t, matchRule, "Spring.Redis.Host", "redis")
	wantType(t, matchRule, "SPRING.DATASOURCE.URL", "mysql")
	wantType(t, matchRule, "Kafka.Bootstrap-Servers", "kafka")
}

// value 路径：协议变体与大小写；无协议特征的裸地址必须留给 key 规则（不误判）
func TestMatchValueRuleProtocolVariants(t *testing.T) {
	wantType(t, matchValueRule, "jdbc:mysql://db-master:3306/mydb", "mysql")
	wantType(t, matchValueRule, "jdbc:mysql:loadbalance://10.1.1.1:3306,10.1.1.2:3306/db", "mysql")
	wantType(t, matchValueRule, "Redis://redis-cache:6379", "redis")
	wantType(t, matchValueRule, "rediss://redis-cache:6380", "redis")
	wantType(t, matchValueRule, "mongodb://mongo-a:27017/app", "mongodb")
	wantType(t, matchValueRule, "mongodb+srv://cluster0.x.mongodb.net/app", "mongodb")
	wantType(t, matchValueRule, "AMQP://user:pw@rabbit:5672/", "rabbitmq")
	wantType(t, matchValueRule, "amqps://rabbit:5671/", "rabbitmq")
	wantType(t, matchValueRule, "memcached://mc:11211", "memcached")
	wantType(t, matchValueRule, "memcache://mc:11211", "memcached")
	wantType(t, matchValueRule, "zookeeper://zk1:2181,zk2:2181", "zookeeper")
	// 无协议特征：不能靠 value 兜底（kafka/rocketmq 等裸地址留给 key 规则）
	if r := matchValueRule("kafka-1:9092,kafka-2:9092"); r != nil {
		t.Errorf("裸地址误走 value 兜底：得到 %s（应留给 key 规则）", r.Type)
	}
}

// 顺序敏感：宽泛词不吞精确规则；纯参数 key（无地址）不应命中任何规则
func TestMatchRuleOrderingAndNoFalseHit(t *testing.T) {
	// spring.data.mongo.uri 必须命中不带 b 的别名，而不是其它类型
	wantType(t, matchRule, "spring.data.mongo.uri", "mongodb")
	// redisson 属 redis 客户端，不能被吞成别的类型
	wantType(t, matchRule, "redisson.singleServerConfig.address", "redis")
	// 纯参数/业务 key 不命中
	if r := matchRule("server.port"); r != nil {
		t.Errorf("server.port 误命中：%s", r.Type)
	}
	if r := matchRule("spring.application.name"); r != nil {
		t.Errorf("spring.application.name 误命中：%s", r.Type)
	}
}

// 落盘方式 B：执行完整但个别应用失败时，仅把失败应用的上版快照行并入本次产物，
// 且不与本次成功行重复、同步日期统一改写为当前批。
func TestMergeUnfinishedRows(t *testing.T) {
	base := func(appId, hostName, env, typ, addr string) m.OperationTopology {
		return m.OperationTopology{
			ApolloAppId: appId, ApolloHostName: hostName, ApolloHostId: 13,
			Env: env, SyncDate: "2026-09-08", MiddlewareType: typ, ConnectAddr: addr,
		}
	}
	// 上版快照：ok-app（本轮成功，不应继承）、fail-app（本轮失败，应继承）、gone-app（本轮已消失，不继承）
	prev := []m.OperationTopology{
		base("ok-app", "h1", "PRO", "mysql", "db:3306"),
		base("fail-app", "h1", "PRO", "mysql", "db-fail:3306"),
		base("fail-app", "h1", "PRO", "redis", "redis-fail:6379"),
		base("gone-app", "h1", "PRO", "kafka", "kfk:9092"),
	}
	// 本轮产物：ok-app 有新行、fail-app 有一行与本轮成功行同键（该键本轮存在，不应再继承）
	all := []m.OperationTopology{
		base("ok-app", "h1", "PRO", "mysql", "db:3306"), // 同键同构：本轮已成功
		base("ok-app", "h1", "PRO", "redis", "redis-new:6379"),
		base("fail-app", "h1", "PRO", "mysql", "db-fail:3306"), // 同键：本轮其实已成功（理论上不会进 unfinished，防御性去重）
	}
	out := mergeUnfinishedRows(all, prev, []string{"fail-app"}, "2026-09-09")
	// 期望：3 行本轮 + fail-app 的 redis 旧行继承（1 行），mysql 同键不重复、gone-app 不继承
	if len(out) != 4 {
		t.Fatalf("期望 4 行（3 本轮 + 1 继承），得到 %d", len(out))
	}
	inherited := 0
	for _, r := range out {
		if r.ApolloAppId == "fail-app" && r.MiddlewareType == "redis" {
			inherited++
			if r.SyncDate != "2026-09-09" {
				t.Errorf("继承行同步日期未改写为当前批: %+v", r)
			}
		}
	}
	if inherited != 1 {
		t.Errorf("期望继承 fail-app 的 redis 旧行 1 行，得到 %d", inherited)
	}
	// 继承去重：fail-app 的 mysql 行与本轮产物同键，不应重复出现
	dup := 0
	for _, r := range out {
		if r.ApolloAppId == "fail-app" && r.MiddlewareType == "mysql" {
			dup++
		}
	}
	if dup != 1 {
		t.Errorf("fail-app mysql 行应只有本轮 1 行（同键不继承），得到 %d", dup)
	}
	// 空失败名单 / 无旧快照：原样返回
	if got := mergeUnfinishedRows(all, prev, nil, "2026-09-09"); len(got) != len(all) {
		t.Errorf("无失败名单时应原样返回，得到 %d 行", len(got))
	}
	if got := mergeUnfinishedRows(all, nil, []string{"fail-app"}, "2026-09-09"); len(got) != len(all) {
		t.Errorf("无旧快照时应原样返回，得到 %d 行", len(got))
	}
}
