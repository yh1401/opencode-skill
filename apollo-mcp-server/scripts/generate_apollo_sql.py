#!/usr/bin/env python3
"""
Apollo Mock 数据 → SQL 转换脚本

将 references/mock_responses.json 中的 Mock 数据转换为 Apollo 数据库 SQL 脚本。
生成的 SQL 可直接导入 ApolloConfigDB 和 ApolloPortalDB。

使用方法:
    python3 scripts/generate_apollo_sql.py
"""

import json
import os
import sys
from datetime import datetime

_script_dir = os.path.dirname(os.path.abspath(__file__))
_root_dir = os.path.dirname(_script_dir)
_references_dir = os.path.join(_root_dir, 'references')


def escape_sql(value):
    """转义 SQL 字符串"""
    return str(value).replace("'", "\\'")


def load_mock_data():
    """加载 Mock 数据"""
    mock_path = os.path.join(_references_dir, 'mock_responses.json')
    if not os.path.exists(mock_path):
        print(f"错误: Mock 数据文件不存在: {mock_path}")
        sys.exit(1)
    with open(mock_path, 'r', encoding='utf-8') as f:
        return json.load(f)


def generate_configdb_sql(mock_data):
    """生成 ApolloConfigDB SQL"""
    lines = []
    lines.append("-- ===============================================")
    lines.append("-- Apollo ConfigDB - Mock 测试数据")
    lines.append("-- ===============================================")
    lines.append("")
    lines.append("USE ApolloConfigDB;")
    lines.append("")

    apps = mock_data.get('_apps', [])
    configs_data = mock_data.get('_configs', {})
    releases_data = mock_data.get('_releases', {})
    namespaces_data = mock_data.get('_namespaces', {})

    # ========== 1. App 表 ==========
    lines.append("-- ---------- App 应用表 ----------")
    lines.append("")
    app_id_map = {}
    for idx, app in enumerate(apps, start=1):
        app_id = idx
        app_id_map[app['appId']] = app_id
        lines.append(
            f"INSERT INTO `App` (`Id`, `AppId`, `Name`, `OrgId`, `OrgName`, "
            f"`OwnerName`, `OwnerEmail`, `IsDeleted`, `DeletedAt`, "
            f"`DataChange_CreatedBy`, `DataChange_CreatedTime`) VALUES "
            f"({app_id}, '{app['appId']}', '{escape_sql(app['name'])}', "
            f"'{app.get('orgId', f'org-{idx:03d}')}', '{escape_sql(app['orgName'])}', "
            f"'admin', 'admin@company.com', b'0', 0, 'admin', NOW());"
        )
    lines.append("")

    # ========== 2. Cluster 表 ==========
    lines.append("-- ---------- Cluster 集群表 ----------")
    lines.append("")
    cluster_id = 1
    cluster_id_map = {}
    for app in apps:
        cluster_name = 'default'
        key = (app['appId'], cluster_name)
        cluster_id_map[key] = cluster_id
        lines.append(
            f"INSERT INTO `Cluster` (`Id`, `Name`, `AppId`, `ParentClusterId`, `Comment`, "
            f"`IsDeleted`, `DeletedAt`, `DataChange_CreatedBy`, `DataChange_CreatedTime`) VALUES "
            f"({cluster_id}, '{cluster_name}', '{app['appId']}', 0, '默认集群', "
            f"b'0', 0, 'admin', NOW());"
        )
        cluster_id += 1
    lines.append("")

    # ========== 3. Namespace 表 ==========
    lines.append("-- ---------- Namespace 命名空间表 ----------")
    lines.append("")
    namespace_id = 1
    namespace_id_map = {}
    for app in apps:
        app_ns_list = namespaces_data.get(app['appId'], ['application'])
        for ns_name in app_ns_list:
            key = (app['appId'], 'default', ns_name)
            namespace_id_map[key] = namespace_id
            lines.append(
                f"INSERT INTO `Namespace` (`Id`, `AppId`, `ClusterName`, `NamespaceName`, "
                f"`IsDeleted`, `DeletedAt`, `DataChange_CreatedBy`, `DataChange_CreatedTime`) VALUES "
                f"({namespace_id}, '{app['appId']}', 'default', '{ns_name}', "
                f"b'0', 0, 'admin', NOW());"
            )
            namespace_id += 1
    lines.append("")

    # ========== 4. AppNamespace 表 ==========
    lines.append("-- ---------- AppNamespace 应用命名空间表 ----------")
    lines.append("")
    app_ns_id = 1
    for app in apps:
        app_ns_list = namespaces_data.get(app['appId'], ['application'])
        for ns_name in app_ns_list:
            lines.append(
                f"INSERT INTO `AppNamespace` (`Id`, `Name`, `AppId`, `Format`, `IsPublic`, "
                f"`Comment`, `IsDeleted`, `DeletedAt`, `DataChange_CreatedBy`, `DataChange_CreatedTime`) VALUES "
                f"({app_ns_id}, '{ns_name}', '{app['appId']}', 'properties', b'0', "
                f"'{ns_name}配置', b'0', 0, 'admin', NOW());"
            )
            app_ns_id += 1
    lines.append("")

    # ========== 5. Item 表 ==========
    lines.append("-- ---------- Item 配置项表 ----------")
    lines.append("")
    item_id = 1
    for app in apps:
        app_configs = configs_data.get(app['appId'], {})
        for ns_name, config_dict in app_configs.items():
            ns_key = (app['appId'], 'default', ns_name)
            ns_id = namespace_id_map.get(ns_key)
            if ns_id is None:
                print(f"  警告: 找不到 Namespace '{ns_name}' 对应的 ID，跳过 {app['appId']}")
                continue
            for key, value in config_dict.items():
                val_str = escape_sql(value)
                # 自动推断类型
                val_type = 0  # String
                if str(value).lower() in ('true', 'false'):
                    val_type = 2  # Boolean
                elif str(value).isdigit():
                    val_type = 1  # Number
                lines.append(
                    f"INSERT INTO `Item` (`Id`, `NamespaceId`, `Key`, `Type`, `Value`, "
                    f"`Comment`, `LineNum`, `IsDeleted`, `DeletedAt`, "
                    f"`DataChange_CreatedBy`, `DataChange_CreatedTime`) VALUES "
                    f"({item_id}, {ns_id}, '{escape_sql(key)}', {val_type}, '{val_str}', "
                    f"'', 0, b'0', 0, 'admin', NOW());"
                )
                item_id += 1
    lines.append("")

    # ========== 6. Release 表 ==========
    lines.append("-- ---------- Release 发布表 ----------")
    lines.append("")
    release_id = 1
    release_key_map = {}
    for app in apps:
        app_releases = releases_data.get(app['appId'], {})
        for ns_name, release_list in app_releases.items():
            for rel in release_list:
                release_key = f"{app['appId']}+default+{ns_name}+{rel['id']}"
                release_key_map[release_id] = release_key
                
                app_configs = configs_data.get(app['appId'], {})
                ns_configs = app_configs.get(ns_name, {})
                config_json = escape_sql(json.dumps(ns_configs, ensure_ascii=False))
                rel_title = escape_sql(rel['releaseTitle'])
                rel_comment = escape_sql(rel.get('releaseComment', ''))
                rel_by = escape_sql(rel.get('releasedBy', 'admin'))
                rel_time = rel.get('releaseTime', datetime.now().strftime('%Y-%m-%d %H:%M:%S'))
                
                lines.append(
                    f"INSERT INTO `Release` (`Id`, `ReleaseKey`, `Name`, `Comment`, "
                    f"`AppId`, `ClusterName`, `NamespaceName`, `Configurations`, "
                    f"`IsAbandoned`, `IsDeleted`, `DeletedAt`, "
                    f"`DataChange_CreatedBy`, `DataChange_CreatedTime`) VALUES "
                    f"({release_id}, '{release_key}', '{rel_title}', "
                    f"'{rel_comment}', "
                    f"'{app['appId']}', 'default', '{ns_name}', '{config_json}', "
                    f"b'0', b'0', 0, '{rel_by}', '{rel_time}');"
                )
                release_id += 1
    lines.append("")

    # ========== 7. ReleaseHistory 表 ==========
    lines.append("-- ---------- ReleaseHistory 发布历史表 ----------")
    lines.append("")
    history_id = 1
    for app in apps:
        app_releases = releases_data.get(app['appId'], {})
        for ns_name, release_list in app_releases.items():
            prev_id = 0
            for rel in release_list:
                release_key = f"{app['appId']}+default+{ns_name}+{rel['id']}"
                rel_id = 0
                for rid, rk in release_key_map.items():
                    if rk == release_key:
                        rel_id = rid
                        break
                
                op_context = escape_sql(json.dumps({
                    "releaseId": rel_id,
                    "releaseTitle": rel['releaseTitle'],
                    "operator": rel.get('releasedBy', 'admin')
                }, ensure_ascii=False))
                rel_by = escape_sql(rel.get('releasedBy', 'admin'))
                rel_time = rel.get('releaseTime', datetime.now().strftime('%Y-%m-%d %H:%M:%S'))
                
                lines.append(
                    f"INSERT INTO `ReleaseHistory` (`Id`, `AppId`, `ClusterName`, `NamespaceName`, "
                    f"`BranchName`, `ReleaseId`, `PreviousReleaseId`, `Operation`, "
                    f"`OperationContext`, `IsDeleted`, `DeletedAt`, "
                    f"`DataChange_CreatedBy`, `DataChange_CreatedTime`) VALUES "
                    f"({history_id}, '{app['appId']}', 'default', '{ns_name}', "
                    f"'default', {rel_id}, {prev_id}, 0, "
                    f"'{op_context}', "
                    f"b'0', 0, '{rel_by}', '{rel_time}');"
                )
                if rel_id:
                    prev_id = rel_id
                history_id += 1
    lines.append("")

    print(f"  App: {len(apps)} 条")
    print(f"  Cluster: {cluster_id - 1} 条")
    print(f"  Namespace: {namespace_id - 1} 条")
    print(f"  AppNamespace: {app_ns_id - 1} 条")
    print(f"  Item: {item_id - 1} 条")
    print(f"  Release: {release_id - 1} 条")
    print(f"  ReleaseHistory: {history_id - 1} 条")

    return "\n".join(lines)


