用户要求使用技能查询贵州机房的服务器，需要先用 remote_read_file 读取 ops-data-query 技能的 SKILL.md 文件，了解如何使用该技能。

Let me check the actual path of the skill.

好的，我来仔细分析用户的需求和当前情况：

用户要求：
1. 使用技能查询贵州机房的服务器
2. 不要使用 mock 数据
3. 直接使用 API 调用
4. 展示前 10 条在回答里面

当前情况：
1. 我是"运维数据查询助手-v2"，拥有 ops-data-query 技能
2. 技能描述说这是"企业 CMDB 运维数据综合查询技能"
3. 但我无法直接访问技能的 SKILL.md 文件来了解具体的 API 调用方式

根据系统提示，技能应该在 `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md`，但文件不存在。

我需要：
1. 先查看技能目录下有什么文件
2. 了解如何调用 API 查询服务器数据
3. 查询贵州机房的服务器
4. 展示前 10 条结果

让我先查看技能目录的内容。

The path seems to be resolved differently. Let me try to find the SKILL.md file.

让我尝试从根目录开始查找。

The skill file doesn't exist in the workspace. But the system prompt mentions the skill is enabled. Let me check if there's a way to access the skill definition. The prompt says to check the SKILL.md at "/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md" but that path doesn't seem to exist in the remote workspace. Let me try using local_read_file since the instructions say local_* for bound directories.

Actually, looking more carefully at the tool hints:
- **远端服务器工作区**（`files/sessions/`、`my_documents/`、`git_docs/`、数字员工根）：用 **`remote_*`**。

The skill is at `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md` which is in the digital employee's workspace root. The remote_* tools use relative paths from the working directory which is `files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/20260629135551/`.

Let me try to read from the root. The AGENTS.md says `remote_list_dir` default `.` is the session output directory. To see root, pass `files`, `sessions` etc.

But the skill is at `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md`. The working_dir is `/data/workspace/AI1060_workspace/files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/20260629135551/`.

So relative to working_dir, the skill would be at `../../../../../../active_skills/ops-data-query/SKILL.md` - that seems wrong.

Wait, let me re-read: "根目录=上方「工作目录」（当前数字员工或信任工作区根）" - the working directory IS `/data/workspace/AI1060_workspace`. But then it also says "本 Agent 工作目录（裸相对路径默认写入）: `files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/2020260629135551/`"

Actually the remote_read_file takes `file_path` which could be absolute. Let me try with the absolute path since the error showed it tried the absolute path.

Actually looking at the error: "The file /data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md does not exist." - it seems the file literally doesn't exist at that path.

Let me try local_read_file since the instructions mention local_* for bound directories on the desktop client.

Actually, let me look at this differently. The skill_authoring skill path is `/usr/local/lib/python3.13/site-packages/staragent/agents/skills/skill_authoring/SKILL.md` which is a system path. The ops-data-query skill path is `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md`.

Maybe I should try local_read_file for this:

The user wants to query servers in the Guizhou datacenter using the ops-data-query skill. The skill file seems to be at a system path based on the skill description. Let me try to read it from the path mentioned in the skill listing.

The skill says: Check "/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md" but that doesn't seem to exist in the remote workspace. However, the skill_authoring skill mentions a path at "/usr/local/lib/python3.13/site-packages/staragent/agents/skills/skill_authoring/SKILL.md".

Let me try to read the ops-data-query skill from the system path or find it. The remote tools seem to be working on the session directory. Let me look for the skill in the workspace root.

The skill file is supposed to be at `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md`, but the remote tools are working relative to the session directory. Let me try to find the skill using the path hints from the system prompt.

The user wants me to use the ops-data-query skill to query servers in the Guizhou data center. The skill is listed as an active skill but I can't find the SKILL.md file in my workspace. Let me try to access it through the system path mentioned in the skills list, or look for it in the workspace more carefully.

Let me check if there's a way to access the skill. The skill description says:
"企业 CMDB 运维数据综合查询技能。提供服务器查询、公网IP查询、部署记录查询、产品信息查询和工程项目基础信息查询五大功能，支持跨维度关联查询和智能路由。"

The SKILL.md should be at `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md` but remote_read_file failed for that path. This is because the remote_* tools work relative to the session working directory. Let me check if there's something I can do.

Actually, looking at the tools more carefully, the `remote_read_file` takes a `file_path` parameter. The working directory is `/data/workspace/AI1060_workspace`. Let me try with the absolute path or find the skill another way.

Wait, let me re-read the tool hint:
- 远端服务器工作区（`files/sessions/`、`my_documents/`、`git_docs/`、数字员工根）：用 **`remote_*`**。

The root is `/data/workspace/AI1060_workspace`. But the skill is at `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md`. 

