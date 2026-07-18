// Package plugin implements Marga's Lua scripting surface. Users add custom
// slash commands, keybindings, and message-hook bots as Lua scripts, evaluated
// by the pure-Go, CGo-free gopher-lua VM.
//
// Each plugin owns exactly one *lua.LState, confined to its own goroutine and
// driven by a request channel, because an LState is not goroutine-safe. Handlers
// never touch the TUI: they append Effects (via the marga.* host functions),
// which the host converts, on the Bubble Tea Update loop, into existing message
// types. All Model mutation stays on the Update loop; all Lua runs off it.
//
// VMs are sandboxed: only the base/table/string/math standard libraries are
// opened, and os/io/package(require)/loadfile/dofile/debug are withheld. Every
// call is bounded by a context timeout so a runaway script cannot hang Marga,
// and panics are recovered. A plugin that fails to load is logged and skipped;
// it never aborts startup.
package plugin

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/commands"
	lua "github.com/yuin/gopher-lua"
)

// defaultTimeout bounds a single Lua call (load or handler). A script that
// exceeds it is cancelled via the LState's context.
const defaultTimeout = 3 * time.Second

// Options configures a Manager.
type Options struct {
	// Dir is the base directory for relative plugin paths.
	Dir string
	// Files lists the plugin scripts to load, in order.
	Files []FileSpec
	// Timeout bounds a single Lua call. Zero uses defaultTimeout.
	Timeout time.Duration
}

// FileSpec identifies one plugin script and whether it is enabled.
type FileSpec struct {
	Path    string
	Enabled bool
}

// Manager owns the loaded plugins and exposes their registrations to the host.
type Manager struct {
	plugins []*plugin
	logger  *log.Logger
}

// New loads every enabled plugin in opts.Files. A script that fails to load is
// logged and skipped; New always returns a usable Manager (never nil, never an
// error), so one bad plugin can never abort startup.
func New(opts Options, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	m := &Manager{logger: logger}
	for _, f := range opts.Files {
		if !f.Enabled {
			continue
		}
		path := f.Path
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) && opts.Dir != "" {
			path = filepath.Join(opts.Dir, path)
		}
		p, err := loadPlugin(path, timeout, logger)
		if err != nil {
			logger.Printf("plugin %q: load failed, skipping: %v", path, err)
			continue
		}
		m.plugins = append(m.plugins, p)
		logger.Printf("plugin %q loaded: %d command(s), %d keybind(s), %d hook(s)",
			p.name, len(p.commands), len(p.keybinds), len(p.hooks))
	}
	return m
}

// RegisterCommands registers every plugin command with the host command
// registry. Completion and /help come for free via registry.List().
func (m *Manager) RegisterCommands(reg *commands.Registry) {
	if m == nil || reg == nil {
		return
	}
	for _, p := range m.plugins {
		for _, c := range p.commands {
			reg.Register(c)
		}
	}
}

// Keybindings returns the plugin-bound keys, mapped by their Bubble Tea
// key string (e.g. "ctrl+j"). On conflict, a later plugin wins.
func (m *Manager) Keybindings() map[string]KeyRef {
	if m == nil {
		return nil
	}
	out := make(map[string]KeyRef)
	for _, p := range m.plugins {
		for _, kb := range p.keybinds {
			out[kb.Key] = kb
		}
	}
	return out
}

// RunKey returns a command that invokes the keybinding's Lua handler off the
// Update loop and dispatches its effects. Returns nil for a zero KeyRef.
func (m *Manager) RunKey(ref KeyRef) tea.Cmd {
	if m == nil || ref.p == nil || ref.fn == nil {
		return nil
	}
	p, fn := ref.p, ref.fn
	return func() tea.Msg {
		return effectsMsg(p.run(invocation{kind: invKeybind, fn: fn}))
	}
}

// HasMessageHooks reports whether any plugin registered an on_message handler,
// letting the host skip the per-message work when none exist.
func (m *Manager) HasMessageHooks() bool {
	if m == nil {
		return false
	}
	for _, p := range m.plugins {
		if len(p.hooks) > 0 {
			return true
		}
	}
	return false
}

