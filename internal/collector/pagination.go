package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const (
	defaultContainerPageSize = 500
	maxContainerPageSize     = 500
	continueTokenBytes       = 67
	scopedContinueTokenBytes = 84
)

type keyedContainer struct {
	key  string
	item api.ContainerSnapshot
}

type PageSelection struct {
	Indexes  []int
	Continue string
}

func writeContainerPage(w http.ResponseWriter, r *http.Request, store *Store, opts HandlerOptions) {
	limit, err := pageLimit(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	after, err := decodeContainerCursor(r.URL.Query().Get("continue"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	containers := store.ListContainers(time.Now().UTC(), opts.SnapshotTTL)
	items := make([]keyedContainer, len(containers))
	for index, item := range containers {
		items[index] = keyedContainer{key: containerSortKey(item), item: item}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
	start := sort.Search(len(items), func(index int) bool {
		return items[index].key > after
	})
	end := min(start+limit, len(items))
	pageItems := pageContainerItems(items[start:end])
	for {
		page := api.ContainerPage{Items: pageItems}
		if end < len(items) && len(pageItems) > 0 {
			page.Continue = encodeContainerCursor(items[end-1].key)
		}
		body, encodeErr := encodeBoundedJSON(page, opts.MaxResponseBytes)
		if encodeErr == nil {
			writeJSONBody(w, http.StatusOK, body)
			return
		}
		if len(pageItems) <= 1 {
			writeError(w, http.StatusInsufficientStorage, "one container exceeds the configured maximum response size")
			return
		}
		pageItems = pageItems[:len(pageItems)/2]
		end = start + len(pageItems)
	}
}

func pageLimit(query url.Values) (int, error) {
	value := query.Get("limit")
	if value == "" {
		return defaultContainerPageSize, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maxContainerPageSize {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxContainerPageSize)
	}
	return limit, nil
}

func encodeContainerCursor(key string) string {
	return "v1." + key
}

func decodeContainerCursor(token string) (string, error) {
	if token == "" {
		return "", nil
	}
	if len(token) != continueTokenBytes || !strings.HasPrefix(token, "v1.") {
		return "", fmt.Errorf("continue token is invalid")
	}
	key := strings.TrimPrefix(token, "v1.")
	if _, err := hex.DecodeString(key); err != nil {
		return "", fmt.Errorf("continue token is invalid")
	}
	return key, nil
}

func containerSortKey(item api.ContainerSnapshot) string {
	identity := strings.Join([]string{item.Namespace, item.PodName, item.ContainerName, item.ContainerID, item.NodeName}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func pageContainerItems(items []keyedContainer) []api.ContainerSnapshot {
	page := make([]api.ContainerSnapshot, len(items))
	for index := range items {
		page[index] = items[index].item
	}
	return page
}

// PaginateContainers returns one deterministic page and binds its continuation
// token to the already-authorised resource scope. A token from a namespace or
// cluster view cannot be replayed against another view.
func PaginateContainers(items []api.ContainerSnapshot, query url.Values, scope string) (api.ContainerPage, error) {
	keys := make([]string, len(items))
	for index, item := range items {
		keys[index] = strings.Join([]string{item.Namespace, item.PodUID, item.PodName, item.ContainerName, item.ContainerID, item.NodeName}, "\x00")
	}
	selection, err := PaginateKeys(keys, query, scope)
	if err != nil {
		return api.ContainerPage{}, err
	}
	page := api.ContainerPage{Items: make([]api.ContainerSnapshot, len(selection.Indexes)), Continue: selection.Continue}
	for offset, index := range selection.Indexes {
		page.Items[offset] = items[index]
	}
	return page, nil
}

// PaginateKeys selects at most 500 stable item identities. The returned indexes
// are ordered by an opaque digest, so continuation tokens reveal neither names
// nor namespaces and remain bound to the caller's authorised resource scope.
func PaginateKeys(keys []string, query url.Values, scope string) (PageSelection, error) {
	limit, err := pageLimit(query)
	if err != nil {
		return PageSelection{}, err
	}
	after, err := decodeScopedContainerCursor(query.Get("continue"), scope)
	if err != nil {
		return PageSelection{}, err
	}
	type keyedIndex struct {
		key   string
		index int
	}
	keyed := make([]keyedIndex, len(keys))
	for index, identity := range keys {
		digest := sha256.Sum256([]byte(identity))
		keyed[index] = keyedIndex{key: hex.EncodeToString(digest[:]), index: index}
	}
	sort.Slice(keyed, func(i, j int) bool { return keyed[i].key < keyed[j].key })
	start := sort.Search(len(keyed), func(index int) bool { return keyed[index].key > after })
	end := min(start+limit, len(keyed))
	selection := PageSelection{Indexes: make([]int, end-start)}
	for offset, item := range keyed[start:end] {
		selection.Indexes[offset] = item.index
	}
	if end < len(keyed) && end > start {
		selection.Continue = encodeScopedContainerCursor(scope, keyed[end-1].key)
	}
	return selection, nil
}

func encodeScopedContainerCursor(scope, key string) string {
	digest := sha256.Sum256([]byte(scope))
	return "v2." + hex.EncodeToString(digest[:8]) + "." + key
}

func decodeScopedContainerCursor(token, scope string) (string, error) {
	if token == "" {
		return "", nil
	}
	if len(token) != scopedContinueTokenBytes || !strings.HasPrefix(token, "v2.") {
		return "", fmt.Errorf("continue token is invalid")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", fmt.Errorf("continue token is invalid")
	}
	digest := sha256.Sum256([]byte(scope))
	if parts[1] != hex.EncodeToString(digest[:8]) {
		return "", fmt.Errorf("continue token is invalid")
	}
	if _, err := hex.DecodeString(parts[2]); err != nil || len(parts[2]) != sha256.Size*2 {
		return "", fmt.Errorf("continue token is invalid")
	}
	return parts[2], nil
}
