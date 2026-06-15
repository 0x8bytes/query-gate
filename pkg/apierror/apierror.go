// Package apierror 定义统一的 API 错误码与 JSON 错误响应。
package apierror

import (
	"encoding/json"
	"net/http"
)

// APIError 是写给客户端的错误。Code 为机器可读码，Message 为可读说明。
// DBErrno 仅在 query_error 时填充（MySQL errno），其余为 0（不输出）。
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"error"`
	Message string `json:"message"`
	DBErrno int    `json:"db_errno,omitempty"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// 预定义错误（spec 4.6）。
var (
	ErrUnauthorized = &APIError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "missing or invalid API key"}
	ErrForbiddenIP  = &APIError{Status: http.StatusForbidden, Code: "forbidden_ip", Message: "client IP not allowed"}
	ErrUnknownDB    = &APIError{Status: http.StatusNotFound, Code: "unknown_database", Message: "unknown database alias"}
	ErrQueryTimeout = &APIError{Status: http.StatusGatewayTimeout, Code: "query_timeout", Message: "query timed out"}
	ErrInternal     = &APIError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "internal server error"}
)

// Forbidden 构造写/改/危险语句被拒的错误（4.6 forbidden_statement）。
func Forbidden(msg string) *APIError {
	return &APIError{Status: http.StatusForbidden, Code: "forbidden_statement", Message: msg}
}

// BadRequest 构造一般的请求参数错误（沿用 400 + 自定义码）。
func BadRequest(code, msg string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: code, Message: msg}
}

// QueryError 构造透传数据库原始报错的错误（4.6 query_error，含 errno）。
func QueryError(msg string, errno int) *APIError {
	return &APIError{Status: http.StatusUnprocessableEntity, Code: "query_error", Message: msg, DBErrno: errno}
}

// Write 把 APIError 序列化为 JSON 写入响应。
func Write(w http.ResponseWriter, e *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}
