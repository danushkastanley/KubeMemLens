package memorytopology

import (
	"math/big"
	"time"
)

func analyseNUMA(section Section[NUMANode], now time.Time) Distribution {
	result := Distribution{State: sectionState(section, now), Nodes: []NUMAUse{}}
	if result.State != "observed" && result.State != "partial" {
		return result
	}
	var bearing []NUMANode
	complete := true
	for _, node := range section.Items {
		use := NUMAUse{ID: node.ID, State: "unreported"}
		switch {
		case node.TotalBytes == nil || node.FreeBytes == nil:
			complete = false
		case *node.FreeBytes > *node.TotalBytes:
			complete = false
			use.State = "inconsistent"
		case *node.TotalBytes == 0:
			use.State = "memoryless"
		default:
			used := *node.TotalBytes - *node.FreeBytes
			fraction := float64(used) / float64(*node.TotalBytes)
			use.State = "observed"
			use.NonFreeBytes = &used
			use.NonFreeFraction = &fraction
			bearing = append(bearing, node)
		}
		result.Nodes = append(result.Nodes, use)
	}
	if !complete {
		result.State = "partial"
		return result
	}
	if len(bearing) < 2 {
		result.State = "insufficient-memory-domains"
		return result
	}
	// Compare exact fractions, so a 20-percentage-point boundary is not moved
	// across the threshold by binary floating-point subtraction.
	least, most := bearing[0], bearing[0]
	for _, node := range bearing[1:] {
		ratio := nonFreeRatio(node)
		if ratio.Cmp(nonFreeRatio(least)) < 0 {
			least = node
		}
		if ratio.Cmp(nonFreeRatio(most)) > 0 {
			most = node
		}
	}
	spread := new(big.Rat).Sub(nonFreeRatio(most), nonFreeRatio(least))
	excess := new(big.Rat).Mul(spread, new(big.Rat).SetInt(new(big.Int).SetUint64(*most.TotalBytes)))
	spreadValue, _ := spread.Float64()
	excessValue, _ := excess.Float64()
	result.LeastOccupiedID = &least.ID
	result.MostOccupiedID = &most.ID
	result.FractionSpread = &spreadValue
	result.EstimatedExcessBytes = &excessValue
	result.State = "within-threshold"
	if spread.Cmp(big.NewRat(1, 5)) > 0 && excess.Cmp(new(big.Rat).SetInt64(MinimumExcessBytes)) > 0 {
		result.State = "uneven-nonfree"
	}
	return result
}

func nonFreeRatio(node NUMANode) *big.Rat {
	return new(big.Rat).SetFrac(new(big.Int).SetUint64(*node.TotalBytes-*node.FreeBytes), new(big.Int).SetUint64(*node.TotalBytes))
}
