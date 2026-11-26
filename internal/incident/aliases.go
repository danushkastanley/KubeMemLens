package incident

import (
	"strconv"
	"strings"
)

// captureAliases replaces real names with capture-local aliases such as
// namespace-1 or pod-2. Each kind is numbered separately in order of first
// appearance, so the same input always gets the same aliases and a value seen
// again keeps the alias it was first given. The mapping back to real names
// never leaves this value and is never written to a file.
type captureAliases struct {
	values map[string]string
	counts map[string]int
}

func newCaptureAliases() *captureAliases {
	return &captureAliases{values: map[string]string{}, counts: map[string]int{}}
}

func (a *captureAliases) name(kind, key string) string {
	if key == "" {
		return ""
	}
	combined := kind + "\x00" + key
	if prior, ok := a.values[combined]; ok {
		return prior
	}
	a.counts[kind]++
	value := kind + "-" + strconv.Itoa(a.counts[kind])
	a.values[combined] = value
	return value
}

// alias reports whether value is a capture-local alias of the given kind.
func alias(value, prefix string) bool {
	suffix, ok := strings.CutPrefix(value, prefix+"-")
	if !ok || suffix == "" || suffix[0] == '0' {
		return false
	}
	n, err := strconv.Atoi(suffix)
	return err == nil && n > 0 && n <= 4096 && strconv.Itoa(n) == suffix
}
