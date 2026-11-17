# StarAgent 技能 ZIP 导入说明

本文档说明如何通过「工作区 → 技能 → 添加技能 → ZIP 导入」批量上传技能包。

## 基本要求

| 项 | 要求 |
|---|---|
| 格式 | 仅 `.zip` |
| 大小 | 非空，且 ≤ **50 MB** |
| 技能标识 | 以每个 `SKILL.md` **YAML frontmatter 的 `name`** 为准（不是 ZIP 文件名，也不是文件夹名） |
| 必填 frontmatter | `name`、`description` |
| 建议 | 文件夹名与 frontmatter `name` 保持一致（`snake_case`） |

导入时需在页面上选择一个**技能分类**，会应用到本次 ZIP 内所有成功导入的技能。

---

## 支持的压缩包结构

### 1. 多技能并列（推荐）

根目录直接放多个技能文件夹，一次上传可批量导入：

```text
my-skills.zip
├── skill_name1/
│   └── SKILL.md
├── skill_name2/
│   └── SKILL.md
└── skill_name3/
    ├── SKILL.md
    ├── references/
    └── scripts/
```

### 2. 单技能

```text
my-skill.zip
└── skill_name/
    ├── SKILL.md
    ├── references/
    │   └── guide.md
    ├── scripts/
    │   └── run.py
    └── config.env.example
```

### 3. 带 `skills/` 前缀

顶层目录名必须是 `skills`，其下再放各技能目录：

```text
bundle.zip
└── skills/
    ├── skill_name1/
    │   └── SKILL.md
    └── skill_name2/
        └── SKILL.md
```

### 4. 两种前缀可混用（同一 ZIP 内）

```text
mixed.zip
├── skill_a/
│   └── SKILL.md
└── skills/
    └── skill_b/
        └── SKILL.md
```

### 5. 每个技能目录内可带的文件

| 路径 | 说明 |
|------|------|
| `SKILL.md` | **必需** |
| `references/**` | 参考文档，支持子目录 |
| `scripts/**` | 脚本文件，支持子目录 |
| 其他任意文件 | 落在技能根目录，如 `icon.png`、`config.env.example` |

示例：

```text
skill_name/
├── SKILL.md
├── references/
│   └── nested/
│       └── doc.md
├── scripts/
│   └── utils/
│       └── helper.py
└── assets/
    └── logo.png
```

---

## 不支持的压缩包结构

| 结构 | 原因 |
|------|------|
| 根目录只有 `SKILL.md`（无子目录） | 路径层级不足，会被跳过 |
| 多包一层无关父目录 | 如 `bundle/skill_a/SKILL.md`，系统找不到 `bundle/SKILL.md` |
| `skills/SKILL.md`（没有技能子目录） | 不符合 `skills/<name>/SKILL.md` 结构 |
| 绝对路径 / 盘符路径 / 含 `..` | 安全校验拒绝 |
| 同 ZIP 内多个 `SKILL.md` 的 frontmatter `name` 相同 | 报「压缩包内存在重复的技能名称」 |
| 某目录没有 `SKILL.md` | 该目录被跳过 |
| `SKILL.md` 缺少有效 `name` 或 `description` | 无法创建技能 |

### 常见踩坑

在资源管理器中选中多个技能文件夹后压缩，若 ZIP 内是：

```text
# 不支持
archive.zip
└── my-skills/          ← 多了一层
    ├── skill_a/
    └── skill_b/
```

应**直接压缩各技能文件夹本身**，或进入 `my-skills` 目录后再全选压缩：

```text
# 支持
archive.zip
├── skill_a/
└── skill_b/
```

---

## SKILL.md 示例

```yaml
---
name: my-skill
description: 简要说明这个技能适合在什么场景下使用
category: 开发工具
---

# 使用说明

在这里描述技能的使用方式、输入要求和注意事项。

# 工作流程

1. 理解用户目标
2. 调用必要工具
3. 输出结构化结果
```

> `category` 可在 ZIP 导入后由页面选择的分类写入；也可在 frontmatter 中预先填写。

---

## 导入结果说明

- **成功**：技能写入当前用户工作区的 `customized_skills/<name>/`，并在技能库中创建记录
- **同名已存在**：默认不覆盖，该技能导入失败，其余技能仍可成功
- **部分失败**：页面会提示成功数量与失败原因（如「已存在」）

---

## 本模板包内容

| 路径 | 说明 |
|------|------|
| `README.md` | 本说明（导入时会被忽略） |
| `example_skill/` | 示例技能目录，可参考后替换为你的技能 |

将 `example_skill` 复制多份、改名并修改 `SKILL.md` 后，与技能文件夹一起压缩即可批量导入。
