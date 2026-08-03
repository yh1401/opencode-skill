import unittest
import json
import os


class TestParamExtraction(unittest.TestCase):
    """参数提取测试类"""

    NODE_MAPPING = {
        "贵州": "云公司->贵州",
        "贵阳": "云公司->贵州",
        "贵州机房": "云公司->贵州",
        "北京": "云公司->北京",
        "北京机房": "云公司->北京",
        "上海": "省公司->上海",
        "上海机房": "省公司->上海",
        "广州": "云公司->广州",
        "广州机房": "云公司->广州"
    }

    STATE_MAPPING = {
        "在线": "0",
        "库存": "1",
        "计划上线": "2",
        "维修中": "3",
        "已报废": "4",
        "待分配": "5",
        "待清退": "6"
    }

    ENV_MAPPING = {
        "测试": 1,
        "灰度": 2,
        "生产": 3,
        "研发": 4
    }

    TYPE_MAPPING = {
        "物理机": "0",
        "虚拟机": "1",
        "第三方云机": "2"
    }

    def load_test_cases(self):
        """加载测试用例"""
        test_cases_path = os.path.join(os.path.dirname(__file__), 'fixtures', 'test_cases.json')
        with open(test_cases_path, 'r', encoding='utf-8') as f:
            return json.load(f)

    def extract_node(self, input_text):
        """提取机房位置"""
        for alias, standard in self.NODE_MAPPING.items():
            if alias in input_text:
                return standard
        return None

    def extract_state(self, input_text):
        """提取状态"""
        for alias, standard in self.STATE_MAPPING.items():
            if alias in input_text:
                return standard
        return "0"

    def extract_environment(self, input_text):
        """提取环境"""
        for alias, standard in self.ENV_MAPPING.items():
            if alias in input_text:
                return standard
        return None

    def extract_type(self, input_text):
        """提取服务器类型"""
        for alias, standard in self.TYPE_MAPPING.items():
            if alias in input_text:
                return standard
        return None

    def extract_count(self, input_text):
        """提取数量"""
        if "一台" in input_text or "一个" in input_text:
            return 1
        if "几台" in input_text or "一些" in input_text:
            return 15
        if "所有" in input_text or "全部" in input_text:
            return 100
        return 15

    def extract_cmdb_params(self, input_text):
        """提取CMDB查询参数"""
        params = {
            "currentPage": 1,
            "pageSize": self.extract_count(input_text)
        }
        
        node = self.extract_node(input_text)
        if node:
            params["node"] = node
        
        state = self.extract_state(input_text)
        if state:
            params["state"] = state
        
        server_type = self.extract_type(input_text)
        if server_type:
            params["type"] = server_type
        
        if "主机名包含" in input_text or "主机名" in input_text:
            import re
            match = re.search(r'主机名(?:包含)?\s*(\S+)', input_text)
            if match:
                params["hostName"] = match.group(1)
        
        return params

    def test_param_extraction_cases(self):
        """测试参数提取"""
        test_cases = self.load_test_cases()['param_extraction_test_cases']
        
        for case in test_cases:
            with self.subTest(name=case['name']):
                if case['skill'] == 'cmdb-server-query':
                    result = self.extract_cmdb_params(case['input'])
                    expected = case['expected_params']
                    
                    for key in expected:
                        self.assertIn(key, result,
                                   f"输入: '{case['input']}' 缺少参数: {key}")
                        self.assertEqual(result[key], expected[key],
                                       f"输入: '{case['input']}' 参数 {key} 不匹配: {result[key]} != {expected[key]}")

    def test_node_extraction(self):
        """测试机房位置提取"""
        cases = [
            ("贵州机房的服务器", "云公司->贵州"),
            ("北京的主机", "云公司->北京"),
            ("上海的数据", "省公司->上海"),
            ("广州机房", "云公司->广州")
        ]
        
        for input_text, expected in cases:
            with self.subTest(input=input_text):
                result = self.extract_node(input_text)
                self.assertEqual(result, expected)

    def test_state_extraction(self):
        """测试状态提取"""
        cases = [
            ("在线服务器", "0"),
            ("库存机器", "1"),
            ("维修中的主机", "3")
        ]
        
        for input_text, expected in cases:
            with self.subTest(input=input_text):
                result = self.extract_state(input_text)
                self.assertEqual(result, expected)

    def test_count_extraction(self):
        """测试数量提取"""
        cases = [
            ("一台服务器", 1),
            ("几台机器", 15),
            ("所有服务器", 100),
            ("服务器", 15)
        ]
        
        for input_text, expected in cases:
            with self.subTest(input=input_text):
                result = self.extract_count(input_text)
                self.assertEqual(result, expected)


if __name__ == '__main__':
    unittest.main()
