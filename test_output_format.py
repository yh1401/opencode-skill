#!/usr/bin/env python3
"""
测试 ops-data-query 输出格式化功能
验证：获取数据后，按照 output_templates.md 格式化输出
"""

import json

def format_server_output(api_response, user_query):
    """
    格式化服务器查询输出
    """
    if api_response.get('code') != 200:
        return f"查询失败: {api_response.get('message')}"

    data = api_response.get('data', {})
    records = data.get('records', [])
    total = data.get('total', 0)
    current_page = data.get('currentPage', 1)
    page_size = data.get('pageSize', 10)
    show_count = len(records)

    # 如果没有记录
    if not records:
        return f"## CMDB 服务器查询结果\n\n**查询条件**: {user_query}\n\n**结果摘要**: 未查询到符合条件的记录"

    # 构建表格
    table_header = "| 主机名 | IP | 机房 | 状态 | 类型 | CPU | 内存 | 产品 |\n|--------|-----|------|------|------|-----|------|------|"

    # 状态映射
    state_map = {
        '0': '🟢 在线',
        '1': '🟡 库存',
        '2': '🟠 计划上线',
        '3': '🔧 维修中',
        '4': '⚫ 已报废'
    }

    table_rows = []
    for record in records:
        host_name = record.get('hostName', '-')
        ip = record.get('ip', '-')
        node = record.get('node', '-')
        state = state_map.get(record.get('state'), record.get('state', '-'))
        server_type = record.get('serverType', '-')
        cpu_cores = record.get('cpuCores', '-')
        memory = record.get('memory', '')
        product_name = record.get('productName', '-')

        # 格式化内存
        if memory and not memory.endswith('GB'):
            memory = f"{memory}GB"

        row = f"| {host_name} | {ip} | {node} | {state} | {server_type} | {cpu_cores}核 | {memory} | {product_name} |"
        table_rows.append(row)

    # 构建完整输出
    output = f"""## CMDB 服务器查询结果

**查询条件**: {user_query}
**匹配技能**: cmdb-server-query
**查询范围**: node: 云公司->贵州

---

**结果摘要**: 共查询到 {total} 条记录，当前显示前 {show_count} 条

---

{table_header}
{chr(10).join(table_rows)}

---

💡 您可以说：
- "查看详细信息" - 显示更多字段
- "下一页" - 查看更多结果
- "查看全部" - 显示所有结果
"""

    return output


def test_with_mock_data():
    """使用 Mock 数据测试格式化"""
    # 读取 Mock 数据
    with open('/Users/a666/Documents/trae_projects/opencode_skill/mcp-server/references/mock_responses.json', 'r') as f:
        mock_data = json.load(f)

    server_data = mock_data['cmdb-server-query']

    # 模拟 API 响应
    api_response = {
        "code": 200,
        "message": "success",
        "fail": False,
        "data": {
            "records": server_data['data']['records'],
            "total": 2951,
            "currentPage": 1,
            "pageSize": 10,
            "pages": 296
        }
    }

    user_query = "贵州机房的服务器"

    # 格式化输出
    formatted_output = format_server_output(api_response, user_query)
    print(formatted_output)

    return formatted_output


if __name__ == "__main__":
    print("=" * 60)
    print("测试 ops-data-query 输出格式化功能")
    print("=" * 60)
    print()

    result = test_with_mock_data()

    # 验证关键内容是否存在
    checks = {
        "表格头部": "| 主机名 | IP | 机房 | 状态 | 类型 | CPU | 内存 | 产品 |" in result,
        "结果摘要": f"共查询到 2951 条记录" in result,
        "显示数量": f"当前显示前 4 条" in result,
        "主机名": "| gz-server-01 |" in result,
        "IP地址": "| 192.168.7.101 |" in result,
        "状态图标": "🟢 在线" in result,
        "操作引导": "💡 您可以说" in result
    }

    print()
    print("=" * 60)
    print("验证结果:")
    print("=" * 60)
    for check, passed in checks.items():
        status = "✅" if passed else "❌"
        print(f"{status} {check}: {passed}")

    all_passed = all(checks.values())
    print()
    if all_passed:
        print("🎉 所有检查通过！格式化功能正常。")
    else:
        print("⚠️  部分检查失败，需要修复格式化逻辑。")