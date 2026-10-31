package access

import (
	"context"
	"strings"
	"testing"
)

func credentials() []Credential {
	return []Credential{{Reader, strings.Repeat("1", 64)}, {Worker, strings.Repeat("2", 64)}, {Coordinator, strings.Repeat("3", 64)}}
}
func TestAuthenticatedIdentityAndRoleOperationMatrix(t *testing.T) {
	auth, err := New(credentials())
	if err != nil {
		t.Fatal(err)
	}
	identities := map[string]bool{}
	for _, credential := range credentials() {
		p, ok := auth.Authenticate("Bearer " + credential.Token)
		if !ok || p.Role != credential.Role || identities[p.ClientID] {
			t.Fatal("invalid or ambiguous principal")
		}
		identities[p.ClientID] = true
		restarted, _ := New(credentials())
		same, ok := restarted.Authenticate("Bearer " + credential.Token)
		if !ok || same != p {
			t.Fatal("unstable credential identity")
		}
		for _, op := range []Operation{ReadWorkspace, ReadTask, ReadInbox, ReadMaterial, Receive, ResolveCreateTask, "provider.execute"} {
			expected := op == ReadWorkspace || op == ReadTask || op == ReadInbox || op == ReadMaterial || (op == Receive && p.Role != Reader) || (op == ResolveCreateTask && p.Role == Coordinator)
			if Allowed(p, op) != expected {
				t.Fatalf("role %s operation %s", p.Role, op)
			}
		}
		roundTrip, ok := FromContext(WithPrincipal(context.Background(), p))
		if !ok || roundTrip != p {
			t.Fatal("principal discarded at boundary")
		}
	}
	for _, header := range []string{"", "bearer " + strings.Repeat("1", 64), "Bearer " + strings.Repeat("a", 64), "Bearer " + strings.Repeat("1", 64) + ",Bearer " + strings.Repeat("2", 64), "Bearer " + strings.Repeat("1", 64) + " ", "Bearer " + strings.Repeat("A", 64)} {
		if _, ok := auth.Authenticate(header); ok {
			t.Fatal("accepted invalid authentication")
		}
	}
	if Allowed(Principal{ClientID: "claimed-provider", Role: Coordinator}, ResolveCreateTask) {
		t.Fatal("claimed identity authorized")
	}
}
func TestInvalidOrAmbiguousCredentialSetupIsClosed(t *testing.T) {
	cases := [][]Credential{nil, credentials()[:2], {{Reader, strings.Repeat("1", 64)}, {Reader, strings.Repeat("2", 64)}, {Coordinator, strings.Repeat("3", 64)}}, {{Reader, strings.Repeat("1", 64)}, {Worker, strings.Repeat("1", 64)}, {Coordinator, strings.Repeat("3", 64)}}, {{"admin", strings.Repeat("1", 64)}, {Worker, strings.Repeat("2", 64)}, {Coordinator, strings.Repeat("3", 64)}}}
	for _, c := range cases {
		if auth, err := New(c); err == nil || auth != nil {
			t.Fatal("unsafe credentials accepted")
		}
	}
	var auth *Authenticator
	if _, ok := auth.Authenticate("Bearer " + strings.Repeat("1", 64)); ok {
		t.Fatal("nil authenticator authorized")
	}
}
