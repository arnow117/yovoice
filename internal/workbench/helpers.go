package workbench

func ptr(s string) *string { return &s }

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// truncateRunes 按字符数截断文本。
func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
