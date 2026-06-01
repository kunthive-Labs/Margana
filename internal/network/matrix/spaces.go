package matrix

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/kunthive-Labs/Margana/internal/network"
)

// Matrix groups rooms with "spaces" (rooms whose m.room.create type is m.space,
// linked to member rooms via m.space.child state). Marga surfaces each joined
// space as a network.Server, plus a "home" server (the homeserver itself) for
// rooms that belong to no joined space. Clients that don't switch servers still
// get every joined room via ListChannels with an empty serverID.

// roomTopology is the grouping of a user's joined rooms into spaces plus an
// "ungrouped" bucket for rooms that belong to no joined space.
type roomTopology struct {
	spaceOrder      []string            // space room IDs, sorted by display name
	spaceName       map[string]string   // space room ID -> display name
	childrenBySpace map[string][]string // space room ID -> joined child room IDs
	ungrouped       []string            // joined non-space rooms in no joined space
	allRooms        []string            // every joined non-space room, sorted
	names           map[string]string   // room ID -> display name
}

// buildRoomTopology assembles the space grouping from already-fetched data. It
// is pure (no network) so the grouping rules stay unit-testable. Ordering is
// deterministic: spaces and rooms sort by display name, then by ID. Children
// are limited to rooms the user has actually joined; nested spaces are not
// listed as channels (they appear as their own server).
func buildRoomTopology(joined []string, isSpace map[string]bool, spaceChildren map[string][]string, names map[string]string) roomTopology {
	nameOf := func(roomID string) string {
		if n := names[roomID]; n != "" {
			return n
		}
		return roomID
	}
	joinedSet := make(map[string]bool, len(joined))
	for _, r := range joined {
		joinedSet[r] = true
	}

	topo := roomTopology{
		spaceName:       map[string]string{},
		childrenBySpace: map[string][]string{},
		names:           names,
	}

	grouped := map[string]bool{}
	for _, sp := range joined {
		if !isSpace[sp] {
			continue
		}
		topo.spaceOrder = append(topo.spaceOrder, sp)
		topo.spaceName[sp] = nameOf(sp)
		var children []string
		for _, child := range spaceChildren[sp] {
			if joinedSet[child] && !isSpace[child] {
				children = append(children, child)
				grouped[child] = true
			}
		}
		sortRoomsByName(children, nameOf)
		topo.childrenBySpace[sp] = children
	}
	sortRoomsByName(topo.spaceOrder, nameOf)

	for _, r := range joined {
		if isSpace[r] {
			continue
		}
		topo.allRooms = append(topo.allRooms, r)
		if !grouped[r] {
			topo.ungrouped = append(topo.ungrouped, r)
		}
	}
	sortRoomsByName(topo.allRooms, nameOf)
	sortRoomsByName(topo.ungrouped, nameOf)
	return topo
}

func sortRoomsByName(rooms []string, nameOf func(string) string) {
	sort.SliceStable(rooms, func(i, j int) bool {
		ni, nj := nameOf(rooms[i]), nameOf(rooms[j])
		if ni == nj {
			return rooms[i] < rooms[j]
		}
		return ni < nj
	})
}

// hostName renders the homeserver URL as a bare host for display.
func hostName(homeserver string) string {
	name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(homeserver, "https://"), "http://"), "/")
	if name == "" {
		return "matrix"
	}
	return name
}

// ListServers surfaces the homeserver as a "home" server plus one server per
// joined space. Without a connection (or if discovery fails) it degrades to the
// single home server, so a flat client keeps working.
func (a *Adapter) ListServers(ctx context.Context) ([]network.Server, error) {
	home := network.Server{ID: a.homeserver, Name: hostName(a.homeserver)}
	if a.client == nil {
		return []network.Server{home}, nil
	}
	topo, err := a.fetchTopology(ctx)
	if err != nil {
		return []network.Server{home}, nil
	}
	servers := make([]network.Server, 0, len(topo.spaceOrder)+1)
	servers = append(servers, home)
	for _, sp := range topo.spaceOrder {
		servers = append(servers, network.Server{ID: sp, Name: topo.spaceName[sp]})
	}
	return servers, nil
}