// RunMessageHooks returns a command that runs every plugin's on_message
// handlers off the Update loop and dispatches their effects. Self and system
// messages are skipped by default to avoid feedback loops. Returns nil when
// there is nothing to do.
func (m *Manager) RunMessageHooks(msg HookMessage) tea.Cmd {
	if m == nil || msg.IsSelf || msg.IsSystem {
		return nil
	}
	type hookRef struct {
		p  *plugin
		fn *lua.LFunction
	}
	var refs []hookRef
	for _, p := range m.plugins {
		for _, fn := range p.hooks {
			refs = append(refs, hookRef{p: p, fn: fn})
		}
	}
	if len(refs) == 0 {
		return nil
	}
	hm := msg
	return func() tea.Msg {
		var all []Effect
		for _, r := range refs {
			all = append(all, r.p.run(invocation{kind: invHook, fn: r.fn, hookMsg: &hm})...)
		}
		return effectsMsg(all)
	}
}

// Close shuts down every plugin goroutine and frees its LState. Safe to call
// once; safe on a nil Manager.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	for _, p := range m.plugins {
		p.close()
	}
}

// invKind selects how an invocation's arguments are marshalled to Lua.
type invKind int

const (
	invCommand invKind = iota
	invKeybind
	invHook
)

// invocation is one request to a plugin goroutine to call a Lua function.
type invocation struct {
	kind    invKind
	fn      *lua.LFunction
	args    []string     // command arguments
	hookMsg *HookMessage // message-hook payload
	respCh  chan []Effect
}

// plugin owns one LState, confined to loop()'s goroutine.
type plugin struct {
	name    string
	logger  *log.Logger
	timeout time.Duration

	L      *lua.LState
	reqCh  chan invocation
	done   chan struct{}
	closer sync.Once

	// effects accumulates the current invocation's side effects. It is only
	// ever touched on the plugin goroutine.
	effects []Effect

	// Registrations, populated during load and read-only thereafter.
	commands []*luaCommand
	keybinds []KeyRef
	hooks    []*lua.LFunction
}

// loadPlugin creates a sandboxed LState, runs the script (bounded by timeout to
// prevent a startup hang), then starts the plugin's goroutine. The load runs on
// the caller's goroutine before loop() starts, so all registration host calls
// are single-threaded.
func loadPlugin(path string, timeout time.Duration, logger *log.Logger) (*plugin, error) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	openSandboxLibs(L)

	p := &plugin{
		name:    pluginName(path),
		logger:  logger,
		timeout: timeout,
		L:       L,
		reqCh:   make(chan invocation),
		done:    make(chan struct{}),
	}
	p.registerAPI()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	L.SetContext(ctx)
	err := L.DoFile(path)
	cancel()
	L.RemoveContext()
	if err != nil {
		L.Close()
		return nil, err
	}

	go p.loop()
	return p, nil
}

// loop is the plugin's single goroutine: it owns the LState and serializes all
// invocations onto it.
func (p *plugin) loop() {
	defer p.L.Close()
	for {
		select {
		case <-p.done:
			return
		case inv := <-p.reqCh:
			inv.respCh <- p.invoke(inv)
		}
	}
}

// invoke calls the requested Lua function on the plugin goroutine, bounded by a
// timeout and guarded by recover, and returns the effects it accumulated. It
// always returns the effects collected so far, even on error or panic.
func (p *plugin) invoke(inv invocation) (out []Effect) {
	p.effects = nil
	defer func() {
		if r := recover(); r != nil {
			p.logger.Printf("plugin %q: recovered panic: %v", p.name, r)
		}
		out = p.effects
		p.effects = nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()
	p.L.SetContext(ctx)
	defer p.L.RemoveContext()

	p.L.Push(inv.fn)
	nargs := 0
	switch inv.kind {
	case invCommand:
		p.L.Push(argsTable(p.L, inv.args))
		nargs = 1
	case invHook:
		p.L.Push(hookTable(p.L, inv.hookMsg))
		nargs = 1
	case invKeybind:
		nargs = 0
	}
	if err := p.L.PCall(nargs, 0, nil); err != nil {
		p.logger.Printf("plugin %q: handler error: %v", p.name, err)
	}
	return p.effects
}

// run sends an invocation to the plugin goroutine and waits for its effects. It
// is called off the Update loop (inside a tea.Cmd). A closed plugin returns nil.
func (p *plugin) run(inv invocation) []Effect {
	respCh := make(chan []Effect, 1)
	inv.respCh = respCh
	select {
	case <-p.done:
		return nil
	case p.reqCh <- inv:
	}
	select {
	case <-p.done:
		return nil
	case res := <-respCh:
		return res
	}
}

func (p *plugin) close() {
	p.closer.Do(func() { close(p.done) })
}

// pluginName is the script's base name without its .lua extension.
func pluginName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
