# 项目部署信息查询字段映射表

## 请求参数字段

| Key | 中文名称 | 类型 | 是否必须 | 说明 |
|-----|---------|------|----------|------|
| id | ID | string | 非必须 | 部署记录ID |
| pageNo | 页码 | int | 非必须 | 默认1 |
| pageSize | 每页大小 | int | 非必须 | 默认40 |
| name | 项目名称 | string | 非必须 | 部署项目名称 |
| ips | IP地址 | string | 非必须 | IP地址 |
| outv | 外部版本 | string | 非必须 | 外部版本号 |
| productId | 产品ID | string | 非必须 | 产品ID |
| product | 产品名称 | string | 非必须 | 产品名称 |
| deployEnv | 部署环境类型 | int | 非必须 | 1传统等 |
| operA.id | 负责人A ID | string | 非必须 | 负责人A的ID |
| operA.name | 负责人A姓名 | string | 非必须 | 负责人A姓名 |
| noServerFlag | 无服务器标志 | string | 非必须 | 无服务器标志 |
| noProductFlag | 无产品标志 | string | 非必须 | 无产品标志 |
| noDatabaseFlag | 无数据库标志 | string | 非必须 | 无数据库标志 |
| noOperatorFlag | 无运维标志 | string | 非必须 | 无运维标志 |
| totalProj | 总项目 | string | 非必须 | 总项目 |
| orderBy | 排序字段 | string | 非必须 | 排序字段 |

## 返回参数字段

| Key | 中文名称 | 类型 | 说明 |
|-----|---------|------|------|
| 部署项目名 | 部署项目名称 | string | 部署项目名称 |
| 整机项目名 | 整机项目名称 | string | 整机项目名称 |
| 工程项目名 | 工程项目名称 | string | 工程项目名称 |
| 所属产品 | 所属产品 | string | 产品名称 |
| 部署环境类型 | 环境类型 | string | 如传统 |
| 所在机房 | 机房 | string | 机房名称 |
| 研发 | 研发人员 | string | 研发人员姓名 |
| 负责人A | 负责人A | string | 负责人A姓名 |
| 负责人B | 负责人B | string | 负责人B姓名 |
| 反应地址 | 反应地址 | string | 反应地址 |
| 程序包 | 程序包 | string | 程序包名称 |

## 用户输入字段 → API参数映射

| 用户可能的中文表达 | 对应API参数 | 值类型 | 示例 |
|-------------------|-------------|--------|------|
| 项目名称/项目 | name | string | "guizh-rules-api" |
| 产品ID | productId | string | "1661fea29df44946ab2ff2bb9577179f" |
| 产品名称/产品 | product | string | "5G工业视宽平台" |
| 部署环境/环境 | deployEnv | integer | 1 |
| 负责人A | operA.name | string | "张三" |

## 环境类型映射

| 值 | 中文名称 | 说明 |
|----|---------|------|
| 1 | 传统 | 传统部署环境 |

## 支持后置过滤的字段

| 字段 | 中文名称 | 过滤方式 | 示例值 |
|------|---------|---------|--------|
| 部署环境类型 | 环境类型 | 值匹配 | = '传统' |
| 所在机房 | 机房 | 模糊匹配 | ~ '贵州' |
| 负责人A | 负责人 | 模糊匹配 | ~ '张' |