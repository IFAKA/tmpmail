package mail

import (
	"html"
	"regexp"
	"strings"
)

var tagRE = regexp.MustCompile(`(?s)<[^>]*>`)
var hrefRE = regexp.MustCompile(`(?is)href\s*=\s*["']([^"']+)["']`)

func stripHTML(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	var links []string
	for _, m := range hrefRE.FindAllStringSubmatch(s, -1) {
		if len(m) > 1 {
			links = append(links, m[1])
		}
	}
	s = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>`).ReplaceAllString(s, "\n")
	s = tagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	if len(links) > 0 {
		s += "\n" + strings.Join(links, "\n")
	}
	return strings.Join(strings.Fields(s), " ")
}
