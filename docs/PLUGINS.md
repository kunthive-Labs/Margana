# Plugins (Lua scripting)

Marga can be extended with **Lua scripts** that add custom slash commands,
keybindings, and message-hook bots — the same mental model as WeeChat, irssi, or
Neovim scripting. Scripts run in a pure-Go, CGo-free Lua VM
([gopher-lua](https://github.com/yuin/gopher-lua)), so plugins add no native
dependencies and ship in the single Marga binary.

A worked example lives in [`docs/plugins/roll.lua`](plugins/roll.lua).

## Enabling plugins

Plugins are **off by default**. Turn them on in your `config.toml`:

```toml
[plugins]
enabled = true
# Base directory for relative plugin paths.
# Defaults to <config-dir>/plugins (e.g. ~/.config/marga/plugins on Linux).
dir = ""

# List each script as its own [[plugins.entries]] block. `path` is absolute,
# or relative to `dir`. Set enabled = false to keep a script installed but off.
[[plugins.entries]]
path = "roll.lua"
enabled = true
```

Environment overrides: `MARGA_PLUGINS_ENABLED` (`true`/`false`) and
`MARGA_PLUGINS_DIR`.

> **Why `[[plugins.entries]]` and not `[[plugins]]`?** In TOML a key can be a
> table *or* an array of tables, not both. Since `[plugins]` holds the `enabled`
> and `dir` settings, the per-script list is nested under it as
> `[[plugins.entries]]`.

A plugin that fails to load (syntax error, missing file, a rejected
registration) is logged and skipped — it never aborts Marga's startup or affects
other plugins. Enable logging (`marga --debug`) to see why a plugin was skipped.

## The `marga` API

Every script runs with a global `marga` table. There are two groups of
functions: **registration** functions (call these at the top level of your
script, when it loads) and **effect** functions (call these from inside a
handler to make something happen).

### Registration

#### `marga.command{ name=, description=, run=function(args) ... end }`

Registers a slash command. `args` is a 1-based array of the whitespace-separated
arguments the user typed after `/name`. The command shows up in `/help` and in
tab-completion automatically.

```lua
marga.command{
  name = "roll",
  description = "roll an N-sided die: /roll [sides]",
  run = function(args)
    local sides = math.floor(tonumber(args[1]) or 6)
    marga.reply(string.format("🎲 rolled %d (d%d)", math.random(1, sides), sides))
  end,
}
```

#### `marga.keybind{ key=, description=, run=function() ... end }`

Binds a key to a handler. The `key` is a
[Bubble Tea key string](https://github.com/charmbracelet/bubbletea) such as
`"ctrl+j"` or `"alt+p"`.

Rules:

- The key **must include a `ctrl+` or `alt+` modifier** (so plugins can't
  capture ordinary typing).
- Marga's **reserved keys are refused**: `ctrl+c`, `ctrl+q`, `enter`, `esc`,
  `tab`, and the arrow / page keys.
- **Built-ins always win.** Marga consults plugin keys only after its own
  keybindings, so binding a key Marga already uses (e.g. `ctrl+b`) simply has no
  effect.

```lua
marga.keybind{
  key = "ctrl+j",
  description = "insert a shrug",
  run = function() marga.set_input([[¯\_(ツ)_/¯]]) end,
}
```

#### `marga.on_message(function(msg) ... end)`

Registers a bot that runs on each incoming chat message. The `msg` table has:

| field       | type    | meaning                                     |
|-------------|---------|---------------------------------------------|
| `channel`   | string  | channel the message arrived in              |
| `username`  | string  | sender's display name                       |
| `content`   | string  | message text                                |
| `is_self`   | boolean | true if you sent it                         |
| `is_system` | boolean | true for Marga's own system lines           |

Your own messages and system messages **do not trigger hooks** (this prevents
feedback loops), so a `ping → pong` bot is safe:

```lua
marga.on_message(function(msg)
  if msg.content == "ping" then
    marga.send("pong")
  end
end)
```

### Effects

Call these from inside a `run`/`on_message` handler. They queue an action that
Marga performs after your handler returns.

| function                 | effect                                                        |
|--------------------------|---------------------------------------------------------------|
| `marga.reply(text)`      | print `text` into the chat as a local system line (not sent)  |
| `marga.send(text)`       | send `text` to the current channel as you                     |
| `marga.set_input(text)`  | replace the input line's contents with `text`                 |
| `marga.notify(title[, body])` | show an OS desktop notification                          |

A handler may call several effects; they are applied in order.

## Sandbox & safety

Plugins are sandboxed:

- Only the **base**, **table**, **string**, and **math** standard libraries are
  available. `os`, `io`, `require`, `package`, `debug`, `dofile`, and `loadfile`
  are withheld — a plugin cannot touch the filesystem, spawn processes, or load
  other modules.
- Each plugin owns one Lua state confined to its own goroutine, so scripts can
  never corrupt Marga's UI state; all UI changes happen through the effects
  above, on Marga's main loop.
- Every call (load and handler) is bounded by a **timeout** (3s by default), so a
  runaway script — even `while true do end` — is cancelled instead of hanging
  Marga.

## Troubleshooting

- **Nothing happens / command missing:** confirm `[plugins].enabled = true`, the
  entry's `enabled = true`, and the `path` (absolute, or relative to
  `[plugins].dir`). Run `marga --debug` and check the log for a
  `plugin "..." load failed` line.
- **A keybinding does nothing:** it may be reserved or shadowed by a built-in, or
  it may lack a `ctrl+`/`alt+` modifier — the log records rejected bindings.
- **A handler stops midway:** it likely hit the call timeout; avoid unbounded
  loops and heavy work in handlers.
