package model

// SensitiveColumn 是 sensitive_columns 表一行。
type SensitiveColumn struct {
	DB     string
	Column string
}
