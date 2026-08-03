import unittest
import json
import os
import sys
from jsonschema import validate

class TestSkillSchemas(unittest.TestCase):
    """技能Schema验证测试类"""
    
    SKILLS_DIR = os.path.join(os.path.dirname(__file__), '..', 'skills')
    
    def load_schema(self, skill_id):
        """加载技能的schema.json文件"""
        schema_path = os.path.join(self.SKILLS_DIR, skill_id, 'schema.json')
        self.assertTrue(os.path.exists(schema_path), f"Schema文件不存在: {schema_path}")
        with open(schema_path, 'r', encoding='utf-8') as f:
            return json.load(f)
    
    def test_cmdb_server_query_schema(self):
        """测试CMDB服务器查询技能Schema"""
        schema = self.load_schema('cmdb-server-query')
        
        # 验证Schema结构
        self.assertIn('input', schema)
        self.assertIn('output', schema)
        self.assertIn('definitions', schema)
        
        # 验证输入参数
        input_schema = schema['input']
        self.assertIn('properties', input_schema)
        self.assertIn('node', input_schema['properties'])
        self.assertIn('ip', input_schema['properties'])
        self.assertIn('status', input_schema['properties'])
        
        # 验证输出结构
        output_schema = schema['output']
        self.assertIn('success', output_schema['properties'])
        self.assertIn('message', output_schema['properties'])
        self.assertIn('data', output_schema['properties'])
        
        # 验证示例输入符合Schema
        test_input = {
            "node": "云公司->贵州",
            "ip": "192.168.7.101",
            "page": 1,
            "pageSize": 50
        }
        validate(instance=test_input, schema=input_schema)
    
    def test_server_public_ip_query_schema(self):
        """测试服务器公网IP查询技能Schema"""
        schema = self.load_schema('server-public-ip-query')
        
        self.assertIn('input', schema)
        self.assertIn('output', schema)
        
        input_schema = schema['input']
        self.assertIn('node', input_schema['properties'])
        self.assertIn('publicIp', input_schema['properties'])
        
        # 验证示例输入
        test_input = {
            "node": "云公司->贵州",
            "publicIp": "113.12.13.14",
            "page": 1,
            "pageSize": 40
        }
        validate(instance=test_input, schema=input_schema)
    
    def test_product_query_schema(self):
        """测试产品查询技能Schema"""
        schema = self.load_schema('product-query')
        
        self.assertIn('input', schema)
        self.assertIn('output', schema)
        
        input_schema = schema['input']
        self.assertIn('id', input_schema['properties'])
        self.assertIn('name', input_schema['properties'])
        
        # 验证示例输入
        test_input = {
            "name": "规则引擎平台",
            "page": 1,
            "pageSize": 40
        }
        validate(instance=test_input, schema=input_schema)
    
    def test_project_deployment_query_schema(self):
        """测试项目部署信息查询技能Schema"""
        schema = self.load_schema('project-deployment-query')
        
        self.assertIn('input', schema)
        self.assertIn('output', schema)
        
        input_schema = schema['input']
        self.assertIn('projectName', input_schema['properties'])
        self.assertIn('environment', input_schema['properties'])
        self.assertIn('deploymentStatus', input_schema['properties'])
        
        # 验证示例输入
        test_input = {
            "projectName": "guizh-rules-api",
            "environment": "生产",
            "currentPage": 1,
            "pageSize": 100
        }
        validate(instance=test_input, schema=input_schema)
    
    def test_project_basis_query_schema(self):
        """测试工程项目信息查询技能Schema"""
        schema = self.load_schema('project-basis-query')
        
        self.assertIn('input', schema)
        self.assertIn('output', schema)
        
        input_schema = schema['input']
        self.assertIn('id', input_schema['properties'])
        self.assertIn('name', input_schema['properties'])
        self.assertIn('productId', input_schema['properties'])
        
        # 验证示例输入
        test_input = {
            "name": "tykj-kafka-test",
            "productName": "天翼看家",
            "page": 1,
            "pageSize": 40
        }
        validate(instance=test_input, schema=input_schema)

class TestParamsYaml(unittest.TestCase):
    """参数配置文件测试类"""
    
    SKILLS_DIR = os.path.join(os.path.dirname(__file__), '..', 'skills')
    
    def test_cmdb_server_query_params(self):
        """测试CMDB服务器查询参数配置"""
        params_path = os.path.join(self.SKILLS_DIR, 'cmdb-server-query', 'config', 'params.yaml')
        self.assertTrue(os.path.exists(params_path), f"参数配置文件不存在: {params_path}")
    
    def test_server_public_ip_query_params(self):
        """测试服务器公网IP查询参数配置"""
        params_path = os.path.join(self.SKILLS_DIR, 'server-public-ip-query', 'config', 'params.yaml')
        self.assertTrue(os.path.exists(params_path), f"参数配置文件不存在: {params_path}")
    
    def test_product_query_params(self):
        """测试产品查询参数配置"""
        params_path = os.path.join(self.SKILLS_DIR, 'product-query', 'config', 'params.yaml')
        self.assertTrue(os.path.exists(params_path), f"参数配置文件不存在: {params_path}")
    
    def test_project_deployment_query_params(self):
        """测试项目部署信息查询参数配置"""
        params_path = os.path.join(self.SKILLS_DIR, 'project-deployment-query', 'config', 'params.yaml')
        self.assertTrue(os.path.exists(params_path), f"参数配置文件不存在: {params_path}")
    
    def test_project_basis_query_params(self):
        """测试工程项目信息查询参数配置"""
        params_path = os.path.join(self.SKILLS_DIR, 'project-basis-query', 'config', 'params.yaml')
        self.assertTrue(os.path.exists(params_path), f"参数配置文件不存在: {params_path}")

