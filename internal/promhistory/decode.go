package promhistory

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type matrixSeries struct {
	Metric map[string]string   `json:"metric"`
	Values [][]json.RawMessage `json:"values"`
}

type envelope struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string         `json:"resultType"`
		Result     []matrixSeries `json:"result"`
	} `json:"data"`
	Warnings []string `json:"warnings"`
	Infos    []string `json:"infos"`
}

type pair struct{ value, sampled *matrixSeries }

var labelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func decode(ctx context.Context, data []byte, cluster string, r *memoryhistory.Report) error {
	if err := boundedJSON(ctx, data); err != nil {
		return err
	}
	var response envelope
	if json.Unmarshal(data, &response) != nil || response.Status != "success" || response.Data.ResultType != "matrix" || response.Data.Result == nil {
		return memoryhistory.ErrSource
	}
	pairs := make([]pair, len(r.Series))
	for i := range response.Data.Result {
		item := &response.Data.Result[i]
		index, err := match(item, cluster, *r)
		if err != nil {
			return err
		}
		p := &pairs[index]
		switch item.Metric[fieldLabel] {
		case "value":
			if p.value != nil {
				return memoryhistory.ErrSource
			}
			p.value = item
		case "sampled":
			if p.sampled != nil {
				return memoryhistory.ErrSource
			}
			p.sampled = item
		default:
			return memoryhistory.ErrSource
		}
	}
	for i, p := range pairs {
		if p.value == nil && p.sampled == nil {
			continue
		}
		if p.value == nil || p.sampled == nil || !sameLabels(p.value.Metric, p.sampled.Metric) {
			return memoryhistory.ErrSource
		}
		if err := fill(&r.Series[i], r.Query, p); err != nil {
			return err
		}
	}
	memoryhistory.Summarise(r, len(response.Warnings) > 0 || len(response.Infos) > 0)
	if len(response.Warnings) > 0 || len(response.Infos) > 0 {
		r.Reason = "provider-partial"
	} else if r.State == memoryhistory.Missing {
		r.Reason = "series-unreported"
	}
	return nil
}

func match(s *matrixSeries, cluster string, r memoryhistory.Report) (int, error) {
	if len(s.Metric) == 0 || s.Values == nil {
		return 0, memoryhistory.ErrSource
	}
	for name := range s.Metric {
		if !labelName.MatchString(name) {
			return 0, memoryhistory.ErrSource
		}
	}
	if name := s.Metric["__name__"]; name != "" && name != metricName(r.Query.Metric) {
		return 0, memoryhistory.ErrSource
	}
	found := -1
	for i, series := range r.Series {
		matches := true
		for key, value := range targetLabels(cluster, series.Target) {
			if s.Metric[key] != value {
				matches = false
				break
			}
		}
		if matches {
			if found != -1 {
				return 0, memoryhistory.ErrSource
			}
			found = i
		}
	}
	if found < 0 {
		return 0, memoryhistory.ErrSource
	}
	return found, nil
}

func sameLabels(a, b map[string]string) bool {
	for k, v := range a {
		if k != "__name__" && k != fieldLabel && b[k] != v {
			return false
		}
	}
	for k, v := range b {
		if k != "__name__" && k != fieldLabel && a[k] != v {
			return false
		}
	}
	return true
}

func fill(s *memoryhistory.Series, q memoryhistory.Query, p pair) error {
	if len(p.value.Values) != len(p.sampled.Values) {
		return memoryhistory.ErrSource
	}
	previous := -1
	for i, v := range p.value.Values {
		index, value, err := sample(v, q)
		if err != nil {
			return err
		}
		other, sampled, err := sample(p.sampled.Values[i], q)
		if err != nil {
			return err
		}
		if index <= previous || other != index {
			return memoryhistory.ErrSource
		}
		previous = index
		at := s.Points[index].At
		if math.IsNaN(value) || math.IsNaN(sampled) {
			continue
		}
		if value < 0 || value > 1<<53-1 || math.Trunc(value) != value || sampled < 0 || sampled > float64(at.Unix()) {
			return memoryhistory.ErrSource
		}
		seconds, fraction := math.Modf(sampled)
		stamp := time.Unix(int64(seconds), int64(math.Round(fraction*1e9))).UTC()
		if stamp.Before(s.Target.StartedAt) {
			return memoryhistory.ErrSource
		}
		bytes := uint64(value)
		point := memoryhistory.Point{At: at, SampledAt: stamp, Bytes: &bytes, State: memoryhistory.Fresh}
		if at.Sub(stamp) > memoryhistory.SampleMaxAge {
			point.State = memoryhistory.Stale
		}
		s.Points[index] = point
	}
	return nil
}

func sample(pair []json.RawMessage, q memoryhistory.Query) (int, float64, error) {
	if len(pair) != 2 {
		return 0, 0, memoryhistory.ErrSource
	}
	var encoded string
	if json.Unmarshal(pair[1], &encoded) != nil {
		return 0, 0, memoryhistory.ErrSource
	}
	at, err := strconv.ParseFloat(string(pair[0]), 64)
	if err != nil || math.IsNaN(at) || math.IsInf(at, 0) || math.Trunc(at) != at || at < float64(q.Start.Unix()) || at > float64(q.End.Unix()) {
		return 0, 0, memoryhistory.ErrSource
	}
	seconds := int64(at)
	if seconds < q.Start.Unix() || seconds > q.End.Unix() || (seconds-q.Start.Unix())%int64(q.Step/time.Second) != 0 {
		return 0, 0, memoryhistory.ErrSource
	}
	value, err := strconv.ParseFloat(encoded, 64)
	if err != nil || math.IsInf(value, 0) {
		return 0, 0, memoryhistory.ErrSource
	}
	return int((seconds - q.Start.Unix()) / int64(q.Step/time.Second)), value, nil
}
