package model

// 角色常量。
const (
	RoleSuperAdmin = "super_admin"
	RoleUser       = "user"
)

// User 是 users 表一行。
type User struct {
	Username     string
	PasswordHash string
	APIKey       string
	Role         string // super_admin | user
	Status       string // enabled | disabled
	CreatedAt    string
	UpdatedAt    string
}
