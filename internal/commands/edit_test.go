package commands

import "testing"

func TestEditCommandStartsPickerWithoutArgs(t *testing.T) {
	cmd := NewEditCmd()
	msgFn, err := cmd.Execute(nil)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	msg := msgFn()
	if _, ok := msg.(StartEditMsg); !ok {
		t.Fatalf("expected StartEditMsg, got %T", msg)
	}
}

func TestEditCommandStartsTargetedPrefillWithoutContent(t *testing.T) {
	cmd := NewEditCmd()
	msgFn, err := cmd.Execute([]string{"last"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	msg := msgFn()
	start, ok := msg.(StartEditMsg)
	if !ok {
		t.Fatalf("expected StartEditMsg, got %T", msg)
	}
	if start.Target != "last" {
		t.Fatalf("expected target 'last', got %q", start.Target)
	}
}
