package nodecontext

import "time"

// These are contract ceilings, not performance claims or deployment sizing.
const (
	MaxSummaryBytes     = 4 << 20
	MaxJSONDepth        = 32
	MaxJSONTokens       = 250_000
	MaxObservationBytes = 16 << 10
	MaxNodeNameBytes    = 253
	MaxNodeUIDBytes     = 128
	MaxSystemContainers = 4
	MaxHugepages        = 16
	MaxResourceBytes    = 63
	MaxCaveats          = 16
	MaxCaveatBytes      = 64
	MaxPageRecords      = 100
	MaxHistorySeries    = 1000
	MaxHistoryPoints    = 61
	MaxHistoryBytes     = 64 << 20
	CollectionInterval  = 15 * time.Second
	RequestTimeout      = 5 * time.Second
	MaxBackoff          = 60 * time.Second
	StaleAfter          = 45 * time.Second
	MaxSampleSkew       = 5 * time.Second
	HistoryDuration     = 15 * time.Minute
)
