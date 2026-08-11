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
	Type       ActionType
	Value      string
	Score      int // retained for compatibility with the TUI; this is the confidence score.
	Confidence int
	Evidence   string
}

var (
	codeRE       = regexp.MustCompile(`(?i)\b[A-Z0-9]{4,12}\b`)
	segCodeRE    = regexp.MustCompile(`(?i)\b[A-Z0-9]{1,8}(?:-[A-Z0-9]{1,8})+\b`)
	spacedCodeRE = regexp.MustCompile(`\b\d{1,4}(?: \d{1,4})+\b`)
	urlRE        = regexp.MustCompile(`https?://[^\s<>'\"]+`)
	contextRE    = regexp.MustCompile(`(?i)\b(otp|one[- ]?time|verification|verify|security|login|sign[- ]?in|confirm|authentication|auth|passcode|code|device)\b`)
	badRE        = regexp.MustCompile(`(?i)\b(order|invoice|price|amount|phone|ticket|year|date|time|copyright|unsubscribe|tracking|total)\b`)
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
	addCode := func(raw string, start int) {
		val := normalizeCode(raw)
		if !validCode(val) {
			return
		}
		around := window(body, start, 90)
		score := 20
		ev := "code-shaped token"
		if contextRE.MatchString(around) {
			score += 50
			ev = "near verification/login language"
		}
		if badRE.MatchString(around) {
			score -= 60
		}
		if strings.Contains(strings.ToUpper(subject), val) {
			score += 15
			ev += "; appears in subject"
		}
		if score >= 50 {
			out = append(out, ActionCandidate{Type: Code, Value: val, Score: score, Confidence: score, Evidence: ev})
		}
	}
	for _, idx := range codeRE.FindAllStringIndex(body, -1) {
		addCode(body[idx[0]:idx[1]], idx[0])
	}
	for _, idx := range segCodeRE.FindAllStringIndex(body, -1) {
		addCode(body[idx[0]:idx[1]], idx[0])
	}
	for _, idx := range spacedCodeRE.FindAllStringIndex(body, -1) {
		addCode(body[idx[0]:idx[1]], idx[0])
	}
	for _, raw := range urlRE.FindAllString(body, -1) {
		u := strings.TrimRight(raw, ".,);]")
		score := 20
		ev := "URL in message"
		l := strings.ToLower(u)
		if strings.Contains(l, "verify") || strings.Contains(l, "confirm") || strings.Contains(l, "login") || strings.Contains(l, "signin") || strings.Contains(l, "token") || strings.Contains(l, "magic") {
			score += 45
			ev = "verification/sign-in URL"
		}
		if strings.Contains(l, "unsubscribe") || strings.Contains(l, "track") {
			score -= 50
		}
		if parsed, e := url.Parse(u); e == nil && parsed.Scheme != "" && parsed.Host != "" && score > 0 {
			out = append(out, ActionCandidate{Type: Link, Value: u, Score: score, Confidence: score, Evidence: ev})
		}
	}
	out = dedupe(out)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
func validCode(s string) bool {
	if len(s) < 4 || len(s) > 12 || allLetters(s) {
		return false
	}
	allDigits := true
	for _, r := range s {
		if !unicode.IsDigit(r) {
			allDigits = false
			break
		}
	}
	if allDigits {
		return len(s) == 4 || len(s) == 6 || len(s) == 8
	}
	// Short mixed tokens in newsletters and tracking templates (for example
	// 24PX) are not useful verification codes. Unsegmented mixed codes need
	// enough numeric signal to be credible; segmented forms are handled by
	// the stronger context around the token.
	digits, letters := 0, 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			digits++
		} else if unicode.IsLetter(r) {
			letters++
		}
	}
	return digits >= 3 && letters >= 1
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
