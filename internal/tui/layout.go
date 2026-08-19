package tui

type layoutMode int

const (
	layoutCompact layoutMode = iota
	layoutStandard
	layoutWide
)

type focusMode int

const (
	focusTable focusMode = iota
	focusDetail
)

type layoutPlan struct {
	mode         layoutMode
	width        int
	height       int
	bodyRows     int
	tableWidth   int
	detailWidth  int
	splitDetail  bool
	minimumValid bool
}

func layoutFor(width, height int, view viewMode) layoutPlan {
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	bodyRows := height - 4
	if bodyRows < 5 {
		bodyRows = 5
	}
	plan := layoutPlan{
		mode:         layoutStandard,
		width:        width,
		height:       height,
		bodyRows:     bodyRows,
		tableWidth:   width,
		minimumValid: width >= 40 && height >= 10,
	}
	if width < 100 || height < 24 {
		plan.mode = layoutCompact
	}
	if width >= 150 && height >= 30 && view != viewDetail {
		plan.mode = layoutWide
		plan.splitDetail = true
		plan.tableWidth = width * 3 / 5
		plan.detailWidth = width - plan.tableWidth - 1
	}
	return plan
}

func (p layoutPlan) contentWidth() int {
	if p.width < 1 {
		return 1
	}
	return p.width
}

func (p layoutPlan) tableRows() int {
	rows := p.bodyRows - 1
	if rows < 1 {
		return 1
	}
	return rows
}
