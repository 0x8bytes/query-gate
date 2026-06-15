// Package model 定义跨层共享的纯数据结构(无业务逻辑、无第三方依赖)。
package model

// DatabaseInfo 是对外暴露的实例信息(不含 DSN)。spec 4.2。
type DatabaseInfo struct {
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	Description string `json:"description"`
}

// TableInfo 是一张表的名称与备注。
type TableInfo struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

// DatabaseRecord 是 databases 表一行。
type DatabaseRecord struct {
	Name        string
	Driver      string
	DSN         string // read_dsn:只读账号,query 走这个
	ExecDSN     string // exec_dsn:写账号,exec 走这个;空表示不支持 exec
	Description string
}
