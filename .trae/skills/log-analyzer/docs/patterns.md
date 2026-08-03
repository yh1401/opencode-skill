# 错误模式规则库

本文档描述日志分析工具中内置的错误模式规则，用于根因推断和问题分类。

## 规则格式

每条规则包含以下字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| id | string | 规则唯一标识 |
| name | string | 规则名称（中文） |
| severity | string | 严重级别：critical / high / medium / low |
| patterns | list | 正则表达式列表，匹配错误消息 |
| cause | string | 可能的原因描述 |
| remediation | list | 建议的修复措施 |
| impact | string | 影响范围描述 |

## 内置规则

### 1. 数据库连接失败

**ID**: `db_connection_failed`
**级别**: critical

**匹配模式**:
- `Connection refused.*database|DB.*connection.*failed`
- `Cannot.*connect.*to.*database`
- `DatabaseConnectionException|SQLException.*connection`
- `MySQLNonTransientConnectionException`
- `PostgreSQL.*connection.*timeout`
- `OracleConnectionException`

**原因**: 数据库服务未启动、网络不通或连接池耗尽

**建议措施**:
1. 检查数据库服务是否正常运行
2. 验证数据库连接配置（主机、端口、用户名、密码）
3. 检查网络连通性（防火墙、安全组）
4. 查看数据库连接池配置，考虑增加最大连接数
5. 检查数据库磁盘空间是否充足

**影响**: 所有依赖数据库的功能不可用

---

### 2. 内存溢出

**ID**: `out_of_memory`
**级别**: critical

**匹配模式**:
- `OutOfMemoryError|java.lang.OutOfMemoryError`
- `OOM.*error|Memory.*leak`
- `GC.*overhead.*limit.*exceeded`
- `PermGen.*space|Metaspace.*error`

**原因**: JVM堆内存不足或存在内存泄漏

**建议措施**:
1. 增加JVM堆内存参数（-Xmx、-Xms）
2. 分析堆转储文件定位内存泄漏
3. 检查是否有大对象未释放
4. 优化GC策略和内存使用
5. 考虑使用内存分析工具（如VisualVM、MAT）

**影响**: 服务进程崩溃，需要重启

---

### 3. 网络超时

**ID**: `network_timeout`
**级别**: high

**匹配模式**:
- `SocketTimeoutException|ConnectTimeoutException`
- `Connection.*timeout|Read.*timeout`
- `Timed.*out.*while.*connecting`
- `netty.*timeout`
- `HTTP.*timeout`

**原因**: 网络延迟过高、目标服务响应慢或不可达

**建议措施**:
1. 检查网络链路状态和延迟
2. 增加超时时间配置
3. 检查目标服务是否正常响应
4. 考虑引入重试和熔断机制
5. 查看DNS解析是否正常

**影响**: 部分请求失败，用户体验下降

---

### 4. 线程池耗尽

**ID**: `thread_pool_exhausted`
**级别**: high

**匹配模式**:
- `ThreadPoolExecutor.*rejected|RejectedExecutionException`
- `Thread.*pool.*exhausted|All.*threads.*busy`
- `ExecutorService.*rejected`

**原因**: 线程池配置过小或任务提交过快

**建议措施**:
1. 增加线程池大小（corePoolSize、maxPoolSize）
2. 优化任务处理逻辑，减少耗时
3. 增加队列容量
4. 配置合理的拒绝策略
5. 考虑异步处理和解耦

**影响**: 新请求被拒绝，服务不可用

---

### 5. 磁盘空间不足

**ID**: `file_system_full`
**级别**: high

**匹配模式**:
- `Disk.*full|No.*space.*left.*on.*device`
- `FileNotFoundException.*write`
- `IOException.*disk|Storage.*full`

**原因**: 磁盘空间已满或配额不足

**建议措施**:
1. 清理日志文件和临时文件
2. 扩展磁盘空间
3. 配置日志自动清理策略
4. 检查是否有异常大文件产生
5. 设置磁盘空间告警

**影响**: 无法写入日志和数据，服务异常

---

### 6. 认证失败

**ID**: `authentication_failed`
**级别**: medium

**匹配模式**:
- `AuthenticationException|InvalidTokenException`
- `Login.*failed|Unauthorized|401.*Unauthorized`
- `JWT.*expired|Token.*invalid`
- `Invalid.*credentials|Bad.*credentials`

**原因**: 用户凭证无效、令牌过期或认证配置错误

**建议措施**:
1. 检查用户认证凭证是否正确
2. 验证JWT令牌配置和过期时间
3. 检查OAuth配置
4. 查看认证服务是否正常
5. 检查LDAP/数据库认证配置

**影响**: 用户无法登录，部分功能受限

---

### 7. 配置错误

**ID**: `configuration_error`
**级别**: high

