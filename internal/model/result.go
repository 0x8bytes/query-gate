package model

// QueryResult 是一次查询的结果(spec 4.5)。
type QueryResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"row_count"`
	Truncated bool     `json:"truncated"`
	ElapsedMs int64    `json:"elapsed_ms"`
}
