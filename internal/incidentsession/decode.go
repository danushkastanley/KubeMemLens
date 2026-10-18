package incidentsession

import (
	json "encoding/json/v2"
	"fmt"
	"io"
)

// DecodedExport is an offline document, not proof of provenance or authority to
// restore a live session. Exactly one representation is populated by DecodeExport.
type DecodedExport struct {
	Authorised *AuthorisedExport
	Sanitised  *SanitisedExport
}

func (DecodedExport) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[incident export]") }

// DecodeExport accepts v1 timelines and trace-bearing v2 timelines. Unknown versions or fields,
// aliases, duplicate members and missing policy fields fail closed. Raw syntax
// bounds are checked before allocating a typed timeline.
func DecodeExport(data []byte) (DecodedExport, error) {
	if len(data) == 0 || len(data) > MaxExportBytes || boundedSyntax(data) != nil {
		return DecodedExport{}, ErrInvalid
	}
	var header struct {
		Redacted bool `json:"redacted"`
	}
	if json.Unmarshal(data, &header) != nil {
		return DecodedExport{}, ErrInvalid
	}
	opts := []json.Options{json.RejectUnknownMembers(true), json.MatchCaseInsensitiveNames(false)}
	if header.Redacted {
		var doc SanitisedExport
		if json.Unmarshal(data, &doc, opts...) != nil || validateSanitised(doc) != nil {
			return DecodedExport{}, ErrInvalid
		}
		return DecodedExport{Sanitised: &doc}, nil
	}
	var doc AuthorisedExport
	if json.Unmarshal(data, &doc, opts...) != nil || validateAuthorised(doc) != nil {
		return DecodedExport{}, ErrInvalid
	}
	return DecodedExport{Authorised: &doc}, nil
}
