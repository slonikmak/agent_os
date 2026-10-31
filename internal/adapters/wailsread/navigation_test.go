package wailsread

import (
	"testing"
)

func TestSafeNavigationOnlyCallsOpenerForAcceptedUserTargets(t *testing.T) {
	calls := 0
	n := NewNavigation(func(string) error { calls++; return nil })
	for _, target := range []string{"https://example.invalid/path?q=1#anchor", "HTTP://localhost:1234/", "http://127.0.0.1/", "https://[::1]:443/"} {
		if !n.OpenURL(target) {
			t.Fatal("valid URL refused", target)
		}
	}
	before := calls
	for _, target := range []string{"", "/private/path", "file:///etc/passwd", "javascript:alert(1)", "//example.invalid", "https://user:secret@example.invalid", "https://example.invalid:0/", "https://example.invalid:65536/", "https://example.invalid:/", "https://example.invalid\n/", "https://example.invalid/%0a", "https://example.invalid/%5c", "https://exa mple.invalid/", "https://127.1/", "https://0x7f000001/", "https://2130706433/", "https://.example.invalid/", "https://example..invalid/", "https://-bad.invalid/", "https://example.invalid\\@other.invalid/", "https:///example.invalid", "https://[::1%25en0]/"} {
		if n.OpenURL(target) {
			t.Fatal("unsafe URL accepted", target)
		}
	}
	if calls != before {
		t.Fatal("unsafe target reached OS opener")
	}
}
