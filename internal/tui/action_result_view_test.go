package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func TestLongActionResultWrapsAndRemainsNavigable(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {160, 35}} {
		m := panelModel(t)
		m.width, m.height = size[0], size[1]
		m.action.mode = actionResultMode
		m.action.result.title = "Read-only recommendations"
		for i := 0; i < 90; i++ {
			m.action.result.lines = append(m.action.result.lines, fmt.Sprintf("Step %d: %s", i, strings.Repeat("review ", 25)))
		}
		m.action.result.lines = append(m.action.result.lines, "Final verification remains reachable.")
		updated, _ := m.handleActionKey(keyMessage("G"))
		end := updated.(appModel)
		rendered := end.renderAction(size[0])
		if !strings.Contains(rendered, "Final verification remains reachable") {
			t.Fatal("truncated verification")
		}
		if len(strings.Split(rendered, "\n")) > m.bodyRows() {
			t.Fatal("vertical overflow")
		}
		for _, line := range strings.Split(rendered, "\n") {
			if uniseg.StringWidth(line) > size[0] {
				t.Fatal("horizontal overflow")
			}
		}
		updated, _ = end.handleActionKey(keyMessage("g"))
		start := updated.(appModel)
		if !strings.Contains(start.renderAction(size[0]), "Read-only recommendations") {
			t.Fatal("first page inaccessible")
		}
		updated, _ = end.handleActionKey(keyMessage("esc"))
		closed := updated.(appModel)
		if closed.action.mode != actionClosed || closed.action.viewport.offset != 0 {
			t.Fatal("result close retained scroll position")
		}
	}
}
