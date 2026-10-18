package extension

import (
	"errors"
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type memoryHistoryContextResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Context           changemarkers.Context `json:"context"`
}

func (h *ReadHandler) writeHistoryContext(w http.ResponseWriter, history memoryhistory.Report, changes changemarkers.Report) {
	result := changemarkers.Context{SchemaVersion: 1, History: history, Changes: changes}
	if err := result.Validate(); err != nil {
		if errors.Is(err, changemarkers.ErrBounds) {
			writeMemoryHistoryError(w, memoryhistory.ErrBounds)
			return
		}
		writeMemoryHistoryError(w, memoryhistory.ErrSource)
		return
	}
	request := history.Selection.Request
	resource := memoryHistoryContextResource{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "MemoryHistoryContext"}, ObjectMeta: metav1.ObjectMeta{Namespace: request.Namespace, Name: request.Name, UID: types.UID(history.Selection.UID)}, Context: result}
	writeBoundedReadJSON(w, resource, min(h.opts.MaxResponseBytes, memoryhistory.MaxResponseBytes))
}
