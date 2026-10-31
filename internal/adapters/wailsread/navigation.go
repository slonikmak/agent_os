package wailsread

import (
	"net"
	"net/url"
	"personaldesk/internal/adapters/diagnostics"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// SafeBrowserURL is the application allowlist immediately before the OS opener.
// It deliberately rejects ambiguous authorities rather than repairing them.
func SafeBrowserURL(value string) (string, bool) {
	if len(value) == 0 || len(value) > 8192 || strings.ContainsAny(value, "\\") {
		return "", false
	}
	decoded, err := url.PathUnescape(value)
	if err != nil || strings.Contains(decoded, "\\") {
		return "", false
	}
	for _, r := range decoded {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", false
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	host := u.Hostname()
	if host == "" || strings.Contains(host, "%") || strings.HasSuffix(u.Host, ":") {
		return "", false
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", false
		}
	}
	if ip := net.ParseIP(host); ip == nil {
		lastLabel := host[strings.LastIndex(host, ".")+1:]
		_, numericErr := strconv.ParseUint(lastLabel, 0, 64)
		if strings.Trim(lastLabel, "0123456789") == "" || numericErr == nil {
			return "", false
		}
		if len(host) > 253 {
			return "", false
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return "", false
			}
			for _, r := range label {
				if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
					return "", false
				}
			}
		}
	}
	return u.String(), true
}

// NavigationFacade is separate from the saved-state facade and has no file-open
// operation. The UI calls it only from a deliberate link activation.
type NavigationFacade struct {
	open func(string) error
	diag *diagnostics.Buffer
}

func NewNavigation(open func(string) error) *NavigationFacade {
	return &NavigationFacade{open: open, diag: diagnostics.New()}
}
func NewNavigationDiagnostics(open func(string) error, diag *diagnostics.Buffer) *NavigationFacade {
	return &NavigationFacade{open: open, diag: diag}
}
func (n *NavigationFacade) OpenURL(value string) (opened bool) {
	requestID := diagnostics.ID()
	defer n.diag.Operation("wailsread", "framework", requestID, time.Now())
	defer func() {
		if recover() != nil {
			n.diag.Fault("wailsread", "framework", requestID, "panic")
			opened = false
		}
	}()
	target, ok := SafeBrowserURL(value)
	if !ok || n.open == nil {
		return false
	}
	if err := n.open(target); err != nil {
		n.diag.Fault("wailsread", "framework", requestID, "io")
		return false
	}
	return true
}
