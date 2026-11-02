package tools

import "testing"

func TestWithToken(t *testing.T) {
	tests := []struct {
		name  string
		wsURL string
		token string
		want  string
	}{
		{
			name:  "no token",
			wsURL: "ws://browser:3000",
			want:  "ws://browser:3000",
		},
		{
			name:  "blank token",
			wsURL: "ws://browser:3000",
			token: "   ",
			want:  "ws://browser:3000",
		},
		{
			name:  "appends token",
			wsURL: "ws://browser:3000",
			token: "secret",
			want:  "ws://browser:3000?token=secret",
		},
		{
			name:  "keeps explicit token",
			wsURL: "ws://browser:3000?token=explicit",
			token: "secret",
			want:  "ws://browser:3000?token=explicit",
		},
		{
			name:  "keeps existing query",
			wsURL: "ws://browser:3000/devtools/browser/abc?foo=bar",
			token: "secret",
			want:  "ws://browser:3000/devtools/browser/abc?foo=bar&token=secret",
		},
		{
			name:  "url-encodes token",
			wsURL: "ws://browser:3000",
			token: "a b+c&d",
			want:  "ws://browser:3000?token=a+b%2Bc%26d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withToken(tt.wsURL, tt.token); got != tt.want {
				t.Errorf("withToken(%q, %q) = %q, want %q", tt.wsURL, tt.token, got, tt.want)
			}
		})
	}
}
