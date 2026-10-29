package client

import "strings"

// CodeLen 是房间码长度。
const CodeLen = 6

// codeChars 去掉了 I/O/0/1 这些容易看错的字符。
// 现场最常见的报错就是"房间码输错了"，所以字符集宁可小一点。
const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NormalizeCode 把用户输入规整成房间码（去空格、转大写）。
func NormalizeCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// ValidCode 判断房间码是否合法。
func ValidCode(s string) bool {
	if len(s) != CodeLen {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(codeChars, r) {
			return false
		}
	}
	return true
}
