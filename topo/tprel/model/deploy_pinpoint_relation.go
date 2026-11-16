package model

import "time"

// DeployPinpointRelation 部署项目 ↔ PP 应用 映射维表。
// 用途：拓扑表（operation_topology）的 dep_project / eng_project / node 为预留列，
// 真实来源是本表，按 pp_application 关联补齐（见 service/pinpoint_relation.go）。
//
// 该表纳入 AutoMigrate 自动建表（见 initialize/gorm.go 的 MysqlTables），服务器启动即建表；
// 字段已与生产 DDL 对齐（列宽/主键一致），AutoMigrate 对已存在的同名表只补缺失列与索引、不删列。
// 表内数据仍由运维导入（见 docs/业务数据/deploy_pinpoint_relation.sql）。
func (DeployPinpointRelation) TableName() string {
	return "deploy_pinpoint_relation"
}

type DeployPinpointRelation struct {
	Id            int64     `gorm:"column:id;primary_key;auto_increment" json:"id"`
	DepProject    string    `gorm:"column:dep_project;size:200" json:"depProject"`             // 部署项目
	PpApplication string    `gorm:"column:pp_application;size:100;index" json:"ppApplication"` // PP 应用（关联键）
	EgProject     string    `gorm:"column:eg_project;size:100" json:"egProject"`               // 工程项目
	Node          string    `gorm:"column:node;size:50" json:"node"`                           // 节点
	CreateTime    time.Time `gorm:"column:create_time" json:"createTime"`                      // 创建时间
	UpdateTime    time.Time `gorm:"column:update_time" json:"updateTime"`                      // 更新时间
}

// PinpointOwner 映射行的精简视图（按 deploy_project 索引后在内存中复用）
type PinpointOwner struct {
	DepProject    string // 部署项目
	PpApplication string // PP 应用
	EgProject     string // 工程项目
	Node          string // 节点
}
