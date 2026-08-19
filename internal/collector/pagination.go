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
)

type keyedContainer struct {
	key  string
	item api.ContainerSnapshot
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
