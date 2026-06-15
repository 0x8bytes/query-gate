package model

// QueryLog 是 query_logs 表一行(不含自增 id)。
type QueryLog struct {
	TS         string
	APIKeyName string
	DB         string
	Query      string
	RowCount   int
	ElapsedMs  int64
	Status     string // ok | denied | error
	Error      string
}

// QueryLogFilter 是 list 过滤条件。Before 为空表示不限。
type QueryLogFilter struct {
	DB     string
	APIKey string // 按 api_key_name 过滤
	Before string // ts < Before(分页游标)
	Limit  int
}
