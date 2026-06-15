package apierror

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWrite_SetsStatusAndBody(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, ErrUnauthorized)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if body["error"] != "unauthorized" {
		t.Fatalf("error = %v, want unauthorized", body["error"])
	}
}

func TestWrite_QueryErrorIncludesErrno(t *testing.T) {
	rec := httptest.NewRecorder()
	Write(rec, QueryError("Error 1146: Table 'x' doesn't exist", 1146))

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if body["db_errno"].(float64) != 1146 {
		t.Fatalf("db_errno = %v, want 1146", body["db_errno"])
	}
	if body["message"] == "" {
		t.Fatalf("message should be passed through")
	}
}
