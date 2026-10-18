package memorytopology

import "reflect"

// Validate reproduces the interpretation from its retained source values. It
// rejects altered conclusions and does not treat a claimed state as evidence.
func (r Report) Validate() error {
	expected, err := Analyse(r.Observation, r.ObservedAt)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, r) {
		return ErrInvalid
	}
	return nil
}
