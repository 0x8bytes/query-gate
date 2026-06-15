// Package router 负责 HTTP 引擎装配与路由注册(组合根)。
package router

import (
	"path/filepath"
	"runtime"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/handler"
	"github.com/0x8bytes/query-gate/internal/middleware"
	"github.com/0x8bytes/query-gate/internal/model"
)

// Setup 构建并返回配置好的 gin 引擎(*gin.Engine 实现 http.Handler)。
func Setup(h *handler.Handler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	_ = r.SetTrustedProxies(nil) // ClientIP 用 RemoteAddr
	r.LoadHTMLGlob(viewGlob())   // 运行时加载 internal/view/*.html 视图模板
	register(h, r)
	return r
}

// register 在一处集中注册全部路由(公开/查询侧、admin 侧、后台页面与 ui/* session 组)。全部 POST(RPC 风格),/health、根路径与后台页面 GET 除外。
func register(h *handler.Handler, r *gin.Engine) {
	r.GET("/", h.Index)
	r.GET("/health", h.Health)

	// ---- 初始化与后台页面（cookie session）----
	r.GET("/setup", h.SetupPage)
	r.POST("/setup", h.SetupSubmit)
	r.GET("/admin/login", h.LoginPage)
	r.POST("/admin/login", h.LoginSubmit)
	r.POST("/admin/logout", h.Logout)
	r.GET("/admin", middleware.Session(h.Store, h.ServerSecret, true), h.Dashboard)

	api := r.Group("/api/v1")
	api.Use(middleware.IPWhitelist(h.IPWhitelist), middleware.APIKey(h.Store))
	{
		api.POST("/databases/list", h.Databases)
		api.POST("/tables/list", h.Tables)
		api.POST("/tables/detail", h.Schema)
		api.POST("/query", h.Query)
		api.POST("/exec", h.Exec)
	}

	// ---- 后台 UI 接口（cookie session）----
	ui := r.Group("/api/v1/admin/ui")
	ui.Use(middleware.Session(h.Store, h.ServerSecret, false))
	{
		// 任意登录角色
		ui.POST("/tree", h.UITree)
		ui.POST("/query", h.UIQuery)
		ui.POST("/ddl", h.UIDDL)
		ui.POST("/me", h.MeGet)
		ui.POST("/me/password", h.MePassword)

		// 仅 super_admin
		adm := ui.Group("")
		adm.Use(middleware.RequireRole(model.RoleSuperAdmin))
		{
			adm.POST("/databases/list", h.AdminDBList)
			adm.POST("/databases/test", h.AdminDBTest)
			adm.POST("/databases/create", h.AdminDBCreate)
			adm.POST("/databases/update", h.AdminDBUpdate)
			adm.POST("/databases/delete", h.AdminDBDelete)
			adm.POST("/users/list", h.UserList)
			adm.POST("/users/create", h.UserCreate)
			adm.POST("/users/reset-password", h.UserResetPassword)
			adm.POST("/users/set-role", h.UserSetRole)
			adm.POST("/users/set-status", h.UserSetStatus)
			adm.POST("/users/delete", h.UserDelete)
			adm.POST("/exec", h.UIExec)
			adm.POST("/exec/databases", h.UIExecDatabases)
			adm.POST("/sensitive-columns/list", h.AdminSensList)
			adm.POST("/sensitive-columns/create", h.AdminSensCreate)
			adm.POST("/sensitive-columns/delete", h.AdminSensDelete)
			adm.POST("/query-logs/list", h.AdminLogList)
		}
	}
}

// viewGlob 返回视图模板的 glob 路径,兼容从仓库根目录启动与测试 CWD 两种情况。
func viewGlob() string {
	const rel = "internal/view/*.html"
	if m, _ := filepath.Glob(rel); len(m) > 0 {
		return rel
	}
	// 本文件位于 internal/router/router.go;模板在 internal/view/。
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		abs := filepath.Join(filepath.Dir(thisFile), "..", "view", "*.html")
		if m, _ := filepath.Glob(abs); len(m) > 0 {
			return abs
		}
	}
	return rel
}
