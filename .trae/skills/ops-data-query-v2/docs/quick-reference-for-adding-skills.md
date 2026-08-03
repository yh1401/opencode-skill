# 新增技能快速参考

## 🚀 快速添加技能（只需3步）

### 第一步：创建技能目录结构

```bash
skills/
└── new-skill/              # 新技能目录
    ├── SKILL.md           # 技能定义（必填）
    ├── config/            # 配置目录（可选）
    │   └── params.yaml    # 参数配置
    └── references/        # 参考文档（可选）
        └── fields.md      # 字段映射
```

### 第二步：编写 SKILL.md

```markdown
---
name: new_skill
description: 新技能功能描述
keywords:
  - 关键词1
  - 关键词2
tool_type: function
response_format: markdown
parameters:
  - name: param1
    type: string
    label: 参数1
    required: false
---

# 新技能（new-skill）

## 1. When to Activate（触发条件）

当用户查询**XXX**相关信息时触发此技能。

### 触发关键词
- XXX查询、获取XXX、查找XXX

### 排他性判断
若同时包含其他技能关键词（如"服务器"、"部署"），需结合整体意图判断。

## 2. How It Works（执行流程）

### 核心目标
通过自然语言查询，返回XXX信息列表。

### 执行步骤
1. 意图识别 → 识别为XXX查询
2. 参数提取 → 提取查询条件
3. API调用 → 调用 XXX 接口
4. 数据处理 → 格式化返回结果

## 3. Examples（示例）

### 示例1：基础查询
**输入**："查询XXX信息"

**输出**：返回符合条件的数据列表

## 4. Anti-Patterns（常见错误）

- ❌ 不处理无结果情况
  ✅ 返回友好提示："未查询到匹配的XXX信息"
```

### 第三步：注册技能

在 `registry/skills.json` 的 `skills` 数组中添加：

```json
{
  "id": "new-skill",
  "name": "新技能名称",
  "description": "技能功能描述",
  "enabled": true,
  "path": "skills/new-skill/SKILL.md",
  "keywords": ["关键词1", "关键词2", "关键词3"],
  "version": "1.0.0"
}
```

---

## ✅ 最低完成标准

只需完成以上3步即可添加一个可用的技能！

| 步骤 | 必填项 | 说明 |
|------|--------|------|
| 1. 目录结构 | ✅ SKILL.md | 其他文件可选 |
| 2. SKILL.md | ✅ 完整填写 YAML 元数据<br>✅ 明确触发条件<br>✅ 基本执行流程<br>✅ 至少1个示例 | 确保技能可被正确路由和执行 |
| 3. 注册中心 | ✅ 添加到 skills.json | 6个核心字段即可 |

---

## 📝 注册中心字段说明

只需填写以下 **7个核心字段**：

| 字段 | 必填 | 说明 | 示例 |
|------|------|------|------|
| `id` | ✅ | 技能唯一标识（英文、短横线） | `cmdb-server-query` |
| `name` | ✅ | 技能显示名称（中文） | `CMDB服务器查询` |
| `description` | ✅ | 一句话功能描述 | `查询服务器信息、机房、状态等` |
| `enabled` | ✅ | 是否启用（默认 true） | `true` |
| `path` | ✅ | SKILL.md 相对路径 | `skills/cmdb-server-query/SKILL.md` |
| `keywords` | ✅ | 3-5个中文关键词 | `["服务器", "机房", "CMDB"]` |
| `version` | 建议 | 版本号（可选） | `1.0.0` |

---

## ⚠️ 命名规范

| 项目 | 规范 | 正确示例 | 错误示例 |
|------|------|----------|----------|
| **ID** | 短横线命名 | `new-skill` | `new_skill`、`NewSkill` |
| **路径** | 相对于 skill-agent/ | `skills/cmdb-server-query/SKILL.md` | `/Users/.../SKILL.md` |
| **关键词** | 中文、简洁、3-5个 | `["服务器", "机房", "CMDB"]` | `["查询服务器信息", "获取主机列表"]` |
| **版本号** | 语义化版本（建议） | `1.0.0`、`1.2.3` | `v1`、`latest` |

---

## 🎯 SKILL.md 快速模板

```markdown
---
name: skill_id
description: 技能功能描述
keywords:
  - 关键词1
  - 关键词2
tool_type: function
response_format: markdown
parameters:
  - name: param1
    type: string
    label: 参数1
    required: false
---

# 技能名称（skill-id）

## 1. When to Activate（触发条件）

当用户查询**XXX**相关信息时触发此技能。

### 触发关键词
- XXX查询、获取XXX、查找XXX

### 排他性判断
若同时包含其他技能关键词，需结合整体意图判断。

## 2. How It Works（执行流程）

### 核心目标
通过自然语言查询，返回XXX信息列表。

### 执行步骤
1. 意图识别 → 识别为XXX查询
2. 参数提取 → 提取查询条件
3. API调用 → 调用 XXX 接口
4. 数据处理 → 格式化返回结果

## 3. Examples（示例）

### 示例1：基础查询
**输入**："查询XXX信息"

**输出**：
```
共查询到 X 条记录：

| 序号 | 字段1 | 字段2 |
| ---- | ------ | ------ |
| 1    | 值1   | 值2   |
```

## 4. Anti-Patterns（常见错误）

- ❌ 不处理无结果情况
  ✅ 返回友好提示："未查询到匹配的XXX信息"

- ❌ 不校验参数格式
  ✅ 提示用户修正无效参数

- ❌ 网络异常未处理
  ✅ 检查网络连接，记录日志并提示用户稍后重试
```

---

## 📚 扩展文件（可选）

### config/params.yaml
```yaml
parameters:
  - name: param1
    type: string
    label: 参数1
    required: false
    description: 参数描述
    examples:
      - 示例值1
      - 示例值2
```

### references/fields.md
```markdown
## 字段映射表

| API字段 | 中文名称 | 类型 | 说明 |
|---------|----------|------|------|
| field1  | 字段1   | string | 字段描述 |
| field2  | 字段2   | integer | 字段描述 |
```

---

## ✅ 完成后检查清单

- [ ] 技能ID唯一，未与其他技能冲突
- [ ] SKILL.md 包含完整 YAML 元数据
- [ ] 触发条件明确，包含排他性判断
- [ ] 执行流程清晰，至少包含4个基本步骤
- [ ] 至少1个示例（输入+输出）
- [ ] 常见错误处理完整
- [ ] 已添加到 `registry/skills.json`
- [ ] 关键词与技能功能相关，3-5个
