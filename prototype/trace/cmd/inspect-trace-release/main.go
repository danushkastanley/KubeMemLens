// inspect-trace-release checks payload contents without extracting or executing
// candidates. Its result is structural evidence, never installation permission.
package main

import (
	json "encoding/json/v2"
	"fmt"
	"io"
	"os"

	"github.com/danushkastanley/kube-memlens/prototype/trace/releasebundle"
)

type receipt struct {
	Inventories          []fileInventory `json:"inventories"`
	SchemaVersion        int             `json:"schemaVersion"`
	ReleaseVersion       string          `json:"releaseVersion"`
	SourceCommit         string          `json:"sourceCommit"`
	ImageDigest          string          `json:"imageDigest"`
	ChartImage           string          `json:"chartImage"`
	EngineDigest         string          `json:"engineDigest"`
	ProgrammeIndexDigest string          `json:"programmeIndexDigest"`
	PayloadAgreement     bool            `json:"payloadAgreement"`
	Authenticated        bool            `json:"authenticated"`
	RuntimeExecuted      bool            `json:"runtimeExecuted"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "expected private snapshot directory and request on stdin")
		os.Exit(2)
	}
	result, err := inspect(os.Args[1], os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trace payload inspection failed")
		os.Exit(1)
	}
	if json.MarshalWrite(os.Stdout, result) != nil {
		fmt.Fprintln(os.Stderr, "trace inspection receipt failed")
		os.Exit(1)
	}
}

func inspect(directory string, input io.Reader) (receipt, error) {
	value, err := readRequest(input)
	if err != nil {
		return receipt{}, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return receipt{}, releasebundle.ErrArchive
	}
	defer root.Close()
	var files [4]*os.File
	for index, item := range value.Payloads {
		file, err := openPayload(root, item)
		if err != nil {
			return receipt{}, err
		}
		defer file.Close()
		files[index] = file
	}
	identity := releasebundle.ImageIdentity{Version: value.ReleaseVersion, SourceCommit: value.SourceCommit, IndexDigest: value.ImageDigest}
	chart, err := releasebundle.InspectChart(files[0], value.Payloads[0].Size, value.Payloads[0].SHA256, identity)
	if err != nil {
		return receipt{}, err
	}
	engine, err := releasebundle.InspectEngine(files[1], value.Payloads[1].Size, value.Payloads[1].SHA256)
	if err != nil {
		return receipt{}, err
	}
	image, err := releasebundle.InspectImage(files[2], value.Payloads[2].Size, value.Payloads[2].SHA256, identity)
	if err != nil {
		return receipt{}, err
	}
	programmes, err := releasebundle.InspectProgrammes(files[3], value.Payloads[3].Size, value.Payloads[3].SHA256)
	if err != nil {
		return receipt{}, err
	}
	if releasebundle.MatchEngine(image, engine) != nil || releasebundle.MatchProgrammes(image, engine, programmes) != nil || chart.ImageReference() != "ghcr.io/danushkastanley/kube-memlens-trace@"+image.IndexDigest() {
		return receipt{}, releasebundle.ErrArchive
	}
	return receipt{
		Inventories:   inventories(chart, engine, image, programmes),
		SchemaVersion: 1, ReleaseVersion: value.ReleaseVersion, SourceCommit: value.SourceCommit,
		ImageDigest: image.IndexDigest(), ChartImage: chart.ImageReference(), EngineDigest: engine.Digest(),
		ProgrammeIndexDigest: programmes.Digest(), PayloadAgreement: true,
	}, nil
}
