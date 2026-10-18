package incidentsession

import (
	"bytes"
	"strconv"
)

func referenceAlias(aliases map[string]string, ref CaptureReference) string {
	key := strconv.Itoa(ref.SchemaVersion) + ":" + ref.Digest
	alias, ok := aliases[key]
	if !ok {
		alias = "evidence-" + strconv.Itoa(len(aliases)+1)
		aliases[key] = alias
	}
	return alias
}

// SelectCapture resolves an export-local alias or exact digest to retained
// bytes. This is offline selection, never authority to restore or mutate state.
func SelectCapture(document AuthorisedExport, selector string) (CapturedEvidence, error) {
	if validateAuthorised(document) != nil {
		return CapturedEvidence{}, ErrInvalid
	}
	aliases := map[string]string{}
	selected := ""
	for _, entry := range document.Entries {
		for _, ref := range entry.References {
			alias := referenceAlias(aliases, ref)
			if selector == alias || selector == ref.Digest {
				selected = ref.Digest
			}
		}
	}
	for _, capture := range document.Captures {
		if capture.Digest == selected {
			capture.Data = bytes.Clone(capture.Data)
			return capture, nil
		}
	}
	return CapturedEvidence{}, ErrNotFound
}
