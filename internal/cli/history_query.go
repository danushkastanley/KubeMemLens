package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func parseTrendWindow(source, metric string, window, step time.Duration, scope memoryhistory.Scope) (memoryhistory.Query, error) {
	if window < 0 || window > memoryhistory.MaxRange || window%time.Second != 0 || step < 0 || step > time.Hour || step%time.Second != 0 {
		return memoryhistory.Query{}, fmt.Errorf("window must be at most 168h and step must use whole seconds up to 1h")
	}
	values := url.Values{"source": {source}}
	if metric != "" {
		values.Set("metric", metric)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if window > 0 {
		values.Set("start", now.Add(-window).Format(time.RFC3339))
	}
	if step > 0 {
		values.Set("step", strconv.FormatInt(int64(step/time.Second), 10))
	}
	return memoryhistory.ParseQuery(values, scope, now)
}
