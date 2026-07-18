-- roll.lua — a worked example Marga plugin.
--
-- It demonstrates all three extension points:
--   * a slash command   (/roll)
--   * a keybinding       (ctrl+j)
--   * a message-hook bot  (ping -> pong)
--
-- Enable it in config.toml:
--
--   [plugins]
--   enabled = true
--   # dir defaults to <config-dir>/plugins
--
--   [[plugins.entries]]
--   path = "roll.lua"
--   enabled = true
--
-- The VM is sandboxed: only the base, table, string, and math standard
-- libraries are available. os, io, require, dofile, and friends are withheld.
-- See docs/PLUGINS.md for the full API reference.

-- /roll [sides] — roll an N-sided die (default 6) and print the result into
-- the chat as a local system line (not sent to anyone).
marga.command{
	name = "roll",
	description = "roll an N-sided die: /roll [sides]",
	run = function(args)
		local sides = math.floor(tonumber(args[1]) or 6)
		if sides < 1 then
			sides = 6
		end
		local n = math.random(1, sides)
		marga.reply(string.format("🎲 rolled %d (d%d)", n, sides))
	end,
}

-- ctrl+j — insert a shrug into the input line. Keybindings must carry a ctrl+
-- or alt+ modifier and may not override Marga's reserved keys.
marga.keybind{
	key = "ctrl+j",
	description = "insert a shrug",
	run = function()
		marga.set_input([[¯\_(ツ)_/¯]])
	end,
}

-- A tiny bot: reply "pong" whenever someone says exactly "ping". Your own
-- messages and system messages never trigger hooks, so this cannot loop.
marga.on_message(function(msg)
	if msg.content == "ping" then
		marga.send("pong")
	end
end)
