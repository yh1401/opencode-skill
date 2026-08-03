import unittest
import json
import os


class TestSkillRouting(unittest.TestCase):
    """技能路由测试类"""

    SKILL_PRIORITY = {
        "server-public-ip-query": 1,
        "project-deployment-query": 2,
        "product-query": 3,
        "project-basis-query": 4,
        "cmdb-server-query": 5
    }

    EXCLUSIVE_KEYWORDS = {
        "cmdb-server-query": ["公网IP", "外网", "带宽", "部署", "发布", "上线", "SVN", "GIT"],
        "server-public-ip-query": [],
        "project-deployment-query": [],
        "product-query": [],
        "project-basis-query": []
    }

    KEYWORDS_MAP = {
        "cmdb-server-query": ["服务器", "主机", "机房", "IP", "配置", "硬件", "CPU", "内存"],
        "server-public-ip-query": ["公网IP", "外网IP", "带宽", "出口IP"],
        "project-deployment-query": ["部署", "上线", "发布", "版本", "部署记录"],
        "product-query": ["产品", "产品线", "产品ID", "产品名称"],
        "project-basis-query": ["项目", "代码", "仓库", "SVN", "GIT", "工程项目"]
    }

    def load_test_cases(self):
        """加载测试用例"""
        test_cases_path = os.path.join(os.path.dirname(__file__), 'fixtures', 'test_cases.json')
        with open(test_cases_path, 'r', encoding='utf-8') as f:
            return json.load(f)

    def match_skill(self, user_input):
        """根据用户输入匹配技能（模拟路由逻辑）"""
        matched_skills = []
        
        for skill_id, keywords in self.KEYWORDS_MAP.items():
            for keyword in keywords:
                if keyword in user_input:
                    matched_skills.append((skill_id, self.SKILL_PRIORITY[skill_id]))
                    break
        
        if not matched_skills:
            return None
        
        matched_skills.sort(key=lambda x: x[1])
        
        for skill_id, _ in matched_skills:
            exclusive = self.EXCLUSIVE_KEYWORDS.get(skill_id, [])
            has_exclusive = any(kw in user_input for kw in exclusive)
            if not has_exclusive:
                return skill_id
        
        return matched_skills[0][0]

    def test_routing_cases(self):
        """测试路由匹配"""
        test_cases = self.load_test_cases()['routing_test_cases']
        
        for case in test_cases:
            with self.subTest(name=case['name']):
                result = self.match_skill(case['input'])
                self.assertEqual(result, case['expected_skill'],
                               f"输入: '{case['input']}' 期望: {case['expected_skill']}, 实际: {result}")

    def test_priority_order(self):
        """测试优先级排序"""
        cases = [
            ("查询服务器的公网IP", "server-public-ip-query"),
            ("查询项目部署的服务器", "project-deployment-query"),
            ("查询产品的服务器", "cmdb-server-query")
        ]
        
        for input_text, expected in cases:
            with self.subTest(input=input_text):
                result = self.match_skill(input_text)
                self.assertEqual(result, expected)

    def test_ambiguous_input(self):
        """测试模糊输入"""
        result = self.match_skill("查询一下")
        self.assertIsNone(result)


if __name__ == '__main__':
    unittest.main()
