// Command chart-inventory decodes a bounded, private Helm render without a cluster client.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"k8s.io/apimachinery/pkg/util/yaml"
)

const byteLimit = 2 << 20

func decode(input io.Reader, output io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(input, byteLimit+1))
	if err != nil || len(data) > byteLimit {
		return errors.New("chart input exceeds its bound")
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	objects := make([]map[string]any, 0)
	for {
		var object map[string]any
		err := decoder.Decode(&object)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("chart input is not a Kubernetes object stream")
		}
		if len(object) == 0 {
			continue
		}
		if object["apiVersion"] == nil || object["kind"] == nil || object["metadata"] == nil || len(objects) >= 128 {
			return errors.New("chart object contract or count is invalid")
		}
		objects = append(objects, object)
	}
	if len(objects) == 0 {
		return errors.New("chart has no objects")
	}
	encoded, err := json.Marshal(objects)
	if err != nil || len(encoded) > byteLimit {
		return errors.New("chart inventory exceeds its bound")
	}
	_, err = output.Write(encoded)
	return err
}

func main() {
	if err := decode(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "private chart inventory failed")
		os.Exit(1)
	}
}
