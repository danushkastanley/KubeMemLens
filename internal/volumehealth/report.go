package volumehealth

import (
	"encoding/json"
	"fmt"
)

// Report defaults to an aggregate export. Interactive returns an isolated copy
// for the authorised in-process consumer; it is not a collector wire contract.
type Report struct{ observations []Observation }

func NewReport(observations []Observation) Report {
	r := Report{observations: make([]Observation, len(observations))}
	for i, o := range observations {
		r.observations[i] = o
		r.observations[i].Conditions = append([]Condition(nil), o.Conditions...)
	}
	return r
}

func (r Report) Interactive() []Observation { return NewReport(r.observations).observations }

type Summary struct {
	Observations  int            `json:"observations"`
	Adverse       int            `json:"adverse"`
	UnknownStatus int            `json:"unknownStatus"`
	States        map[State]int  `json:"states"`
	Sources       map[Source]int `json:"sources"`
}

func (r Report) Summary() Summary {
	s := Summary{Observations: len(r.observations), States: map[State]int{}, Sources: map[Source]int{}}
	for _, o := range r.observations {
		s.States[o.State]++
		s.Sources[o.Source]++
		if o.Adverse {
			s.Adverse++
		}
		if o.UnknownStatus {
			s.UnknownStatus++
		}
	}
	return s
}

func (r Report) MarshalJSON() ([]byte, error) { return json.Marshal(r.Summary()) }
func (r Report) String() string               { return fmt.Sprint(r.Summary()) }
func (r Report) GoString() string             { return r.String() }
