import unittest
import json
import os


class TestOutputFormat(unittest.TestCase):
    """输出格式测试类"""

    SKILL_NAMES = {
        "cmdb-server-query": "CMDB服务器查询",
        "server-public-ip-query": "服务器公网IP查询",
        "project-deployment-query": "项目部署查询",
        "product-query": "产品查询",
        "project-basis-query": "项目基础信息查询"
    }

    def load_test_cases(self):
        """加载测试用例"""
        test_cases_path = os.path.join(os.path.dirname(__file__), 'fixtures', 'test_cases.json')
        with open(test_cases_path, 'r', encoding='utf-8') as f:
            return json.load(f)

    def generate_output(self, skill_id, user_input, api_response):
        """生成标准输出格式"""
        skill_name = self.SKILL_NAMES.get(skill_id, skill_id)
        records = api_response.get('data', {}).get('records', [])
        total = api_response.get('data', {}).get('total', 0)
        current_page = api_response.get('data', {}).get('currentPage', 1)
        page_size = api_response.get('data', {}).get('pageSize', 15)
        
        output = f"## {skill_name}查询结果\n\n"
        output += f"**查询条件**：{user_input}\n\n"
        output += f"**匹配技能**：{skill_name}\n\n"
        
        params = {}
        if skill_id == 'cmdb-server-query':
            params['node'] = '云公司->贵州'
            params['pageSize'] = page_size
        output += f"**查询参数**：{json.dumps(params, ensure_ascii=False)}\n\n"
        
        output += "---\n\n"
        
        if total == 0:
            output += "**结果摘要**：未查询到符合条件的记录\n\n"
        elif total > 50:
            output += f"**结果摘要**：共查询到 {total} 条记录，显示前 20 条\n\n"
        else:
            output += f"**结果摘要**：共查询到 {total} 条记录\n\n"
        
        output += "---\n\n"
        
        if total > 0 and total <= 50:
            if skill_id == 'cmdb-server-query':
                output += "| 主机名 | IP地址 | 机房 | 状态 | 类型 | CPU | 内存 |\n"
                output += "|--------|--------|------|------|------|-----|------|\n"
                for record in records[:10]:
                    output += f"| {record.get('hostName', '')} | {record.get('ip', '')} | {record.get('node', '')} | {record.get('state', '')} | {record.get('serverType', '')} | {record.get('cpuCores', '')}核 | {record.get('memory', '')}GB |\n"
        
        if total > 50:
            output += "**导航提示**：\n"
            output += f"- 当前显示第 {current_page} 页，共 {((total - 1) // 20) + 1} 页\n"
            output += "- 如需查看更多，可添加筛选条件\n\n"
            output += "**筛选建议**：\n"
            output += "- 添加状态筛选：\"贵州机房的在线服务器\"\n"
            output += "- 添加类型筛选：\"贵州机房的物理机\"\n"
        
        output += "---\n\n"
        output += "**说明**：数据来源于 CMDB 系统\n"
        
        return output

    def test_output_format_cases(self):
        """测试输出格式"""
        test_cases = self.load_test_cases()['output_format_test_cases']
        
        for case in test_cases:
            with self.subTest(name=case['name']):
                output = self.generate_output(case['skill'], case['input'], case['api_response'])
                
                for expected_text in case['expected_output_contains']:
                    self.assertIn(expected_text, output,
                               f"输出缺少 '{expected_text}'")

    def test_output_structure(self):
        """测试输出结构完整性"""
        output = self.generate_output(
            'cmdb-server-query',
            '测试',
            {'code': 200, 'message': 'success', 'data': {'records': [], 'total': 0}}
        )
        
        required_sections = [
            '##',
            '查询条件',
            '匹配技能',
            '查询参数',
            '结果摘要',
            '说明'
        ]
        
        for section in required_sections:
            self.assertIn(section, output, f"输出缺少必要部分: {section}")

    def test_large_data_output(self):
        """测试大数据量输出"""
        output = self.generate_output(
            'cmdb-server-query',
            '查询所有',
            {'code': 200, 'message': 'success', 'data': {'records': [], 'total': 256}}
        )
        
        self.assertIn('显示前 20 条', output)
        self.assertIn('导航提示', output)
        self.assertIn('筛选建议', output)


if __name__ == '__main__':
    unittest.main()
