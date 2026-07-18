package plugin

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/commands"
	lua "github.com/yuin/gopher-lua"
)

// tLogWriter routes a plugin's diagnostic log to t.Logf, so load/handler errors
// surface only when a test fails.
type tLogWriter struct{ t *testing.T }

func (w tLogWriter) Write(p []byte) (int, error) {
	w.t.Logf("plugin log: %s", bytes.TrimRight(p, "\n"))
	return len(p), nil
}

// loadManager writes src to a temp .lua file and loads it as a single plugin.
func loadManager(t *testing.T, src string, timeout time.Duration) *Manager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lua")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("writing plugin: %v", err)
	}
	logger := log.New(tLogWriter{t}, "", 0)
	mgr := New(Options{
		Files:   []FileSpec{{Path: path, Enabled: true}},
		Timeout: timeout,
	}, logger)
	t.Cleanup(mgr.Close)
	return mgr
}

// findCommand returns the registered command with the given name, or nil.
func findCommand(reg *commands.Registry, name string) commands.Command {
	for _, c := range reg.List() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestCommandRegistrationAndExecute(t *testing.T) {
	mgr := loadManager(t, `
		marga.command{
			name = "hi",
			description = "say hi",
			run = function(args)
				marga.reply("hello " .. (args[1] or ""))
			end,
		}
	`, time.Second)

	reg := commands.NewRegistry()
	mgr.RegisterCommands(reg)

	cmd := findCommand(reg, "hi")
	if cmd == nil {
		t.Fatal("command /hi was not registered")
	}
	if cmd.Description() != "say hi" {
		t.Errorf("description = %q, want %q", cmd.Description(), "say hi")
	}

	teaCmd, err := cmd.Execute([]string{"world"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	msg := teaCmd()
	out, ok := msg.(commands.CommandOutputMsg)
	if !ok {
		t.Fatalf("message type = %T, want commands.CommandOutputMsg", msg)
	}
	if len(out.Messages) != 1 || out.Messages[0].Content != "hello world" {
		t.Fatalf("unexpected command output: %+v", out.Messages)
	}
}

func TestKeybindRegistrationAndValidation(t *testing.T) {
	mgr := loadManager(t, `
		marga.keybind{ key = "ctrl+j", description = "shrug", run = function() marga.set_input("shrug") end }
		marga.keybind{ key = "ctrl+c", description = "reserved", run = function() end }  -- rejected: reserved
		marga.keybind{ key = "x",      description = "bare",     run = function() end }  -- rejected: no modifier
	`, time.Second)

	keys := mgr.Keybindings()
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 valid keybind, got %d: %v", len(keys), keys)
	}
	ref, ok := keys["ctrl+j"]
	if !ok {
		t.Fatal("ctrl+j should be registered")
	}
	if ref.Description != "shrug" {
		t.Errorf("description = %q, want %q", ref.Description, "shrug")
	}
	if _, ok := keys["ctrl+c"]; ok {
		t.Error("ctrl+c is reserved and must be rejected")
	}
	if _, ok := keys["x"]; ok {
		t.Error("modifier-less key 'x' must be rejected")
	}

	teaCmd := mgr.RunKey(ref)
	if teaCmd == nil {
		t.Fatal("RunKey returned nil for a bound key")
	}
	msg := teaCmd()
	si, ok := msg.(commands.SetInputMsg)
	if !ok || si.Value != "shrug" {
		t.Fatalf("expected commands.SetInputMsg{shrug}, got %T %+v", msg, msg)
	}
}

func TestMessageHookPingPong(t *testing.T) {
	mgr := loadManager(t, `
		marga.on_message(function(msg)
			if msg.content == "ping" then
				marga.send("pong")
			end
		end)
	`, time.Second)

	if !mgr.HasMessageHooks() {
		t.Fatal("expected a registered message hook")
	}

	// A non-self "ping" yields SendRawMsg("pong").
	teaCmd := mgr.RunMessageHooks(HookMessage{Content: "ping", Username: "bob"})
	if teaCmd == nil {
		t.Fatal("RunMessageHooks returned nil for ping")
	}
	raw, ok := teaCmd().(commands.SendRawMsg)
	if !ok || raw.Content != "pong" {
		t.Fatalf("expected commands.SendRawMsg{pong}, got %T %+v", teaCmd(), teaCmd())
	}

	// A self "ping" is skipped entirely (no command), preventing feedback loops.
	if teaCmd := mgr.RunMessageHooks(HookMessage{Content: "ping", Username: "me", IsSelf: true}); teaCmd != nil {
		t.Error("self messages must be skipped")
	}

	// A system "ping" is likewise skipped.
	if teaCmd := mgr.RunMessageHooks(HookMessage{Content: "ping", Username: "system", IsSystem: true}); teaCmd != nil {
		t.Error("system messages must be skipped")
	}

	// A non-matching message runs the hook but produces no effect.
	if teaCmd := mgr.RunMessageHooks(HookMessage{Content: "hello", Username: "bob"}); teaCmd != nil {
		if m := teaCmd(); m != nil {
			t.Errorf("non-matching message should yield no message, got %T", m)
		}
	}
}

func TestRunawayHandlerIsCancelled(t *testing.T) {
	mgr := loadManager(t, `
		marga.command{ name = "spin", description = "loop forever", run = function() while true do end end }
	`, 100*time.Millisecond)

	reg := commands.NewRegistry()
	mgr.RegisterCommands(reg)
	cmd := findCommand(reg, "spin")
	if cmd == nil {
		t.Fatal("command /spin was not registered")
	}

	teaCmd, err := cmd.Execute(nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- teaCmd() }()
	select {
	case <-done:
		// Returned promptly: the context timeout cancelled the infinite loop.
	case <-time.After(5 * time.Second):
		t.Fatal("runaway handler did not return; timeout was not enforced")
	}
}

func TestSandboxWithholdsUnsafeGlobals(t *testing.T) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	openSandboxLibs(L)

	for _, name := range []string{"os", "io", "require", "dofile", "loadfile", "package", "debug"} {
		if v := L.GetGlobal(name); v != lua.LNil {
			t.Errorf("sandbox global %q should be nil, got %v", name, v)
		}
	}
	for _, name := range []string{"string", "table", "math", "print", "pairs", "tostring"} {
		if v := L.GetGlobal(name); v == lua.LNil {
			t.Errorf("expected sandbox global %q to be available", name)
		}
	}
}

func TestShippedRollExampleLoads(t *testing.T) {
	// The worked example under docs/plugins must stay valid: load it and assert
	// it registers its command, keybinding, and hook.
	path, err := filepath.Abs(filepath.Join("..", "..", "docs", "plugins", "roll.lua"))
	if err != nil {
		t.Fatalf("resolving example path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("example not found (%v); skipping", err)
	}
	logger := log.New(tLogWriter{t}, "", 0)
	mgr := New(Options{Files: []FileSpec{{Path: path, Enabled: true}}}, logger)
	t.Cleanup(mgr.Close)

	reg := commands.NewRegistry()
	mgr.RegisterCommands(reg)
	if findCommand(reg, "roll") == nil {
		t.Error("roll.lua should register the /roll command")
	}
	if _, ok := mgr.Keybindings()["ctrl+j"]; !ok {
		t.Error("roll.lua should register the ctrl+j keybinding")
	}
	if !mgr.HasMessageHooks() {
		t.Error("roll.lua should register an on_message hook")
	}

	// The /roll command yields a chat line.
	cmd := findCommand(reg, "roll")
	teaCmd, err := cmd.Execute([]string{"20"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := teaCmd().(commands.CommandOutputMsg); !ok {
		t.Errorf("/roll should yield a CommandOutputMsg, got %T", teaCmd())
	}
}

func TestConcurrentInvocationsAreSerialized(t *testing.T) {
	// A single plugin's LState is confined to one goroutine, so concurrent
	// callers must be serialized through it without data races. Run under -race.
	mgr := loadManager(t, `
		local n = 0
		marga.command{ name = "inc", description = "bump", run = function()
			n = n + 1
			marga.reply(tostring(n))
		end }
		marga.on_message(function(msg) marga.send("ack:" .. msg.content) end)
	`, time.Second)

	reg := commands.NewRegistry()
	mgr.RegisterCommands(reg)
	cmd := findCommand(reg, "inc")
	if cmd == nil {
		t.Fatal("command /inc was not registered")
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			teaCmd, _ := cmd.Execute(nil)
			if _, ok := teaCmd().(commands.CommandOutputMsg); !ok {
				t.Errorf("expected CommandOutputMsg from /inc")
			}
		}()
		go func(n int) {
			defer wg.Done()
			teaCmd := mgr.RunMessageHooks(HookMessage{Content: strconv.Itoa(n), Username: "bob"})
			if raw, ok := teaCmd().(commands.SendRawMsg); !ok || raw.Content == "" {
				t.Errorf("expected SendRawMsg from hook, got %T", teaCmd())
			}
		}(i)
	}
	wg.Wait()
}

func TestBadPluginIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.lua")
	bad := filepath.Join(dir, "bad.lua")
	if err := os.WriteFile(good, []byte(`marga.command{name="ok", description="d", run=function() end}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Syntax error: must be logged and skipped, not abort loading of the good one.
	if err := os.WriteFile(bad, []byte(`this is not lua ((`), 0o644); err != nil {
		t.Fatal(err)
	}
	logger := log.New(tLogWriter{t}, "", 0)
	mgr := New(Options{Files: []FileSpec{
		{Path: bad, Enabled: true},
		{Path: good, Enabled: true},
		{Path: filepath.Join(dir, "missing.lua"), Enabled: true},
		{Path: good, Enabled: false}, // disabled: ignored
	}}, logger)
	t.Cleanup(mgr.Close)

	reg := commands.NewRegistry()
	mgr.RegisterCommands(reg)
	if findCommand(reg, "ok") == nil {
		t.Fatal("the good plugin should still load when a sibling is broken")
	}
}
