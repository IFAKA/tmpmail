package mail

import "testing"

func TestExtractCodes(t *testing.T) {
	cases := []struct {
		s    string
		want string
	}{{"Your verification code is 738194", "738194"}, {"OTP: 123-456", "123456"}, {"Security code Q7K-29P", "Q7K29P"}, {"Use ABC123 to sign in", "ABC123"}}
	for _, tc := range cases {
		got := Extract("", tc.s, "")
		if len(got) == 0 || got[0].Value != tc.want {
			t.Fatalf("%q: %#v", tc.s, got)
		}
	}
}
func TestLinkBeatsTracking(t *testing.T) {
	got := Extract("Verify", `Verify: https://x.test/verify?t=abc Tracking: https://x.test/track/1`, "")
	if len(got) == 0 || got[0].Type != Link || got[0].Value != "https://x.test/verify?t=abc" {
		t.Fatalf("%#v", got)
	}
}
func TestHTMLHref(t *testing.T) {
	got := Extract("Confirm", "", `<a href="https://x.test/confirm/abc">Confirm account</a>`)
	if len(got) == 0 || got[0].Type != Link {
		t.Fatalf("%#v", got)
	}
}
