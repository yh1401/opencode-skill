-- deploy_pinpoint_relation 建表脚本（跳②：部署项目 ↔ PP 应用 映射维表）
-- 用途：operation_topology 的 dep_project / eng_project / node / pp_application 为预留/占位列，
--       真实来源为本表，按 dep_project 名称关联后作为 appId 的属性补充
--       （单值列取属性列表首值，多值走 depProjects / engProjects / nodes / ppApplications 列表，不扩行）。
-- 数据导入：由运维用 CSV 导出文件导入，形如
--   LOAD DATA LOCAL INFILE 'deploy_pinpoint_relation_202609141121.csv' INTO TABLE `deploy_pinpoint_relation`
--   FIELDS TERMINATED BY ',' OPTIONALLY ENCLOSED BY '"' LINES TERMINATED BY '\n' IGNORE 1 LINES;
-- 注意：本表**不纳入 AutoMigrate**（initialize/gorm.go 的 MysqlTables 只注册 operation_topology 与 app_dp_relation），
--       需由运维/DB 侧按本脚本自建（或直接导入生产导出）后再启动服务；程序绝不反向修改本表结构。

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS `deploy_pinpoint_relation` (
  `id`             BIGINT       NOT NULL AUTO_INCREMENT COMMENT '主键',
  `dep_project`    VARCHAR(200) NOT NULL DEFAULT '' COMMENT '部署项目',
  `pp_application` VARCHAR(100) NOT NULL DEFAULT '' COMMENT 'PP 应用（关联键）',
  `eg_project`     VARCHAR(100) NOT NULL DEFAULT '' COMMENT '工程项目',
  `node`           VARCHAR(50)  NOT NULL DEFAULT '' COMMENT '节点',
  `node_cn`        VARCHAR(50)  NOT NULL DEFAULT '' COMMENT '中文节点（已废弃 2026-09-30：接口/查询/补录均不再使用，仅为兼容历史列保留）',
  `create_time`    DATETIME     NULL COMMENT '创建时间',
  `update_time`    DATETIME     NULL COMMENT '更新时间',
  PRIMARY KEY (`id`),
  KEY `idx_pp_application` (`pp_application`),
  KEY `idx_dep_project`    (`dep_project`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='部署项目与PP应用关系表';
