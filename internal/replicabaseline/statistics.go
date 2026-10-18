package replicabaseline

import (
	"math"
	"slices"
)

func median(values []float64) float64 {
	ordered := append([]float64(nil), values...)
	slices.Sort(ordered)
	middle := len(ordered) / 2
	if len(ordered)%2 != 0 {
		return ordered[middle]
	}
	return ordered[middle-1]/2 + ordered[middle]/2
}

func compare(policy metricPolicy, candidate float64, values []float64, references []string) Comparison {
	result := Comparison{Metric: policy.metric, Unit: policy.unit, State: "insufficient-peers", Candidate: &candidate, References: references, Outlier: "none", Confidence: "insufficient"}
	if len(values) < MinReferences {
		return result
	}
	mid := median(values)
	distances := make([]float64, len(values))
	for i, value := range values {
		distances[i] = math.Abs(value - mid)
	}
	mad := median(distances)
	difference := candidate - mid
	result.State, result.Confidence = "compared", "current-peers"
	result.Distribution = &Distribution{Minimum: slices.Min(values), Median: mid, Maximum: slices.Max(values), MAD: mad}
	result.Difference = &difference
	result.Method = "flat-reference"
	scorePassed := true
	if mad != 0 {
		score := .6745 * difference / mad
		result.ModifiedScore = &score
		result.Method = "median-mad"
		scorePassed = math.Abs(score) > 3.5
	}
	if scorePassed && math.Abs(difference) > policy.floor && math.Abs(difference) > math.Abs(mid)*.25 {
		switch {
		case difference > 0:
			result.Outlier = "higher"
		case policy.direction == bothDirections:
			result.Outlier = "lower"
		}
	}
	return result
}

func recentHistory(peer Peer) []Point {
	points := append([]Point(nil), peer.History...)
	slices.SortFunc(points, func(a, b Point) int { return a.At.Compare(b.At) })
	return slices.DeleteFunc(points, func(p Point) bool {
		return p.At.Before(peer.StableSince) || p.At.Before(peer.CapturedAt.Add(-HistoryLookback))
	})
}

func slope(peer Peer, points []Point) Value {
	missing := Value{State: Unreported}
	if peer.HistoryState != Available {
		return Value{State: peer.HistoryState}
	}
	if len(points) < MinHistoryPoints || points[len(points)-1].At.Sub(points[0].At) < StabilityWindow || points[0].At.After(peer.CapturedAt.Add(-HistoryLookback+MaximumHistoryGap)) || peer.CapturedAt.Sub(points[len(points)-1].At) > MaximumSampleSkew {
		return missing
	}
	for i := 1; i < len(points); i++ {
		if points[i].At.Sub(points[i-1].At) > MaximumHistoryGap {
			return Value{State: Partial}
		}
	}
	slopes := make([]float64, 0, len(points)*(len(points)-1)/2)
	for i, before := range points {
		for _, after := range points[i+1:] {
			slopes = append(slopes, (float64(after.Bytes)-float64(before.Bytes))/after.At.Sub(before.At).Seconds())
		}
	}
	return Value{State: Available, Number: median(slopes)}
}
