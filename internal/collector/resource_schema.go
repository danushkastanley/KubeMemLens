package collector

import (
	"net/http"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func readSnapshotSchema(w http.ResponseWriter, r *http.Request) (int, error) {
	schema, err := api.NegotiateSnapshotSchema(r.Header.Get(api.SnapshotSchemaHeader))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return 0, err
	}
	w.Header().Set(api.SnapshotSchemaHeader, strconv.Itoa(schema))
	return schema, nil
}

func writeSnapshotJSON(w http.ResponseWriter, r *http.Request, value any, maxBytes int) {
	schema, err := readSnapshotSchema(w, r)
	if err != nil {
		return
	}
	writeBoundedJSON(w, api.SnapshotView(value, schema), maxBytes)
}