I notice the `remote_list_dir` for `active_skills` returned an entry, but when I tried to list the contents of `active_skills/ops-data-query`, it gave an error about a path that doesn't exist. This suggests the directory structure might be different than expected, or there's an issue with how the paths are being resolved. I'm running into path resolution issues—the skill directory appears in the root listing but then seems to redirect to a non-existent path when I try to access it. Since the skill description mentions API calls for querying server and deployment data, let me search for any Python scripts or configuration files that might contain the actual API endpoints I need.

I need to find the ops-data-query skill. According to the instructions, it says to check "/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md". Let me try to read it using remote_read_file with the workspace path. The working directory seems to be the session directory, not the root. Let me check how the paths work.

The skill path is `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md` - but my current working directory for remote_* tools is `files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/20260629135551/`.

So I need to use a relative path from the workspace root. Let me try accessing it via the workspace root.

Actually, looking at the AGENTS.md: "根目录=上方「工作目录」（当前数字员工或信任工作区根）" - the working_dir is `/data/workspace/AI1060_workspace`. But the tool hint says: "本轮 Agent 工作目录（裸相对路径默认写入）: `files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/20260629135551/`"

So the remote_* tools seem to be relative to `/data/workspace/AI1060_workspace/files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/20260629135551/`, not the workspace root.

Let me try to navigate up to the workspace root. The workspace root is `/data/workspace/AI1060_workspace`, and the current session dir is `/data/workspace/AI1060_workspace/files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/20260629135551/`.

So to get to the root, I'd need `../../../../../..` from the session dir... that's `files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/20260629135551/` -> going up 5 levels.

Let me try with `remote_list_dir` using relative path to go up.

The active_skills directory seems to be at the workspace root level, but the remote_* tools seem to be resolving relative paths from the current session directory. Let me try using the absolute path approach or find another way to access the SKILL.md file.

Looking at the skill info in the system prompt:
```
## ops-data-query
企业 CMDB 运维数据综合查询技能。提供服务器查询、公网IP查询、部署记录查询、产品信息查询和工程项目基础信息查询五大功能，支持跨维度关联查询和智能路由。
Check "/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md" for how to use this skill
```

The path is `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md`. The workspace root is `/data/workspace/AI1060_workspace`. So the relative path from workspace root would be `active_skills/ops-data-query/SKILL.md`.

But when I tried `remote_list_dir(relative_path="active_skills")`, it gave an error about the customized_skills path. This is strange.

Let me try `remote_stat_path` first.

技能文件在 `active_skills/ops-data-query/SKILL.md` 路径下，但这个路径是绝对路径 `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md`。当前工作目录是 `/data/workspace/AI1060_workspace/files/sessions/9b911b08-6c9c-4c82-8016-710a63fabc99/20260629135551/`，所以相对路径无法访问 `active_skills`。

让我用代码库搜索来查找技能内容。

让我查看技能文档的提示路径。根据系统提示，技能文档在 `/data/workspace/AI1060_workspace/active_skills/ops-data-query/SKILL.md`。但我的当前工作目录是会话目录，无法直接访问该路径。让我尝试通过其他方式读取。

我需要先创建脚本来读取技能文件。让我先写入脚本文件：

The LSP diagnostics show errors about `/usr` being outside workspace for the import and os.path operations, but these are just false positives since the script is designed to read from system paths. The script itself is valid Python. Let me run it to find the SKILL.md file.

用户要求查询贵州机房的服务器，不使用mock数据，直接使用API调用。我需要查看cmdb-server-query子技能的SKILL.md来了解具体的API调用方式。技能文档已读取。现在让我查看 cmdb-server-query子技能的详细文档，了解具体的 API调用方式：

Let me run this script to read the cmdb-server-query sub-skill documentation.

用户要求查询贵州机房的服务器，并使用真实的 API 调用，不使用 mock 数据。根据技能文档，我需要调用 `cmdb_server_query` 工具。

从技能文档中可以看到：
- API 端点：`POST https://oss.tech.ctseelink.cn/api/v2/cmdbServer/getCmdbServerPageList`
- 机房映射：贵州 → "云公司->贵州"
- 工具名：`cmdb_server_query`

但是我注意到这是一个技能描述，实际的工具调用应该通过系统注册的工具来完成。让我检查一下是否有 `cmdb_server_query` 这个工具可用。

看起来我没有直接的 `cmdb_server_query` 工具。

我需要查看技能目录下的配置文件，了解如何实际调用这个 API。

我需要读取 API 端点配置文件来了解具体的 API 调用方式。让我先运行这个脚本查看配置。

用户要求使用技能查询贵州机房的服务器，不使用 mock 数据，直接调用 API。我已经读取了技能文档，了解了 API 的调用方式。现在让我执行查询脚本。

API 调用成功，拿到了贵州机房 2951 台服务器的真实数据，当前页返回 10 条。现在让我整理成清晰的表格展示给用户。