package matrix

import (
	"reflect"
	"testing"
)

func TestBuildRoomTopologyGroupsChildrenUnderSpaces(t *testing.T) {
	joined := []string{"!space:hs", "!general:hs", "!random:hs", "!solo:hs"}
	isSpace := map[string]bool{"!space:hs": true}
	spaceChildren := map[string][]string{
		"!space:hs": {"!general:hs", "!random:hs", "!notjoined:hs"},
	}
	names := map[string]string{
		"!space:hs":   "Team",
		"!general:hs": "general",
		"!random:hs":  "random",
		"!solo:hs":    "solo",
	}

	topo := buildRoomTopology(joined, isSpace, spaceChildren, names)

	if !reflect.DeepEqual(topo.spaceOrder, []string{"!space:hs"}) {
		t.Errorf("spaceOrder = %v", topo.spaceOrder)
	}
	// Children sorted by name; the un-joined child is dropped.
	if got := topo.childrenBySpace["!space:hs"]; !reflect.DeepEqual(got, []string{"!general:hs", "!random:hs"}) {
		t.Errorf("space children = %v", got)
	}
	// solo belongs to no space.
	if !reflect.DeepEqual(topo.ungrouped, []string{"!solo:hs"}) {
		t.Errorf("ungrouped = %v", topo.ungrouped)
	}
	// allRooms excludes the space room itself and is sorted by name.
	if !reflect.DeepEqual(topo.allRooms, []string{"!general:hs", "!random:hs", "!solo:hs"}) {
		t.Errorf("allRooms = %v", topo.allRooms)
	}
}

func TestBuildRoomTopologyNestedSpaceNotListedAsChannel(t *testing.T) {
	joined := []string{"!parent:hs", "!child-space:hs", "!room:hs"}
	isSpace := map[string]bool{"!parent:hs": true, "!child-space:hs": true}
	spaceChildren := map[string][]string{
		"!parent:hs": {"!child-space:hs", "!room:hs"},
	}
	names := map[string]string{"!parent:hs": "Parent", "!child-space:hs": "Sub", "!room:hs": "room"}

	topo := buildRoomTopology(joined, isSpace, spaceChildren, names)

	// A nested space must not appear as a channel of its parent.
	if got := topo.childrenBySpace["!parent:hs"]; !reflect.DeepEqual(got, []string{"!room:hs"}) {
		t.Errorf("parent children = %v, want only the non-space room", got)
	}
	// Both spaces are surfaced as servers.
	if len(topo.spaceOrder) != 2 {
		t.Errorf("expected 2 spaces, got %v", topo.spaceOrder)
	}
}

func TestBuildRoomTopologyOrdersSpacesByName(t *testing.T) {
	joined := []string{"!b:hs", "!a:hs"}
	isSpace := map[string]bool{"!a:hs": true, "!b:hs": true}
	names := map[string]string{"!a:hs": "Zeta", "!b:hs": "Alpha"}

	topo := buildRoomTopology(joined, isSpace, map[string][]string{}, names)

	// Sorted by display name: Alpha (!b) before Zeta (!a).
	if !reflect.DeepEqual(topo.spaceOrder, []string{"!b:hs", "!a:hs"}) {
		t.Errorf("spaceOrder = %v, want sorted by name", topo.spaceOrder)
	}
}

func TestBuildRoomTopologyFallsBackToRoomIDForName(t *testing.T) {
	joined := []string{"!noname:hs"}
	topo := buildRoomTopology(joined, nil, nil, map[string]string{})
	if !reflect.DeepEqual(topo.ungrouped, []string{"!noname:hs"}) {
		t.Errorf("ungrouped = %v", topo.ungrouped)
	}
}

func TestHostName(t *testing.T) {
	cases := map[string]string{
		"https://matrix.org":     "matrix.org",
		"http://localhost:8008/": "localhost:8008",
		"https://example.org/":   "example.org",
		"":                       "matrix",
	}
	for in, want := range cases {
		if got := hostName(in); got != want {
			t.Errorf("hostName(%q) = %q, want %q", in, got, want)
		}
	}
}
