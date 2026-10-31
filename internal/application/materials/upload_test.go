package materials

import (
	"context"
	"errors"
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/failure"
	"strings"
	"testing"
)

type memory struct {
	receipt               *Receipt
	state                 TaskState
	puts, inserts, checks int
}

func (m *memory) WithinMaterials(ctx context.Context, f func(Tx) error) error { return f(m) }
func (m *memory) FindMaterialReceipt(context.Context, string, string) (Receipt, bool, error) {
	if m.receipt != nil {
		return *m.receipt, true, nil
	}
	return Receipt{}, false, nil
}
func (m *memory) MaterialTask(context.Context, string) (TaskState, error) {
	m.checks++
	return m.state, nil
}
func (m *memory) Revision(context.Context, bool) (int64, error) { return 4, nil }
func (m *memory) InsertMaterial(context.Context, Record) error  { m.inserts++; return nil }
func (m *memory) InsertMaterialReceipt(_ context.Context, _, _ string, r Receipt) error {
	m.receipt = &r
	return nil
}
func (m *memory) Put(context.Context, string, []byte) error { m.puts++; return nil }
func input() Upload {
	return Upload{ExpectedTaskRevision: 1, MediaType: "text/plain", Text: "Unicode 👋\n", AppliedBy: access.Principal{ClientID: "client-" + strings.Repeat("1", 32), Role: access.Coordinator}}
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var f *failure.Failure
	if !errors.As(err, &f) || f.Code != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}
func TestAuthorizationReplayNormalizationAndRefusalBeforeBytes(t *testing.T) {
	m := &memory{state: TaskState{Revision: 1}}
	s := New(m, m, nil)
	in := input()
	first, status, err := s.UploadMaterial(context.Background(), "T-one", "key", in)
	if err != nil || status != 201 || m.puts != 1 || m.inserts != 1 {
		t.Fatal(err)
	}
	m.state = TaskState{Revision: 99, Associations: 32}
	replay, _, err := s.UploadMaterial(context.Background(), "T-one", "key", in)
	if err != nil || replay != first || m.puts != 1 || m.checks != 1 {
		t.Fatal("replay did not precede state and file checks")
	}
	in.Text += "changed"
	_, _, err = s.UploadMaterial(context.Background(), "T-one", "key", in)
	code(t, err, "idempotency_conflict")
	in = input()
	in.AppliedBy.Role = access.Worker
	_, _, err = s.UploadMaterial(context.Background(), "T-one", "key", in)
	code(t, err, "forbidden")
	for _, state := range []TaskState{{Revision: 2}, {Revision: 1, Associations: 32}} {
		m = &memory{state: state}
		_, _, err = New(m, m, nil).UploadMaterial(context.Background(), "T-one", "key", input())
		if err == nil || m.puts != 0 {
			t.Fatal("refusal wrote bytes")
		}
	}
}
func TestTextAndInputBoundaries(t *testing.T) {
	for _, text := range []string{"", strings.Repeat("🙂", MaxCharacters), "a\t\r\nб"} {
		if !ValidText(text) {
			t.Fatal("valid Unicode rejected")
		}
	}
	for _, text := range []string{strings.Repeat("x", MaxCharacters+1), "\x00", "\x7f", "\u0085", string([]byte{0xff})} {
		if ValidText(text) {
			t.Fatal("invalid text accepted")
		}
	}
	for _, change := range []func(*Upload){func(i *Upload) { i.MediaType = "text/html" }, func(i *Upload) { i.ExpectedTaskRevision = 0 }, func(i *Upload) { s := "  "; i.Label = &s }, func(i *Upload) { s := strings.Repeat("x", 257); i.Label = &s }, func(i *Upload) { i.Text = "\x00" }} {
		in := input()
		change(&in)
		m := &memory{}
		_, _, err := New(m, m, nil).UploadMaterial(context.Background(), "T-one", "key", in)
		code(t, err, "invalid_request")
		if m.puts != 0 || m.checks != 0 {
			t.Fatal("invalid command reached state")
		}
	}
}
