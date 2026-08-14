# Apollo 服务/套（多套）选择参考

> 第三方接口会返回多套 Apollo（各产品线/地域独立部署），查询前需先确定**哪一套**。
> **线上以 MCP 工具 `apollo_host_list` 返回为准**，其中每条记录的 `apolloHostId`（即第三方记录 `id`）是查询链路入口：把用户描述（地名/产品）映射到套名，再取该套的 `apolloHostId` 传入 `apollo_config_query` / `apollo_app_list`。
>
> 术语约定：本文件中的"环境"指**哪套 Apollo 服务**；若用户提到 DEV/PRO/生产/测试等**环境名关键词**，则指应用环境 env（见参数指南"0. 环境术语判定规则"）。同时出现地名+环境名（如"广州4生产"）时，地名→套(apolloHostId)、环境名→env，两者都生效。

---

## 常用关键词 → Apollo 服务/套（示例）

| 用户描述关键词 | 可能的 Apollo 服务名称 | 说明 |
|---------------|----------------------|------|
| 贵州、亿讯 | 天翼云眼贵州测试Apollo-亿讯专用 | 贵州测试环境 |
| 广州4、生产 | 天翼云眼广州4多AZ生产Apollo | 广州4生产环境 |
| 广州4测试 | 广州4测试apollo | 广州4测试环境 |
| P2P、易联家、ehome | 3.0 P2P 易联家Apollo | P2P 产品线 |
| 小A平台 | 小A平台-广州4e-gz4etmpl-Apollo / 小A平台-广州4E-gz4etest-Apollo | 小A平台（多套） |
| 百川 | 视联百川 | 视联百川 |
| 看家 | 天翼看家广州4多AZ生产Apollo | 天翼看家 |
| 工单 | 工单系统施工-测试 | 工单系统 |
| 云化 | 云化摄像头Apollo | 云化摄像头 |

---

## 使用规则

1. **优先动态获取**：用户未明确指定时，先调用 `apollo_host_list` 查看全部可用 Apollo 服务/套（以实时返回的 `apolloHostId` 为准）
2. **关键词匹配**：用户提到地名/产品名时，先按上表定位服务名称，再从 `apollo_host_list` 返回中取对应记录的 `apolloHostId`，作为 `apolloHostId` 参数传入 `apollo_config_query` / `apollo_app_list`
3. **多套命中**：一个关键词命中多套（如"小A平台"）时，列出候选让用户确认
4. **默认兜底**：无法匹配时，不传 `apolloHostId`，使用 MCP 默认 Apollo（`APOLLO_HOST_NAME` 配置或第一条）
5. **更新时机**：第三方接口返回的服务列表变化时，同步更新本表
