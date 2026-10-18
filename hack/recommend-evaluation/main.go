// recommendation-evaluation runs offline; it never fetches incident data.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

func run(input io.Reader, copies io.Reader, output io.Writer) error {
	corpus, err := loadCorpus(input)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(copies, 65537))
	if err != nil || len(raw) > 65536 {
		return errCorpus
	}
	if uniqueJSON(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return errCorpus
	}
	var reviewed map[string]string
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&reviewed) != nil || decoder.Decode(new(any)) != io.EOF {
		return errCorpus
	}
	predictions := make([]Prediction, 0, len(corpus.Cases))
	for _, c := range corpus.Cases {
		p, err := predict(c)
		if err != nil {
			return fmt.Errorf("case %s: domain evaluation failed", c.ID)
		}
		predictions = append(predictions, p)
	}
	report, err := evaluate(corpus, predictions, reviewed)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return err
	}
	if !report.Passed {
		return fmt.Errorf("recommendation evaluation failed; retain report and review case-level differences")
	}
	return nil
}

func main() {
	path := flag.String("corpus", "hack/recommend-evaluation/testdata/corpus-v1.json", "bounded labelled corpus")
	copies := flag.String("reviewed-copy", "hack/recommend-evaluation/testdata/copy-v1.json", "reviewed recommendation text hashes")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	input, err := os.Open(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open corpus")
		os.Exit(2)
	}
	defer input.Close()
	copyFile, err := os.Open(*copies)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open reviewed copy")
		os.Exit(2)
	}
	defer copyFile.Close()
	if err := run(input, copyFile, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
