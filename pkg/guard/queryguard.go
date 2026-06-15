package guard

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 危险函数/关键词(单词级,与 tokenize 一致)。
var dangerWords = map[string]bool{
	"sleep": true, "benchmark": true, "get_lock": true, "release_lock": true,
	"load_file": true, "outfile": true, "dumpfile": true,
	"is_free_lock": true, "is_used_lock": true, "master_pos_wait": true, "pg_sleep": true,
}

// query 路径禁止任何写、DDL、权限、事务、锁、连接状态或管理类语句。
var forbiddenSQLWords = map[string]bool{
	"insert": true, "update": true, "delete": true, "replace": true, "merge": true,
	"create": true, "alter": true, "drop": true, "truncate": true, "rename": true,
	"grant": true, "revoke": true,
	"set": true, "call": true, "do": true,
	"lock": true, "unlock": true,
	"begin": true, "start": true, "commit": true, "rollback": true, "savepoint": true,
	"copy": true, "vacuum": true, "analyze": true,
	"explain": true, "show": true, "desc": true, "describe": true,
}

// 首词白名单(只读语句类型)。
var firstWordAllow = map[string]bool{
	"select": true, "with": true,
}

// Guard 持有当前 db 的敏感列集合。非并发安全的不可变值,由调用方为每个 db 构造。
type Guard struct {
	sensitive map[string]bool
}

// New 用敏感列名(已小写)构造 Guard。
func New(sensitiveCols []string) *Guard {
	set := map[string]bool{}
	for _, c := range sensitiveCols {
		set[strings.ToLower(c)] = true
	}
	return &Guard{sensitive: set}
}

// Check 返回 nil 放行;否则返回拒绝原因。
func (g *Guard) Check(sql string) error {
	// 1. 禁多语句:剥字面量/注释后,出现分号且分号后还有非空内容即拒。
	if hasMultipleStatements(sql) {
		return fmt.Errorf("multiple statements are not allowed")
	}
	// 2. 禁 SELECT *(裸 * 或 table.*):检测标识符序列外的 * 字符。
	if containsStar(sql) {
		return fmt.Errorf("SELECT * is not allowed; list columns explicitly")
	}
	tokens := Tokenize(sql)
	if len(tokens) == 0 {
		return fmt.Errorf("empty query")
	}
	// 3. 首词白名单:/query 只允许 SELECT / WITH ... SELECT。
	if !firstWordAllow[tokens[0]] {
		return fmt.Errorf("only SELECT queries are allowed on /api/v1/query (got %q)", tokens[0])
	}
	// 4. 敏感列 + 禁止词 + 危险词:任一 token 命中即拒。
	for _, tok := range tokens {
		if g.sensitive[tok] {
			return fmt.Errorf("query references sensitive column: %s", tok)
		}
		if forbiddenSQLWords[tok] {
			return fmt.Errorf("statement keyword not allowed on /api/v1/query: %s", tok)
		}
		if dangerWords[tok] {
			return fmt.Errorf("dangerous function/keyword not allowed: %s", tok)
		}
	}
	return nil
}

// Mongo query 只允许 find 或只读 aggregate pipeline。pipeline 可使用 $lookup 做联表。
type mongoReadQuery struct {
	Collection string           `json:"collection"`
	Find       *json.RawMessage `json:"find"`
	Pipeline   []map[string]any `json:"pipeline"`
}

var forbiddenMongoKeys = map[string]bool{
	"$out": true, "$merge": true, "$where": true, "$function": true, "$accumulator": true,
	"insert": true, "insertone": true, "insertmany": true,
	"update": true, "updateone": true, "updatemany": true,
	"delete": true, "deleteone": true, "deletemany": true,
	"replaceone": true, "findandmodify": true, "drop": true,
	"createindex": true, "renamecollection": true,
}

// CheckMongo 校验 Mongo JSON 查询只包含 find 或只读 aggregate pipeline。
// 联表查询应使用 pipeline + $lookup;会写出或执行服务端 JS 的 stage/operator 会被拒绝。
func CheckMongo(query string, sensitiveCols []string) error {
	var q mongoReadQuery
	if err := json.Unmarshal([]byte(query), &q); err != nil {
		return fmt.Errorf("mongo query must be JSON {\"collection\",\"pipeline\"|\"find\"}: %v", err)
	}
	if q.Collection == "" {
		return fmt.Errorf("mongo query: collection is required")
	}
	hasFind := q.Find != nil
	hasPipeline := len(q.Pipeline) > 0
	if hasFind == hasPipeline {
		return fmt.Errorf("mongo query must contain exactly one of find or pipeline")
	}
	sens := map[string]bool{}
	for _, c := range sensitiveCols {
		sens[strings.ToLower(c)] = true
	}
	if hasFind {
		var filter any
		if err := json.Unmarshal(*q.Find, &filter); err != nil {
			return fmt.Errorf("mongo find must be a JSON object: %v", err)
		}
		return checkMongoValue(filter, sens)
	}
	for _, stage := range q.Pipeline {
		if len(stage) != 1 {
			return fmt.Errorf("mongo pipeline stage must contain exactly one operator")
		}
		for op, body := range stage {
			if err := checkMongoKey(op, sens); err != nil {
				return err
			}
			if err := checkMongoValue(body, sens); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkMongoValue(v any, sensitive map[string]bool) error {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			if err := checkMongoKey(k, sensitive); err != nil {
				return err
			}
			if err := checkMongoValue(vv, sensitive); err != nil {
				return err
			}
		}
	case []any:
		for _, vv := range x {
			if err := checkMongoValue(vv, sensitive); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkMongoKey(key string, sensitive map[string]bool) error {
	lower := strings.ToLower(key)
	if forbiddenMongoKeys[lower] {
		return fmt.Errorf("mongo write or unsafe operation not allowed: %s", key)
	}
	if sensitive[lower] {
		return fmt.Errorf("query references sensitive column: %s", key)
	}
	return nil
}

// containsStar 检测 query 里是否有 * 作为列选择(裸 * 或 .* )。
// 剥除字符串/注释后,只要出现 '*' 字符就拒(只读查询里 * 只可能是 SELECT *,
// 乘法运算极罕见且可改写,从严)。
func containsStar(sql string) bool {
	return strings.Contains(stripLiteralsAndComments(sql), "*")
}

// hasMultipleStatements 剥字面量/注释后,检测是否有分号且其后仍有非空白内容。
func hasMultipleStatements(sql string) bool {
	clean := stripLiteralsAndComments(sql)
	idx := strings.Index(clean, ";")
	if idx < 0 {
		return false
	}
	return strings.TrimSpace(clean[idx+1:]) != ""
}

// stripLiteralsAndComments 把字符串字面量与注释替换为空格,保留其余结构(含 * ; .)。
func stripLiteralsAndComments(sql string) string {
	var b strings.Builder
	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\'' || c == '"':
			quote := c
			i++
			for i < len(runes) && runes[i] != quote {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++ // 跳过被转义的下一个字符
				}
				i++
			}
			b.WriteByte(' ')
		case c == '-' && i+1 < len(runes) && runes[i+1] == '-':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '#':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i++
			b.WriteByte(' ')
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}