class TestSkillRegistry(unittest.TestCase):
    """技能注册中心测试类"""
    
    def test_registry_skills_json(self):
        """测试技能注册中心JSON文件"""
        registry_path = os.path.join(os.path.dirname(__file__), '..', 'registry', 'skills.json')
        self.assertTrue(os.path.exists(registry_path), "技能注册中心文件不存在")
        
        with open(registry_path, 'r', encoding='utf-8') as f:
            registry = json.load(f)
        
        # 验证基本结构
        self.assertIn('version', registry)
        self.assertIn('skills', registry)
        self.assertIn('categories', registry)
        
        # 验证所有技能都已注册
        skill_ids = [skill['id'] for skill in registry['skills']]
        expected_skills = [
            'cmdb-server-query',
            'server-public-ip-query', 
            'product-query',
            'project-deployment-query',
            'project-basis-query'
        ]
        
        for expected in expected_skills:
            self.assertIn(expected, skill_ids, f"技能 {expected} 未在注册中心中")
    
    def test_registry_skill_structure(self):
        """测试注册中心中技能结构的完整性"""
        registry_path = os.path.join(os.path.dirname(__file__), '..', 'registry', 'skills.json')
        with open(registry_path, 'r', encoding='utf-8') as f:
            registry = json.load(f)
        
        for skill in registry['skills']:
            # 验证必需字段
            self.assertIn('id', skill, f"技能缺少 id 字段: {skill.get('name')}")
            self.assertIn('name', skill, f"技能缺少 name 字段: {skill.get('id')}")
            self.assertIn('description', skill, f"技能缺少 description 字段: {skill.get('id')}")
            self.assertIn('version', skill, f"技能缺少 version 字段: {skill.get('id')}")
            self.assertIn('enabled', skill, f"技能缺少 enabled 字段: {skill.get('id')}")
            self.assertIn('path', skill, f"技能缺少 path 字段: {skill.get('id')}")
            self.assertIn('schemaPath', skill, f"技能缺少 schemaPath 字段: {skill.get('id')}")
            
            # 验证参数结构
            if 'parameters' in skill:
                for param in skill['parameters']:
                    self.assertIn('name', param, f"参数缺少 name 字段")
                    self.assertIn('type', param, f"参数 {param.get('name')} 缺少 type 字段")
                    self.assertIn('label', param, f"参数 {param.get('name')} 缺少 label 字段")

class TestSkillIntegration(unittest.TestCase):
    """技能集成测试类"""
    
    SKILLS_DIR = os.path.join(os.path.dirname(__file__), '..', 'skills')
    
    def test_skill_directory_structure(self):
        """测试技能目录结构完整性"""
        skills = [
            'cmdb-server-query',
            'server-public-ip-query',
            'product-query',
            'project-deployment-query',
            'project-basis-query'
        ]
        
        for skill in skills:
            skill_dir = os.path.join(self.SKILLS_DIR, skill)
            self.assertTrue(os.path.isdir(skill_dir), f"技能目录不存在: {skill_dir}")
            
            # 验证必需文件
            required_files = [
                'SKILL.md',
                'schema.json',
                'config/params.yaml'
            ]
            
            for required_file in required_files:
                file_path = os.path.join(skill_dir, required_file)
                self.assertTrue(os.path.exists(file_path), f"必需文件不存在: {file_path}")
    
    def test_skill_md_exists(self):
        """测试所有技能的SKILL.md文档存在"""
        skills = [
            'cmdb-server-query',
            'server-public-ip-query',
            'product-query',
            'project-deployment-query',
            'project-basis-query'
        ]
        
        for skill in skills:
            md_path = os.path.join(self.SKILLS_DIR, skill, 'SKILL.md')
            self.assertTrue(os.path.exists(md_path), f"SKILL.md不存在: {md_path}")
            
            # 验证文档内容包含必需章节
            with open(md_path, 'r', encoding='utf-8') as f:
                content = f.read()
            
            self.assertIn('## 1. When to Activate', content, f"SKILL.md缺少触发条件章节: {skill}")
            self.assertIn('## 2. How It Works', content, f"SKILL.md缺少执行流程章节: {skill}")
            self.assertIn('## 3. Examples', content, f"SKILL.md缺少示例章节: {skill}")

if __name__ == '__main__':
    unittest.main(verbosity=2)