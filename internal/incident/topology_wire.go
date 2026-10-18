package incident

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

func decodeTopology(data []byte) (TopologyBundle, error) {
	if len(data) > MaxNodeBytes {
		return TopologyBundle{}, memorytopology.ErrBounds
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	i := nodeWireInspector{fields: map[reflect.Type]map[string]reflect.Type{}}
	if err := i.value(d, reflect.TypeFor[TopologyBundle](), "", 0); err != nil {
		return TopologyBundle{}, err
	}
	if _, err := d.Token(); err != io.EOF {
		return TopologyBundle{}, memorytopology.ErrInvalid
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return TopologyBundle{}, err
	}
	var b TopologyBundle
	if err := decodeStrict(compact.Bytes(), &b); err != nil {
		return TopologyBundle{}, err
	}
	return b, ValidateTopology(b)
}
