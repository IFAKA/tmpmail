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

func TestExtractAllRequiredCodeShapes(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"four digit", "Your code is 1234", "1234"},
		{"six digit", "OTP 123456", "123456"},
		{"eight digit", "Security code 12345678", "12345678"},
		{"spaced numeric", "Use verification code 12 34 56", "123456"},
		{"dashed numeric", "Enter OTP 123-456", "123456"},
		{"alphanumeric", "Verify with ABC123", "ABC123"},
		{"subject", "", "4821"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []ActionCandidate
			if tc.name == "subject" {
				got = Extract("Verification code 4821", tc.body, "")
			} else {
				got = Extract("", tc.body, "")
			}
			for _, x := range got {
				if x.Value == tc.want {
					return
				}
			}
			t.Fatalf("want %q in %#v", tc.want, got)
		})
	}
}

func TestExtractRejectsCommonNoise(t *testing.T) {
	for _, body := range []string{
		"The year is 2024 and the date is 0131.",
		"Price: $1234",
		"Call 5551234567",
		"Order ID 123456",
		"Invoice 87654321",
		"Track https://example.test/track/123456",
		"Unsubscribe https://example.test/unsubscribe/123456",
		"Newsletter template spacing: 24PX",
	} {
		if got := Extract("", body, ""); len(got) != 0 {
			t.Errorf("noise %q produced %#v", body, got)
		}
	}
}

func TestReadablePreservesEmailParagraphs(t *testing.T) {
	got := Readable(`<p>Hello</p><p>Confirm your account:</p><a href="https://example.test/confirm">Confirm</a>`)
	if got != "Hello\nConfirm your account:\nConfirm\nhttps://example.test/confirm" {
		t.Fatalf("unexpected readable mail: %q", got)
	}
}

func TestExtractHTMLAndLinkKinds(t *testing.T) {
	got := Extract("", "", `<html><a href="https://example.test/magic?token=x">Sign in</a><button href="https://example.test/confirm/x">Confirm</button></html>`)
	if len(got) != 2 {
		t.Fatalf("want two useful links, got %#v", got)
	}
}
