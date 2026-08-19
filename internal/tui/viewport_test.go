package tui

import "testing"

func TestViewportKeepsSelectionVisible(t *testing.T) {
	var viewport viewport
	viewport.resize(20)
	viewport.reconcile(5000)
	viewport.last()

	start, end := viewport.visibleRange()
	if viewport.selected != 4999 || start != 4980 || end != 5000 {
		t.Fatalf("last range = selected %d range %d:%d", viewport.selected, start, end)
	}

	viewport.move(-10)
	start, end = viewport.visibleRange()
	if viewport.selected < start || viewport.selected >= end {
		t.Fatalf("selected %d is outside %d:%d", viewport.selected, start, end)
	}
}

func TestViewportReconcilesEmptyAndShrinkingData(t *testing.T) {
	viewport := viewport{selected: 8, offset: 5, capacity: 4, count: 10}
	viewport.reconcile(2)
	if viewport.selected != 1 || viewport.offset != 0 {
		t.Fatalf("shrunk viewport = %#v", viewport)
	}

	viewport.reconcile(0)
	if viewport.selected != 0 || viewport.offset != 0 {
		t.Fatalf("empty viewport = %#v", viewport)
	}
}

func TestViewportResizeRetainsSelectedItem(t *testing.T) {
	viewport := viewport{selected: 50, offset: 40, capacity: 20, count: 100}
	viewport.resize(5)
	start, end := viewport.visibleRange()
	if viewport.selected < start || viewport.selected >= end {
		t.Fatalf("selected %d is outside resized range %d:%d", viewport.selected, start, end)
	}

	viewport.resize(60)
	start, end = viewport.visibleRange()
	if viewport.selected < start || viewport.selected >= end {
		t.Fatalf("selected %d is outside expanded range %d:%d", viewport.selected, start, end)
	}
}

func TestViewportWindowBoundsWorkToVisibleItems(t *testing.T) {
	items := make([]int, 5000)
	viewport := viewport{selected: 2500, capacity: 25, count: len(items)}
	viewport.ensureVisible()
	window := viewportWindow(viewport, items)
	if len(window) != 25 {
		t.Fatalf("window length = %d, want 25", len(window))
	}
}
