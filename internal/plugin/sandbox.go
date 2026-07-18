package plugin

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	lua "github.com/yuin/gopher-lua"
)

// openSandboxLibs opens only the safe subset of the Lua standard library:
// base, table, string, and math. os, io, package (require), debug, coroutine,
// and channel are deliberately not opened. It then withholds the filesystem and
// module-loading escapes that OpenBase installs as globals.
func openSandboxLibs(L *lua.LState) {
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.open))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	// OpenBase registers these as globals; remove the ones that reach the
	// filesystem or module loader. os/io/debug/package are never opened.
	for _, name := range []string{"dofile", "loadfile", "require"} {
		L.SetGlobal(name, lua.LNil)
	}
}

// KeyRef is a plugin keybinding: the Bubble Tea key string, a description, and
// an opaque reference to the owning plugin and its Lua handler.
type KeyRef struct {
	Key         string
	Description string
	p           *plugin
	fn          *lua.LFunction
}

// luaCommand adapts a Lua-registered command to the commands.Command interface,
// so it needs no registry changes and inherits completion and /help for free.
type luaCommand struct {
	name string
	desc string
	p    *plugin
	fn   *lua.LFunction
}

func (c *luaCommand) Name() string        { return c.name }
func (c *luaCommand) Description() string { return c.desc }

func (c *luaCommand) Execute(args []string) (tea.Cmd, error) {
	p, fn := c.p, c.fn
	a := append([]string(nil), args...)
	return func() tea.Msg {
		return effectsMsg(p.run(invocation{kind: invCommand, fn: fn, args: a}))
	}, nil
}

// registerAPI installs the `marga` global table with the registration and
// effect host functions, each closed over this plugin.
func (p *plugin) registerAPI() {
	L := p.L
	mod := L.NewTable()
	L.SetGlobal("marga", mod)
	L.SetField(mod, "command", L.NewFunction(p.apiCommand))
	L.SetField(mod, "keybind", L.NewFunction(p.apiKeybind))
	L.SetField(mod, "on_message", L.NewFunction(p.apiOnMessage))
	L.SetField(mod, "reply", L.NewFunction(p.apiReply))
	L.SetField(mod, "send", L.NewFunction(p.apiSend))
	L.SetField(mod, "set_input", L.NewFunction(p.apiSetInput))
	L.SetField(mod, "notify", L.NewFunction(p.apiNotify))
}

// apiCommand implements marga.command{name=, description=, run=function(args)}.
func (p *plugin) apiCommand(L *lua.LState) int {
	tbl := L.CheckTable(1)
	name := strings.TrimSpace(strings.TrimPrefix(optString(tbl.RawGetString("name")), "/"))
	desc := strings.TrimSpace(optString(tbl.RawGetString("description")))
	fn, ok := tbl.RawGetString("run").(*lua.LFunction)
	if name == "" || !ok {
		p.logger.Printf("plugin %q: marga.command requires a name and a run function; skipping", p.name)
		return 0
	}
	if desc == "" {
		desc = "plugin command (" + p.name + ")"
	}
	p.commands = append(p.commands, &luaCommand{name: name, desc: desc, p: p, fn: fn})
	return 0
}

// apiKeybind implements marga.keybind{key=, description=, run=function()}.
func (p *plugin) apiKeybind(L *lua.LState) int {
	tbl := L.CheckTable(1)
	key := strings.TrimSpace(strings.ToLower(optString(tbl.RawGetString("key"))))
	desc := strings.TrimSpace(optString(tbl.RawGetString("description")))
	fn, ok := tbl.RawGetString("run").(*lua.LFunction)
	if !ok {
		p.logger.Printf("plugin %q: marga.keybind requires a run function; skipping", p.name)
		return 0
	}
	if err := validateKeybind(key); err != nil {
		p.logger.Printf("plugin %q: marga.keybind %q rejected: %v", p.name, key, err)
		return 0
	}
	if desc == "" {
		desc = "plugin keybinding (" + p.name + ")"
	}
	p.keybinds = append(p.keybinds, KeyRef{Key: key, Description: desc, p: p, fn: fn})
	return 0
}

// apiOnMessage implements marga.on_message(function(msg)).
func (p *plugin) apiOnMessage(L *lua.LState) int {
	fn := L.CheckFunction(1)
	p.hooks = append(p.hooks, fn)
	return 0
}

func (p *plugin) apiReply(L *lua.LState) int {
	p.effects = append(p.effects, replyEffect{content: L.CheckString(1)})
	return 0
}

func (p *plugin) apiSend(L *lua.LState) int {
	p.effects = append(p.effects, sendEffect{content: L.CheckString(1)})
	return 0
}

func (p *plugin) apiSetInput(L *lua.LState) int {
	p.effects = append(p.effects, setInputEffect{value: L.CheckString(1)})
	return 0
}

func (p *plugin) apiNotify(L *lua.LState) int {
	p.effects = append(p.effects, notifyEffect{title: L.CheckString(1), body: L.OptString(2, "")})
	return 0
}

// reservedKeys are keys a plugin may never bind: they carry global meaning that
// must not be overridden. (Other bare keys are rejected by the modifier rule.)
var reservedKeys = map[string]bool{
	"ctrl+c": true, // quit
	"ctrl+q": true, // quit
	"enter":  true, // submit
	"esc":    true, // cancel / dismiss
	"tab":    true, // completion
	"up":     true,
	"down":   true,
	"left":   true,
	"right":  true,
	"pgup":   true,
	"pgdown": true,
}

// validateKeybind rejects reserved keys and keys without a ctrl+/alt+ modifier,
// so plugins cannot capture bare typing or override core controls. Built-ins
// still win regardless, since the host consults plugin keys only after its own
// switch.
func validateKeybind(key string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if reservedKeys[key] {
		return fmt.Errorf("key is reserved by Marga")
	}
	if !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+") {
		return fmt.Errorf("key must include a ctrl+ or alt+ modifier")
	}
	return nil
}

// optString returns the string value of v, or "" if it is not a Lua string.
func optString(v lua.LValue) string {
	if s, ok := v.(lua.LString); ok {
		return string(s)
	}
	return ""
}

// argsTable marshals command arguments into a 1-based Lua array.
func argsTable(L *lua.LState, args []string) *lua.LTable {
	t := L.NewTable()
	for _, a := range args {
		t.Append(lua.LString(a))
	}
	return t
}

// hookTable marshals a HookMessage into the Lua table passed to on_message.
func hookTable(L *lua.LState, msg *HookMessage) *lua.LTable {
	t := L.NewTable()
	if msg != nil {
		L.SetField(t, "channel", lua.LString(msg.Channel))
		L.SetField(t, "username", lua.LString(msg.Username))
		L.SetField(t, "content", lua.LString(msg.Content))
		L.SetField(t, "is_self", lua.LBool(msg.IsSelf))
		L.SetField(t, "is_system", lua.LBool(msg.IsSystem))
	}
	return t
}
