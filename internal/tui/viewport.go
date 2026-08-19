package tui

// viewport owns selection and vertical windowing for either a table or a
// line-oriented detail view. The selected item is always inside VisibleRange.
type viewport struct {
	selected int
	offset   int
	capacity int
	count    int
}

func (v *viewport) resize(capacity int) {
	if capacity < 1 {
		capacity = 1
	}
	v.capacity = capacity
	v.reconcile(v.count)
}

func (v *viewport) reconcile(count int) {
	if count < 0 {
		count = 0
	}
	v.count = count
	if count == 0 {
		v.selected = 0
		v.offset = 0
		return
	}
	if v.selected < 0 {
		v.selected = 0
	}
	if v.selected >= count {
		v.selected = count - 1
	}
	v.ensureVisible()
}

func (v *viewport) reset() {
	v.selected = 0
	v.offset = 0
	v.reconcile(v.count)
}

func (v *viewport) move(delta int) {
	v.selected += delta
	v.reconcile(v.count)
}

func (v *viewport) first() {
	v.selected = 0
	v.reconcile(v.count)
}

func (v *viewport) last() {
	if v.count > 0 {
		v.selected = v.count - 1
	}
	v.reconcile(v.count)
}

func (v viewport) visibleRange() (int, int) {
	if v.count == 0 {
		return 0, 0
	}
	capacity := v.capacity
	if capacity < 1 {
		capacity = 1
	}
	start := v.offset
	if start < 0 {
		start = 0
	}
	if start >= v.count {
		start = v.count - 1
	}
	end := start + capacity
	if end > v.count {
		end = v.count
	}
	return start, end
}

func viewportWindow[T any](v viewport, items []T) []T {
	copy := v
	copy.reconcile(len(items))
	start, end := copy.visibleRange()
	return items[start:end]
}

func (v *viewport) ensureVisible() {
	capacity := v.capacity
	if capacity < 1 {
		capacity = 1
	}
	if v.selected < v.offset {
		v.offset = v.selected
	}
	if v.selected >= v.offset+capacity {
		v.offset = v.selected - capacity + 1
	}
	maxOffset := v.count - capacity
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.offset > maxOffset {
		v.offset = maxOffset
	}
	if v.offset < 0 {
		v.offset = 0
	}
}
