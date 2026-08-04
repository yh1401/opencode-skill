#!/usr/bin/env python3
"""
MCP 服务器测试客户端
用于测试 HTTP API 调用
"""

import json
import sys

try:
    import requests
except ImportError:
    print("错误: 未安装 requests 模块")
    sys.exit(1)


def test_server_info(base_url="http://localhost:8061"):
    """测试服务器信息接口"""
    print("📋 测试服务器信息接口...")
    try:
        resp = requests.get(f"{base_url}/", timeout=5)
        data = resp.json()
        print(f"✅ 服务名称: {data.get('service')}")
        print(f"✅ 版本: {data.get('version')}")
        print(f"✅ 工具列表: {', '.join(data.get('tools', []))}")
        return True
    except Exception as e:
        print(f"❌ 失败: {e}")
        return False


def test_cmdb_server_query(base_url="http://localhost:8061"):
    """测试 CMDB 服务器查询"""
    print("\n📋 测试 CMDB 服务器查询...")
    try:
        resp = requests.post(
            f"{base_url}/api/cmdb-server-query",
            json={"ip": "192.168.7.101", "currentPage": 1, "pageSize": 5},
            timeout=10
        )
        data = resp.json()
        if data.get("code") == 200:
            records = data.get("data", {}).get("records", [])
            print(f"✅ 查询成功，返回 {len(records)} 条记录")
            if records:
                print(f"   示例: {records[0].get('hostName')} ({records[0].get('ip')})")
            return True
        else:
            print(f"❌ API 返回错误: {data.get('message')}")
            return False
    except Exception as e:
        print(f"❌ 请求失败: {e}")
        return False


def test_public_ip_query(base_url="http://localhost:8061"):
    """测试公网 IP 查询"""
    print("\n📋 测试公网 IP 查询...")
    try:
        resp = requests.post(
            f"{base_url}/api/server-public-ip-query",
            json={"ip": "192.168.7.101"},
            timeout=10
        )
        data = resp.json()
        if data.get("code") == 200:
            records = data.get("data", {}).get("records", [])
            print(f"✅ 查询成功，返回 {len(records)} 条记录")
            return True
        else:
            print(f"❌ API 返回错误: {data.get('message')}")
            return False
    except Exception as e:
        print(f"❌ 请求失败: {e}")
        return False


def test_deployment_query(base_url="http://localhost:8061"):
    """测试部署记录查询"""
    print("\n📋 测试部署记录查询...")
    try:
        resp = requests.post(
            f"{base_url}/api/project-deployment-query",
            json={"projectName": "guizh-rules-api"},
            timeout=10
        )
        data = resp.json()
        if data.get("code") == 200:
            records = data.get("data", {}).get("records", [])
            print(f"✅ 查询成功，返回 {len(records)} 条记录")
            return True
        else:
            print(f"❌ API 返回错误: {data.get('message')}")
            return False
    except Exception as e:
        print(f"❌ 请求失败: {e}")
        return False


def test_product_query(base_url="http://localhost:8061"):
    """测试产品信息查询"""
    print("\n📋 测试产品信息查询...")
    try:
        resp = requests.post(
            f"{base_url}/api/product-query",
            json={"productName": "规则引擎"},
            timeout=10
        )
        data = resp.json()
        if data.get("code") == 200:
            records = data.get("data", {}).get("records", [])
            print(f"✅ 查询成功，返回 {len(records)} 条记录")
            return True
        else:
            print(f"❌ API 返回错误: {data.get('message')}")
            return False
    except Exception as e:
        print(f"❌ 请求失败: {e}")
        return False


def test_project_basis_query(base_url="http://localhost:8061"):
    """测试项目基础信息查询"""
    print("\n📋 测试项目基础信息查询...")
    try:
        resp = requests.post(
            f"{base_url}/api/project-basis-query",
            json={"projectName": "guizh-rules-api"},
            timeout=10
        )
        data = resp.json()
        if data.get("code") == 200:
            records = data.get("data", {}).get("records", [])
            print(f"✅ 查询成功，返回 {len(records)} 条记录")
            return True
        else:
            print(f"❌ API 返回错误: {data.get('message')}")
            return False
    except Exception as e:
        print(f"❌ 请求失败: {e}")
        return False


def main():
    base_url = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8061"
    
    print("=" * 60)
    print("  MCP 服务器测试客户端")
    print(f"  服务器地址: {base_url}")
    print("=" * 60)
    
    tests = [
        test_server_info,
        test_cmdb_server_query,
        test_public_ip_query,
        test_deployment_query,
        test_product_query,
        test_project_basis_query,
    ]
    
    results = []
    for test in tests:
        result = test(base_url)
        results.append(result)
    
    print("\n" + "=" * 60)
    print("  测试结果汇总")
    print("=" * 60)
    passed = sum(results)
    total = len(results)
    print(f"✅ 通过: {passed}/{total}")
    print(f"❌ 失败: {total - passed}/{total}")
    
    if passed == total:
        print("\n🎉 所有测试通过！")
        return 0
    else:
        print(f"\n⚠️  {total - passed} 个测试失败")
        return 1


if __name__ == "__main__":
    sys.exit(main())