package template

import (
	"regexp"
	"strings"
)

var varRe = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Interpolate replaces every {{key}} placeholder in text with the corresponding
// value from vars. Unrecognised placeholders are left unchanged.
func Interpolate(text string, vars map[string]string) string {
	if len(vars) == 0 || !strings.Contains(text, "{{") {
		return text
	}
	return varRe.ReplaceAllStringFunc(text, func(m string) string {
		key := m[2 : len(m)-2]
		if v, ok := vars[key]; ok {
			return v
		}
		return m
	})
}

// ExtractVariables returns the sorted unique set of {{key}} names found in text.
func ExtractVariables(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, match := range varRe.FindAllStringSubmatch(text, -1) {
		if k := match[1]; !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}
