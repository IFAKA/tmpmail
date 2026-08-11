package mail

import (
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type ActionType string

const (
	Code ActionType = "code"
	Link ActionType = "link"
)

type ActionCandidate struct {
	Type     ActionType
	Value    string
	Score    int
	Evidence string
}

var (
	codeRE    = regexp.MustCompile(`(?i)\b(?:[A-Z0-9]{4,12}|[A-Z0-9]{2,8}(?:-[A-Z0-9]{2,8})+)\b`)
	urlRE     = regexp.MustCompile(`https?://[^\s<>'\"]+`)
	contextRE = regexp.MustCompile(`(?i)\b(otp|one[- ]?time|verification|verify|security|login|sign[- ]?in|confirm|authentication|auth|passcode|code|device)\b`)
	badRE     = regexp.MustCompile(`(?i)\b(order|invoice|price|amount|phone|ticket|year|copyright|unsubscribe|tracking)\b`)
)

func normalizeCode(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, s)
}
func Extract(subject, text, htmlBody string) []ActionCandidate {
	body := html.UnescapeString(subject + "\n" + text + "\n" + stripHTML(htmlBody))
	var out []ActionCandidate
	for _, idx := range codeRE.FindAllStringIndex(body, -1) {
		raw := body[idx[0]:idx[1]]
		val := normalizeCode(raw)
		if len(val) < 4 || len(val) > 12 || allLetters(val) {
			continue
		}
		around := window(body, idx[0], 90)
		score := 35
		ev := "code-shaped token"
		if contextRE.MatchString(around) {
			score += 45
			ev = "near verification/login language"
		}
		if badRE.MatchString(around) {
			score -= 35
		}
		if strings.Contains(strings.ToUpper(subject), val) {
			score += 15
			ev += "; appears in subject"
		}
		out = append(out, ActionCandidate{Code, val, score, ev})
	}
	for _, raw := range urlRE.FindAllString(body, -1) {
		u := strings.TrimRight(raw, ".,);]")
		score := 35
		ev := "URL in message"
		l := strings.ToLower(u)
		if strings.Contains(l, "verify") || strings.Contains(l, "confirm") || strings.Contains(l, "login") || strings.Contains(l, "signin") || strings.Contains(l, "token") || strings.Contains(l, "magic") {
			score += 45
			ev = "verification/sign-in URL"
		}
		if strings.Contains(l, "unsubscribe") || strings.Contains(l, "track") {
			score -= 50
		}
		if parsed, e := url.Parse(u); e == nil && parsed.Scheme != "" && parsed.Host != "" {
			out = append(out, ActionCandidate{Link, u, score, ev})
		}
	}
	out = dedupe(out)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
func allLetters(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
func window(s string, i, n int) string {
	a := i - n
	if a < 0 {
		a = 0
	}
	b := i + n
	if b > len(s) {
		b = len(s)
	}
	return s[a:b]
}
func dedupe(in []ActionCandidate) []ActionCandidate {
	m := map[string]ActionCandidate{}
	for _, x := range in {
		k := string(x.Type) + "\x00" + x.Value
		if y, ok := m[k]; !ok || x.Score > y.Score {
			m[k] = x
		}
	}
	out := make([]ActionCandidate, 0, len(m))
	for _, x := range m {
		out = append(out, x)
	}
	return out
}