def generate_portaldb_sql(mock_data):
    """生成 ApolloPortalDB SQL"""
    lines = []
    lines.append("-- ===============================================")
    lines.append("-- Apollo PortalDB - Mock 测试应用注册数据")
    lines.append("-- ===============================================")
    lines.append("")
    lines.append("Use ApolloPortalDB;")
    lines.append("")

    apps = mock_data.get('_apps', [])

    lines.append("-- ---------- App 应用注册信息 ----------")
    lines.append("")

    for idx, app in enumerate(apps, start=1):
        lines.append(f"-- 应用: {app['appId']} ({app['name']})")
        lines.append(
            f"INSERT INTO `App` (`AppId`, `Name`, `OrgId`, `OrgName`, `OwnerName`, `OwnerEmail`, `IsDeleted`, `DeletedAt`) VALUES "
            f"('{app['appId']}', '{escape_sql(app['name'])}', '{app.get('orgId', f'org-{idx:03d}')}', "
            f"'{escape_sql(app['orgName'])}', 'admin', 'admin@company.com', b'0', 0);")
        lines.append("")

    print(f"  Portal App: {len(apps)} 条")
    return "\n".join(lines)


def main():
    print("=" * 60)
    print("  Apollo Mock 数据 → SQL 转换工具")
    print("=" * 60)
    print()

    mock_data = load_mock_data()
    apps = mock_data.get('_apps', [])
    configs = mock_data.get('_configs', {})
    total_configs = sum(len(ns) for app in configs.values() for ns in app.values())
    print(f"Mock 数据概览:")
    print(f"  应用数: {len(apps)}")
    print(f"  配置项总数: {total_configs}")
    print()

    print("生成 ApolloConfigDB SQL...")
    configdb_sql = generate_configdb_sql(mock_data)
    print()

    print("生成 ApolloPortalDB SQL...")
    portaldb_sql = generate_portaldb_sql(mock_data)
    print()

    output_dir = os.path.join(_root_dir, 'sql_output')
    os.makedirs(output_dir, exist_ok=True)

    configdb_path = os.path.join(output_dir, 'apolloconfigdb-mock-data.sql')
    with open(configdb_path, 'w', encoding='utf-8') as f:
        f.write(configdb_sql)
    print(f"✅ ApolloConfigDB SQL: {configdb_path}")

    portaldb_path = os.path.join(output_dir, 'apolloportaldb-mock-data.sql')
    with open(portaldb_path, 'w', encoding='utf-8') as f:
        f.write(portaldb_sql)
    print(f"✅ ApolloPortalDB SQL: {portaldb_path}")

    print()
    print("=" * 60)
    print("导入步骤:")
    print("  mysql -u root -p ApolloConfigDB < sql_output/apolloconfigdb-mock-data.sql")
    print("  mysql -u root -p ApolloPortalDB < sql_output/apolloportaldb-mock-data.sql")
    print("=" * 60)


if __name__ == '__main__':
    main()