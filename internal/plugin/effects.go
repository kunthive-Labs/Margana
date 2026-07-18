package plugin

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/kunthive-Labs/Margana/internal/commands"
	"github.com/kunthive-Labs/Margana/internal/model"
)

// Effect is a side effect requested by a Lua handler. Handlers never mutate the
// TUI directly: they append effects (via the marga.* host functions) which the
// host converts, on the Bubble Tea Update loop, into existing messages. This
// keeps all Model mutation on the Update loop and all Lua off it.
type Effect interface{ effect() }

// replyEffect prints content into the chat as a system line (marga.reply).
type replyEffect struct{ content string }

// sendEffect sends content to the current channel as the user (marga.send).
type sendEffect struct{ content string }

// setInputEffect replaces the input line's contents (marga.set_input).
type setInputEffect struct{ value string }

// notifyEffect shows an OS desktop notification (marga.notify).
type notifyEffect struct {
	title string
	body  string
}

func (replyEffect) effect()    {}
func (sendEffect) effect()     {}
func (setInputEffect) effect() {}
func (notifyEffect) effect()   {}

// HookMessage is the network-neutral view of a chat message passed to
// marga.on_message handlers. IsSelf/IsSystem let the host filter loops and
// noise; hooks are not invoked for self or system messages by default.
type HookMessage struct {
	Channel  string
	Username string
	Content  string
	IsSelf   bool
	IsSystem bool
}

// effectsToCmd converts a batch of effects into a tea.Cmd built from existing
// message types. A single effect collapses to that effect's command; an empty
// batch yields nil.
func effectsToCmd(effects []Effect) tea.Cmd {
	if len(effects) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(effects))
	for _, e := range effects {
		switch ef := e.(type) {
		case replyEffect:
			out := commands.SystemMsg(ef.content)
			cmds = append(cmds, func() tea.Msg {
				return commands.CommandOutputMsg{Messages: []model.Message{out}}
			})
		case sendEffect:
			content := ef.content
			cmds = append(cmds, func() tea.Msg {
				return commands.SendRawMsg{Content: content}
			})
		case setInputEffect:
			value := ef.value
			cmds = append(cmds, func() tea.Msg {
				return commands.SetInputMsg{Value: value}
			})
		case notifyEffect:
			title, body := ef.title, ef.body
			cmds = append(cmds, func() tea.Msg {
				return commands.NotifyMsg{Title: title, Body: body}
			})
		}
	}
	return tea.Batch(cmds...)
}

// effectsMsg runs the effect batch and returns its message. Bubble Tea unpacks
// a returned BatchMsg into its constituent commands, so a multi-effect handler
// still dispatches every message.
func effectsMsg(effects []Effect) tea.Msg {
	cmd := effectsToCmd(effects)
	if cmd == nil {
		return nil
	}
	return cmd()
}
