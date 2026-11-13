package extension

import (
	"bytes"
	"encoding/json"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

// heldResponse keeps a read's response in memory while the read gate is held,
// so the gate covers building the response and never a client's pace in
// reading it. Every body written through it is already bounded by the response
// byte limit, and the server admits a fixed number of reads at once.
type heldResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *heldResponse) Header() http.Header { return r.header }

func (r *heldResponse) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *heldResponse) Write(data []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.body.Write(data)
}

func (r *heldResponse) sendTo(w http.ResponseWriter) {
	r.WriteHeader(http.StatusOK)
	w.WriteHeader(r.status)
	_, _ = w.Write(r.body.Bytes())
}

// serveGated builds the response under the read gate and sends it afterwards.
// The gate is released even if building the response panics.
func (h *ReadHandler) serveGated(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo, schema int) {
	select {
	case h.gate <- struct{}{}:
	case <-r.Context().Done():
		writeReadError(w, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "read request was cancelled")
		return
	}
	held := &heldResponse{header: w.Header()}
	func() {
		defer func() { <-h.gate }()
		h.serveResource(held, r, info, schema)
	}()
	held.sendTo(w)
}

// writeBoundedReadJSON encodes the value once and refuses it, before anything
// is sent, if it is larger than the response byte limit.
func writeBoundedReadJSON(w http.ResponseWriter, value any, maxBytes int) {
	body, err := json.Marshal(value)
	if err != nil {
		writeReadError(w, http.StatusInternalServerError, metav1.StatusReasonInternalError, "response could not be encoded")
		return
	}
	if len(body)+1 > maxBytes {
		writeReadError(w, http.StatusInsufficientStorage, metav1.StatusReason("ResponseTooLarge"), "response exceeds the configured maximum")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	_, _ = w.Write([]byte{'\n'})
}
