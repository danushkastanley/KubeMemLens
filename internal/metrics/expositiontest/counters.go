// Package expositiontest checks metrics exposition text in tests.
package expositiontest

import (
	"fmt"
	"strings"
)

// CounterFamilyProblems reports where text breaks the OpenMetrics counter
// naming rule: HELP and TYPE name the family without a _total suffix, and
// every sample of that family is named <family>_total.
func CounterFamilyProblems(text string) []string {
	var problems []string
	help, family, counter := "", "", false
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 3 && fields[0] == "#" && fields[1] == "HELP":
			help = fields[2]
		case len(fields) == 4 && fields[0] == "#" && fields[1] == "TYPE":
			family, counter = fields[2], fields[3] == "counter"
			if counter && strings.HasSuffix(family, "_total") {
				problems = append(problems, fmt.Sprintf("counter family %s ends in _total", family))
			}
			if counter && help != family {
				problems = append(problems, fmt.Sprintf("counter family %s has HELP for %q", family, help))
			}
		case counter && line != "" && !strings.HasPrefix(line, "#"):
			if name := strings.FieldsFunc(line, func(r rune) bool { return r == '{' || r == ' ' })[0]; name != family+"_total" {
				problems = append(problems, fmt.Sprintf("counter family %s has sample %s", family, name))
			}
		}
	}
	return problems
}
