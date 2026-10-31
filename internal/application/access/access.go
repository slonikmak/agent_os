// Package access owns authenticated local caller identity and operation policy.
package access

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

type Role string

const (
	Reader      Role = "reader"
	Worker      Role = "worker"
	Coordinator Role = "coordinator"
)

type Operation string

const (
	ReadWorkspace     Operation = "workspace.read"
	ReadTask          Operation = "tasks.read"
	ReadInbox         Operation = "inbox.read"
	ReadMaterial      Operation = "materials.read"
	UploadMaterial    Operation = "materials.upload"
	Receive           Operation = "inbox.receive"
	ResolveCreateTask Operation = "inbox.resolve.createTask"
)

type Principal struct {
	ClientID string
	Role     Role
}
type Credential struct {
	Role  Role
	Token string
}
type Authenticator struct{ credentials []Credential }

var ErrCredentials = errors.New("invalid caller credentials")

func ValidToken(token string) bool {
	return len(token) == 64 && strings.Trim(token, "0123456789abcdef") == ""
}
func New(credentials []Credential) (*Authenticator, error) {
	if len(credentials) != 3 {
		return nil, ErrCredentials
	}
	roles := map[Role]bool{}
	tokens := map[string]bool{}
	for _, c := range credentials {
		if (c.Role != Reader && c.Role != Worker && c.Role != Coordinator) || roles[c.Role] || !ValidToken(c.Token) || tokens[c.Token] {
			return nil, ErrCredentials
		}
		roles[c.Role] = true
		tokens[c.Token] = true
	}
	return &Authenticator{append([]Credential{}, credentials...)}, nil
}
func (a *Authenticator) Authenticate(header string) (Principal, bool) {
	if a == nil || len(header) != 71 || !strings.HasPrefix(header, "Bearer ") || !ValidToken(header[7:]) {
		return Principal{}, false
	}
	token := header[7:]
	matches := 0
	var role Role
	for _, c := range a.credentials {
		match := subtle.ConstantTimeCompare([]byte(token), []byte(c.Token))
		matches += match
		if match == 1 {
			role = c.Role
		}
	}
	if matches != 1 {
		return Principal{}, false
	}
	digest := sha256.Sum256([]byte("personal-desk-local-caller-v1:" + token))
	return Principal{ClientID: "client-" + hex.EncodeToString(digest[:16]), Role: role}, true
}
func Allowed(p Principal, operation Operation) bool {
	if len(p.ClientID) != 39 || !strings.HasPrefix(p.ClientID, "client-") || strings.Trim(p.ClientID[7:], "0123456789abcdef") != "" {
		return false
	}
	if p.Role != Reader && p.Role != Worker && p.Role != Coordinator {
		return false
	}
	switch operation {
	case ReadWorkspace, ReadTask, ReadInbox, ReadMaterial:
		return true
	case Receive:
		return p.Role == Worker || p.Role == Coordinator
	case ResolveCreateTask, UploadMaterial:
		return p.Role == Coordinator
	default:
		return false
	}
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