// ListChannels returns rooms for a server:
//   - serverID == ""           → every joined room (flat, back-compatible)
//   - serverID == homeserver   → rooms in no joined space
//   - serverID == a space's ID → that space's joined member rooms
func (a *Adapter) ListChannels(ctx context.Context, serverID string) ([]network.ChannelRef, error) {
	if a.client == nil {
		return nil, fmt.Errorf("matrix: not connected")
	}
	// The flat path stays cheap (one round of room listing), which is what the
	// TUI uses today; space grouping is only computed when a server is named.
	if serverID == "" {
		return a.flatChannels(ctx)
	}

	topo, err := a.fetchTopology(ctx)
	if err != nil {
		return a.flatChannels(ctx)
	}

	var roomIDs []string
	if serverID == a.homeserver {
		roomIDs = topo.ungrouped
	} else {
		roomIDs = topo.childrenBySpace[serverID]
	}

	refs := make([]network.ChannelRef, 0, len(roomIDs))
	for _, rid := range roomIDs {
		refs = append(refs, network.ChannelRef{Network: ID, ServerID: serverID, ID: rid, Name: topo.names[rid]})
	}
	return refs, nil
}

// flatChannels lists every joined room as a channel, ignoring space grouping.
func (a *Adapter) flatChannels(ctx context.Context) ([]network.ChannelRef, error) {
	resp, err := a.client.JoinedRooms(ctx)
	if err != nil {
		return nil, err
	}
	refs := make([]network.ChannelRef, 0, len(resp.JoinedRooms))
	for _, roomID := range resp.JoinedRooms {
		refs = append(refs, network.ChannelRef{Network: ID, ID: roomID.String(), Name: a.roomName(ctx, roomID)})
	}
	return refs, nil
}

// fetchTopology discovers joined rooms, classifies spaces, and resolves each
// space's joined children, then assembles the grouping. Per-room state lookups
// that fail are treated as "not a space" so one inaccessible room can't break
// the whole listing.
func (a *Adapter) fetchTopology(ctx context.Context) (roomTopology, error) {
	resp, err := a.client.JoinedRooms(ctx)
	if err != nil {
		return roomTopology{}, err
	}
	joined := make([]string, 0, len(resp.JoinedRooms))
	isSpace := make(map[string]bool)
	names := make(map[string]string)
	spaceChildren := make(map[string][]string)

	for _, roomID := range resp.JoinedRooms {
		idStr := roomID.String()
		joined = append(joined, idStr)
		names[idStr] = a.roomName(ctx, roomID)
		if a.isSpaceRoom(ctx, roomID) {
			isSpace[idStr] = true
			spaceChildren[idStr] = a.spaceChildren(ctx, roomID)
		}
	}
	return buildRoomTopology(joined, isSpace, spaceChildren, names), nil
}

// roomName resolves a room's display name from its m.room.name state, falling
// back to the room ID.
func (a *Adapter) roomName(ctx context.Context, roomID id.RoomID) string {
	var content event.RoomNameEventContent
	if err := a.client.StateEvent(ctx, roomID, event.StateRoomName, "", &content); err == nil && content.Name != "" {
		return content.Name
	}
	return roomID.String()
}

// isSpaceRoom reports whether a room's create event marks it as an m.space.
func (a *Adapter) isSpaceRoom(ctx context.Context, roomID id.RoomID) bool {
	var content event.CreateEventContent
	if err := a.client.StateEvent(ctx, roomID, event.StateCreate, "", &content); err != nil {
		return false
	}
	return content.Type == event.RoomTypeSpace
}

// spaceChildren returns the room IDs referenced by a space's m.space.child
// state. Membership and nested-space filtering happens in buildRoomTopology.
func (a *Adapter) spaceChildren(ctx context.Context, roomID id.RoomID) []string {
	state, err := a.client.State(ctx, roomID)
	if err != nil {
		return nil
	}
	children := make([]string, 0, len(state[event.StateSpaceChild]))
	for childID := range state[event.StateSpaceChild] {
		children = append(children, childID)
	}
	return children
}
