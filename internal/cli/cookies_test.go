package cli

import "testing"

func TestNormalizeCookieHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "raw cookie header",
			in:   "Cookie: a=1; b=2",
			want: "a=1; b=2",
		},
		{
			name: "set cookie lines",
			in:   "Set-Cookie: a=1; Path=/; HttpOnly\nSet-Cookie: b=2; Secure",
			want: "a=1; b=2",
		},
		{
			name: "bare session token",
			in:   "token.with.dots",
			want: "__Secure-next-auth.session-token=token.with.dots",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeCookieHeader(tt.in)
			if err != nil {
				t.Fatalf("NormalizeCookieHeader returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCookieFileNetscape(t *testing.T) {
	t.Parallel()

	cookieFile := "# Netscape HTTP Cookie File\n" +
		".lunars.dev\tTRUE\t/\tTRUE\t0\t__Secure-next-auth.session-token\tabc\n" +
		".example.com\tTRUE\t/\tTRUE\t0\tignored\tnope\n"

	got, err := ParseCookieFile(cookieFile, "lunars.dev")
	if err != nil {
		t.Fatalf("ParseCookieFile returned error: %v", err)
	}
	if got != "__Secure-next-auth.session-token=abc" {
		t.Fatalf("got %q", got)
	}
}
