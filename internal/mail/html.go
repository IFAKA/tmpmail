package mail

import (
	"html"
	"regexp"
	"strings"
)

var tagRE = regexp.MustCompile(`(?s)<[^>]*>`)
var hrefRE = regexp.MustCompile(`(?is)href\s*=\s*["']([^"']+)["']`)
var bareHrefRE = regexp.MustCompile(`(?is)href\s*=\s*([^\s"'<>]+)`)

// Readable returns a conservative terminal representation of HTML mail. It
// intentionally keeps the extracted hrefs so users can still act on buttons
// when the surrounding markup is malformed.
func Readable(s string) string { return stripHTML(s) }

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
	for _, m := range bareHrefRE.FindAllStringSubmatch(s, -1) {
		if len(m) > 1 && !strings.Contains(strings.Join(links, "\n"), m[1]) {
			links = append(links, strings.TrimRight(m[1], "'\""))
		}
	}
	s = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</div>|</li>`).ReplaceAllString(s, "\n")
	s = tagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	if len(links) > 0 {
		s += "\n" + strings.Join(links, "\n")
	}
	return preserveLines(s)
}

func preserveLines(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
