// Package guard 是轻量统一的只读/敏感字段 guard。
// 实现机制:按标识符字符类扫描切分(非按空格),配合黑名单全等匹配。
// 注意:这不是安全边界,安全边界是数据库只读账号。
package guard

import (
	"strings"
	"unicode"
)

// Tokenize 把 SQL 文本切成小写标识符 token。
// 先剥除字符串字面量('...' "...")与注释(-- ... \n / /* */ / # ... \n),
// 再把连续的 [a-zA-Z0-9_] 累成一个 token,遇任何其他字符即断开。
// 反引号 ` 视为分隔符,故 `mobile` 切出 mobile。
func Tokenize(sql string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '\'' || c == '"': // 字符串字面量:跳到配对引号
			flush()
			quote := c
			i++
			for i < len(runes) && runes[i] != quote {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++ // 跳过被转义的下一个字符
				}
				i++
			}
		case c == '-' && i+1 < len(runes) && runes[i+1] == '-': // 行注释 --
			flush()
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case c == '#': // 行注释 #(MySQL)
			flush()
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(runes) && runes[i+1] == '*': // 块注释 /* */
			flush()
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i++ // 跳过结尾 /
		case isIdent(c):
			cur.WriteRune(c)
		default: // 空格、,.()`等任何非标识符字符 => 断开
			flush()
		}
	}
	flush()
	return tokens
}

func isIdent(c rune) bool {
	return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c)
}
