# -*- coding: utf-8 -*-
"""
生成中等规模模拟数据到 operation_topology 表
规模：~800 应用 × 平均6个中间件 ≈ 4800 行（当日快照）
      + 历史快照 2 天（约 3000 / 2000 行）用于快照对比测试
用法：python3 gen_data.py
"""
import random
import pymysql

random.seed(42)

DB = dict(host="127.0.0.1", port=3306, user="root", password="root123",
          database="devops_automation_test", charset="utf8mb4")

# ---------- 基础池 ----------
NODES = ["su-zh", "sh-gd", "gz-dx", "bj-yd", "hz-ct", "cq-nc"]
ENGS = ["video", "data", "order", "pay", "ai", "base-service"]

APP_CNT = 800
SERVICE_CNT = 100  # 中间件实例数

# 中间件实例池：类型 -> (项目名前缀, 主机名前缀, 端口, 实例数)
MW_POOL = [
    ("mysql",        "mysql",       "db",      3306, 25),
    ("redis",        "redis",       "cache",   6379, 25),
    ("kafka",        "kafka",       "broker",  9092, 15),
    ("elasticsearch", "es",         "es",      9200, 10),
    ("mongodb",      "mongodb",     "mongo",   27017, 8),
    ("rocketmq",     "rocketmq",    "rmq",     9876, 6),
    ("rabbitmq",     "rabbitmq",    "mq",      5672, 5),
    ("nacos",        "nacos",       "nacos",   8848, 3),
    ("zookeeper",    "zookeeper",   "zk",      2181, 2),
    ("memcached",    "memcached",   "memc",    11211, 1),
]

# 共享中间件（扇入热点）：(类型, 地址) -> 被依赖应用比例
HOT_MW = [
    ("mysql", "db-master:3306", 0.40),
    ("mysql", "user-db:3306",   0.30),
    ("mysql", "order-db:3306",  0.20),
    ("redis", "redis-cache:6379", 0.40),
    ("redis", "session-cache:6379", 0.20),
    ("kafka", "broker-core:9092", 0.15),
]


def build_mw_instances():
    """生成 100 个中间件实例：(type, addr, project)"""
    instances = []
    for mw_type, proj_pref, host_pref, port, cnt in MW_POOL:
        for i in range(cnt):
            # 热点的前几个实例用固定地址（与 HOT_MW 对应）
            addr = None
            for t, a, _ in HOT_MW:
                if t == mw_type and a.startswith(f"{host_pref}-") is False:
                    pass
            if mw_type == "mysql" and i == 0:
                addr = "db-master:3306"
            elif mw_type == "mysql" and i == 1:
                addr = "user-db:3306"
            elif mw_type == "mysql" and i == 2:
                addr = "order-db:3306"
            elif mw_type == "redis" and i == 0:
                addr = "redis-cache:6379"
            elif mw_type == "redis" and i == 1:
                addr = "session-cache:6379"
            elif mw_type == "kafka" and i == 0:
                addr = "broker-core:9092"
            if addr is None:
                addr = f"{host_pref}-{i:02d}:{port}"
            instances.append((mw_type, addr, f"{proj_pref}-{i:02d}"))
    return instances


def build_apps():
    """生成 800 个应用：返回 [(node, eng, dep, pp, appid)]"""
    apps = []
    for i in range(APP_CNT):
        node = NODES[i % len(NODES)]
        eng = ENGS[i % len(ENGS)]
        dep = f"app-{i:04d}"
        pp = dep if i % 5 != 0 else ""   # 20% PP 应用名为空
        appid = f"{eng}-{i:04d}"
        apps.append((node, eng, dep, pp, appid))
    return apps


def gen_rows(apps, instances, sync_date, hot_scale=1.0):
    """为指定日期生成关系行：每个应用连 2~8 个中间件（命中热点池则按比例依赖）"""
    rows = []
    hot_lookup = {(t, a): p for t, a, p in HOT_MW}
    for node, eng, dep, pp, appid in apps:
        chosen = set()
        # 先按热点概率加入热点中间件
        for (t, a), p in hot_lookup.items():
            if random.random() < p * hot_scale:
                chosen.add((t, a))
        # 补充随机中间件达到 2~8 个
        target = random.randint(2, 8)
        while len(chosen) < target:
            t, a, proj = random.choice(instances)
            chosen.add((t, a))
        # 生成行
        for t, a in chosen:
            proj = next(pr for tt, aa, pr in instances if tt == t and aa == a)
            rows.append((
                node, eng, dep, pp, appid,
                "application", "spring.datasource.url" if t == "mysql" else "spring.redis.host" if t == "redis" else f"{t}.addr",
                t, a, proj,
                "PRO", sync_date, "gen_data", "中等规模模拟数据"
            ))
    return rows


def main():
    mw = build_mw_instances()
    apps = build_apps()
    print(f"中间件实例: {len(mw)}  应用: {len(apps)}")

    conn = pymysql.connect(**DB)
    cur = conn.cursor()

    # 3 个快照：今天全量，昨天/前天为历史子集
    rows_today = gen_rows(apps, mw, "2026-09-01", hot_scale=1.0)
    rows_yesterday = gen_rows(apps[:600], mw, "2026-08-31", hot_scale=0.8)
    rows_prev = gen_rows(apps[:400], mw, "2026-08-30", hot_scale=0.7)

    all_rows = rows_today + rows_yesterday + rows_prev

    # 先清理这三天已有数据，保证幂等
    cur.execute("DELETE FROM operation_topology WHERE sync_date IN ('2026-09-01','2026-08-31','2026-08-30')")
    conn.commit()
    print(f"清理旧数据完成")

    # 分批插入
    BATCH = 1000
    sql = ("INSERT INTO operation_topology "
           "(node, eng_project, dep_project, pp_application, apollo_app_id, "
           " namespace, config_key, middleware_type, connect_addr, middleware_project, "
           " env, sync_date, source, remark) "
           "VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)")
    for i in range(0, len(all_rows), BATCH):
        cur.executemany(sql, all_rows[i:i + BATCH])
        conn.commit()
    print(f"插入完成: 今日 {len(rows_today)} / 昨日 {len(rows_yesterday)} / 前日 {len(rows_prev)}，共 {len(all_rows)}")

    # 汇总校验
    cur.execute("SELECT sync_date, COUNT(*) FROM operation_topology GROUP BY sync_date")
    for r in cur.fetchall():
        print(f"  快照 {r[0]}: {r[1]} 行")
    cur.close()
    conn.close()


if __name__ == "__main__":
    main()
