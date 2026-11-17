package model

import "time"

// AppDpRelation appId ↔ 部署项目 关系表（跳① 关系层）。
// 用途：operation_topology.apollo_app_id → deploy_project_name，再经跳② DPR 表展开四列。
// 该表纳入 AutoMigrate 自动建表（见 initialize/gorm.go 的 MysqlTables），
// 服务器启动即建表，无需事先执行 docs/业务数据/app_dp_relation.sql（该脚本仅供本地建档参考）。
// 字段已与生产 DDL 对齐（列宽/主键一致），AutoMigrate 对已存在的同名表只补缺失列与索引、不删列。
func (AppDpRelation) TableName() string {
	return "app_dp_relation"
}

type AppDpRelation struct {
	Id                int64      `gorm:"column:id;primary_key" json:"id"`
	DelFlag           int        `gorm:"column:del_flag;default:0" json:"delFlag"` // 删除标记
	CreateTime        *time.Time `gorm:"column:create_time" json:"createTime"`     // 创建时间（可空）
	UpdateTime        *time.Time `gorm:"column:update_time" json:"updateTime"`     // 更新时间（可空）
	ProjectType       string     `gorm:"column:project_type;size:32" json:"projectType"`
	ProjectId         string     `gorm:"column:project_id;size:64" json:"projectId"`
	ProjectName       string     `gorm:"column:project_name;size:128" json:"projectName"`
	DeployProjectId   string     `gorm:"column:deploy_project_id;size:64" json:"deployProjectId"`
	DeployProjectName string     `gorm:"column:deploy_project_name;size:128;index" json:"deployProjectName"`
	Node              string     `gorm:"column:node;size:128" json:"node"` // 节点（机房/地域）
	Env               int        `gorm:"column:env" json:"env"`            // 环境（1=PRO）
	ApolloHostId      int        `gorm:"column:apollo_host_id" json:"apolloHostId"`
	ApolloAppId       string     `gorm:"column:apollo_app_id;size:128;index" json:"apolloAppId"` // 关联键（跳①）
	ApolloAppEnv      string     `gorm:"column:apollo_app_env;size:16" json:"apolloAppEnv"`      // PRO / LPT（与拓扑表 env 对齐）
	ApolloAppCluster  string     `gorm:"column:apollo_app_cluster;size:64" json:"apolloAppCluster"`
	IsReleased        int        `gorm:"column:is_released;default:0" json:"isReleased"`
	Operation         string     `gorm:"column:operation;size:32" json:"operation"`
	Operator          int        `gorm:"column:operator" json:"operator"`
}
