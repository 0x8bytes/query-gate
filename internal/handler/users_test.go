package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0x8bytes/query-gate/internal/data"
	"github.com/0x8bytes/query-gate/internal/handler"
	"github.com/0x8bytes/query-gate/internal/model"
	"github.com/0x8bytes/query-gate/pkg/auth"
)

// actAs 注入 current_user/current_role（与 middleware 用的 context key 一致），
// 免去构造真实 session cookie。
func actAs(username, role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("current_user", username)
		c.Set("current_role", role)
		c.Next()
	}
}

func openStore(t *testing.T) *data.Store {
	t.Helper()
	st, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// seedUser 直接入库一个用户。
func seedUser(t *testing.T, st *data.Store, name, role, status string) {
	t.Helper()
	hash, err := auth.HashPassword("pw123456")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := st.CreateUser(model.User{
		Username: name, PasswordHash: hash, APIKey: "qn_" + name,
		Role: role, Status: status, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body not json (%q): %v", rec.Body.String(), err)
	}
	return m
}

// usersEngine 自建引擎，以 actor 身份注册全部用户管理路由。
func usersEngine(st *data.Store, actorName, actorRole string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	h := &handler.Handler{Store: st, ServerSecret: []byte("s")}
	r := gin.New()
	g := r.Group("/", actAs(actorName, actorRole))
	g.POST("/users/list", h.UserList)
	g.POST("/users/create", h.UserCreate)
	g.POST("/users/reset-password", h.UserResetPassword)
	g.POST("/users/set-role", h.UserSetRole)
	g.POST("/users/set-status", h.UserSetStatus)
	g.POST("/users/delete", h.UserDelete)
	g.POST("/me", h.MeGet)
	g.POST("/me/password", h.MePassword)
	return r
}

func TestUserCreate_ReturnsKeyAndAppearsInList(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/create", `{"username":"alice","password":"secret1","role":"user"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: code %d body %s", rec.Code, rec.Body.String())
	}
	m := decode(t, rec)
	key, _ := m["api_key"].(string)
	if !strings.HasPrefix(key, "qn_") {
		t.Fatalf("expected qn_ api_key, got %q", key)
	}

	list := doJSON(r, http.MethodPost, "/users/list", "")
	if !strings.Contains(list.Body.String(), `"alice"`) {
		t.Fatalf("alice not in list: %s", list.Body.String())
	}
}

func TestUserCreate_DuplicateRejected(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/create", `{"username":"root","password":"x12345","role":"user"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate: want 400, got %d", rec.Code)
	}
}

func TestUserCreate_PipeInNameRejected(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/create", `{"username":"a|b","password":"x12345","role":"user"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pipe name: want 400, got %d", rec.Code)
	}
}

func TestUserResetPassword_UpdatesHash(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	seedUser(t, st, "bob", model.RoleUser, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/reset-password", `{"username":"bob","password":"newpass1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: code %d body %s", rec.Code, rec.Body.String())
	}
	u, found, err := st.GetUserByName("bob")
	if err != nil || !found {
		t.Fatalf("get bob: %v found=%v", err, found)
	}
	if !auth.CheckPassword(u.PasswordHash, "newpass1") {
		t.Fatalf("new password does not verify")
	}
}

func TestUserResetPassword_NotFound(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/reset-password", `{"username":"ghost","password":"newpass1"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("reset ghost: want 404, got %d", rec.Code)
	}
}

func TestUserSetRole_DemoteOnlySuperAdminBlocked(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/set-role", `{"username":"root","role":"user"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("demote last super admin: want 403, got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestUserSetRole_DemoteOKWithAnotherSuperAdmin(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	seedUser(t, st, "second", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/set-role", `{"username":"second","role":"user"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("demote with backup: want 200, got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestUserSetStatus_DisableSelfBlocked(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	seedUser(t, st, "second", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/set-status", `{"username":"root","status":"disabled"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disable self: want 403, got %d", rec.Code)
	}
}

func TestUserSetStatus_DisableLastSuperAdminBlocked(t *testing.T) {
	// 仅一个 enabled 超管 lone，actor 为普通用户 op；op 停用 lone 应被拒。
	st := openStore(t)
	seedUser(t, st, "lone", model.RoleSuperAdmin, "enabled")
	seedUser(t, st, "op", model.RoleUser, "enabled")
	r := usersEngine(st, "op", model.RoleUser)

	rec := doJSON(r, http.MethodPost, "/users/set-status", `{"username":"lone","status":"disabled"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disable last super admin: want 403, got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestUserDelete_SelfBlocked(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/delete", `{"username":"root"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete self: want 403, got %d", rec.Code)
	}
}

func TestUserDelete_LastSuperAdminBlocked(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "lone", model.RoleSuperAdmin, "enabled")
	seedUser(t, st, "op", model.RoleUser, "enabled")
	r := usersEngine(st, "op", model.RoleUser)

	rec := doJSON(r, http.MethodPost, "/users/delete", `{"username":"lone"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete last super admin: want 403, got %d body %s", rec.Code, rec.Body.String())
	}
}

func TestUserDelete_NormalUserOK(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	seedUser(t, st, "bob", model.RoleUser, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/users/delete", `{"username":"bob"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete normal: want 200, got %d body %s", rec.Code, rec.Body.String())
	}
	if _, found, _ := st.GetUserByName("bob"); found {
		t.Fatalf("bob still present after delete")
	}
}

func TestMePassword_CorrectAndIncorrectOld(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled") // seed pw is "pw123456"
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	// 错误旧密码 → 403。
	bad := doJSON(r, http.MethodPost, "/me/password", `{"old":"wrong","new":"brandnew1"}`)
	if bad.Code != http.StatusForbidden {
		t.Fatalf("wrong old: want 403, got %d", bad.Code)
	}

	// 正确旧密码 → 200，新密码可校验。
	ok := doJSON(r, http.MethodPost, "/me/password", `{"old":"pw123456","new":"brandnew1"}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("correct old: code %d body %s", ok.Code, ok.Body.String())
	}
	u, _, _ := st.GetUserByName("root")
	if !auth.CheckPassword(u.PasswordHash, "brandnew1") {
		t.Fatalf("new password does not verify")
	}
}

func TestMeGet_ReturnsSelf(t *testing.T) {
	st := openStore(t)
	seedUser(t, st, "root", model.RoleSuperAdmin, "enabled")
	r := usersEngine(st, "root", model.RoleSuperAdmin)

	rec := doJSON(r, http.MethodPost, "/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me: code %d body %s", rec.Code, rec.Body.String())
	}
	m := decode(t, rec)
	if m["username"] != "root" || m["role"] != model.RoleSuperAdmin {
		t.Fatalf("me payload wrong: %v", m)
	}
}
