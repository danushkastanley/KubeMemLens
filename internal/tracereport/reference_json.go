package tracereport

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
)

// UnmarshalJSON validates imported references without granting source trust.
// Required fields include explicit unknown counts; omissions cannot create them.
func (r *Reference) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || len(data) > 4096 {
		return ErrInvalid
	}
	type wireReference Reference
	var wire wireReference
	if jsonv2.Unmarshal(data, &wire, jsonv2.RejectUnknownMembers(true), jsonv2.MatchCaseInsensitiveNames(false)) != nil {
		return ErrInvalid
	}
	var fields map[string]json.RawMessage
	if jsonv2.Unmarshal(data, &fields) != nil || len(fields) != 22 {
		return ErrInvalid
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	for _, name := range []string{"produced", "sampled", "lost", "rejected"} {
		var count map[string]json.RawMessage
		if jsonv2.Unmarshal(fields[name], &count) != nil || len(count) != 2 {
			return ErrInvalid
		}
		for _, key := range []string{"known", "value"} {
			value, ok := count[key]
			if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return ErrInvalid
			}
		}
	}
	value := Reference(wire)
	if value.Validate() != nil {
		return ErrInvalid
	}
	*r = value
	return nil
}
