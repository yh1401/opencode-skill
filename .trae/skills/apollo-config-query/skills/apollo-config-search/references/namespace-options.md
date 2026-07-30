# Apollo Namespace 可选值列表

> 本文档维护 Apollo 配置中心中的常见 Namespace 列表，用于用户输入到 Namespace 名称的映射。
> 当用户输入无法匹配到以下列表时，原值传递给 API。

---

## 常见 Namespace 列表

| Namespace 名称 | 用户输入关键词 | 说明 | 格式 |
|---------------|---------------|------|------|
| application | 默认、application、默认配置 | 默认应用配置 | properties |
| datasource | 数据库配置、datasource、数据库 | 数据源配置（MySQL/PostgreSQL） | properties |
| redis | Redis配置、redis、缓存配置 | Redis 连接配置 | properties |
| logging | 日志配置、logging、log | 日志级别和输出配置 | properties |
| common | 公共配置、common、公共 | 公共配置（多应用共享） | properties |
| mq | 消息队列配置、mq、kafka | 消息队列配置 | properties |
| thread | 线程池配置、thread、线程 | 线程池参数配置 | properties |
| rate-limit | 限流配置、rate-limit、限流 | 限流策略配置 | properties |
| feature-switch | 功能开关、feature-switch | 功能开关配置 | properties |
| api-config | API配置、api-config | API 接口配置 | properties |

---

## 使用说明

1. **匹配规则**：用户输入的 Namespace 名称与上表"用户输入关键词"列做包含匹配
2. **匹配成功**：取对应的 Namespace 名称传递给 API
3. **匹配失败**：将用户输入原值传递给 API
4. **格式说明**：所有 Namespace 均为 properties 格式
5. **更新时机**：当 Apollo 中新增 Namespace 时，同步更新本表