**匹配模式**:
- `ConfigurationException|InvalidConfiguration`
- `Config.*error|Property.*not.*found`
- `Missing.*configuration|Config.*parse.*error`
- `YAML.*parse.*error|JSON.*parse.*error`

**原因**: 配置文件格式错误或缺少必要配置项

**建议措施**:
1. 检查配置文件语法和格式
2. 验证所有必要配置项是否存在
3. 检查配置文件编码
4. 使用配置校验工具
5. 回滚到之前的正确配置

**影响**: 服务无法启动或功能异常

---

### 8. 缓存穿透/失效

**ID**: `cache_miss`
**级别**: medium

**匹配模式**:
- `Cache.*miss|Redis.*connection.*error`
- `Memcached.*timeout|Cache.*not.*available`
- `Unable.*to.*connect.*to.*cache`

**原因**: 缓存服务不可用或缓存键不存在

**建议措施**:
1. 检查缓存服务是否正常运行
2. 验证缓存连接配置
3. 增加缓存预热机制
4. 配置缓存降级策略
5. 检查缓存键设计是否合理

**影响**: 请求直接打到数据库，可能导致数据库压力增大

---

### 9. 限流触发

**ID**: `rate_limit_exceeded`
**级别**: low

**匹配模式**:
- `RateLimitExceeded|TooManyRequests`
- `429.*Too.*Many.*Requests|Rate.*limit.*exceeded`
- `Throttled|Quota.*exceeded`

**原因**: 请求频率超过系统限流阈值

**建议措施**:
1. 检查限流配置是否合理
2. 考虑增加限流阈值
3. 优化客户端请求频率
4. 引入熔断和降级机制
5. 实现请求排队

**影响**: 部分请求被拒绝，需要重试

---

### 10. 死锁检测

**ID**: `deadlock_detected`
**级别**: critical

**匹配模式**:
- `Deadlock.*detected|DeadlockException`
- `Thread.*deadlock|Lock.*timeout`
- `Circular.*wait.*detected`

**原因**: 多个线程互相持有对方需要的锁

**建议措施**:
1. 分析线程转储定位死锁
2. 检查锁的获取顺序
3. 使用定时锁替代永久锁
4. 减少锁的粒度和持有时间
5. 考虑使用无锁数据结构

**影响**: 线程阻塞，服务响应缓慢或无响应

---

### 11. SSL证书错误

**ID**: `ssl_certificate_error`
**级别**: high

**匹配模式**:
- `SSLHandshakeException|CertificateException`
- `SSL.*error|Certificate.*expired`
- `Untrusted.*certificate|Invalid.*SSL`

**原因**: SSL证书过期、不匹配或未配置

**建议措施**:
1. 检查SSL证书有效期
2. 验证证书域名是否匹配
3. 更新SSL证书
4. 检查证书链配置
5. 验证SSL协议版本

**影响**: HTTPS请求失败，服务不可访问

---

### 12. 依赖服务失败

**ID**: `dependency_failure`
**级别**: high

**匹配模式**:
- `ServiceUnavailableException|503.*Service.*Unavailable`
- `Downstream.*error|Upstream.*timeout`
- `Failed.*to.*call.*service|RPC.*failed`
- `gRPC.*error|Dubbo.*timeout`

**原因**: 下游依赖服务不可用或响应超时

**建议措施**:
1. 检查依赖服务健康状态
2. 验证服务间网络连通性
3. 配置超时和重试策略
4. 实现熔断和降级
5. 考虑服务降级方案

**影响**: 依赖该服务的功能不可用

## 添加自定义规则

要添加自定义规则，需在 `scripts/root_cause.py` 的 `RootCauseEngine._load_patterns()` 方法中添加新的规则字典：

```python
{
    "id": "custom_error",
    "name": "自定义错误",
    "severity": "medium",
    "patterns": [r"CustomException.*"],
    "cause": "自定义错误原因",
    "remediation": ["建议措施1", "建议措施2"],
    "impact": "影响描述",
}
```

### 规则编写最佳实践

1. **正则表达式**: 使用非贪婪匹配，避免过度匹配
2. **严重级别**: 根据影响范围选择合适的级别
3. **建议措施**: 提供具体、可操作的步骤
4. **性能**: 复杂正则表达式可能影响解析性能，需权衡

## 扩展接口

### 运行时添加规则

```python
from scripts.root_cause import RootCauseEngine

engine = RootCauseEngine()
engine.add_custom_pattern({
    "id": "my_custom_rule",
    "name": "自定义规则",
    "severity": "high",
    "patterns": [r"MyCustomException.*"],
    "cause": "自定义异常",
    "remediation": ["检查相关配置"],
    "impact": "部分功能受影响",
})
```

### 列出所有规则

```python
from scripts.root_cause import RootCauseEngine

engine = RootCauseEngine()
patterns = engine.list_patterns()
for p in patterns:
    print(f"{p['id']}: {p['name']} ({p['severity']})")
```