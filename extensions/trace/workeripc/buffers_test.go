package workeripc

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalMessageAcrossWriteBoundaries(t *testing.T) {
	const payload = `{"value":"private\ntext"}`
	encoded := []byte(payload + "\n")
	for split := 0; split < len(encoded); split++ {
		m := canonicalMessage{remaining: []byte(payload)}
		for _, part := range [][]byte{encoded[:split], encoded[split:]} {
			if n, err := m.Write(part); err != nil || n != len(part) {
				t.Fatal("canonical split failed", split, err)
			}
		}
		if !m.complete || len(m.remaining) != 0 {
			t.Fatal("canonical message incomplete")
		}
		if _, err := m.Write([]byte("\n")); err == nil {
			t.Fatal("accepted second terminator")
		}
	}
	for _, bad := range []string{payload + " ", payload + "\n\n", `{"value":"other"}` + "\n"} {
		m := canonicalMessage{remaining: []byte(payload)}
		if _, err := m.Write([]byte(bad)); err == nil {
			t.Fatal("accepted noncanonical bytes")
		}
	}
}

func TestMessageBufferMatchesOwnedEncodingAtLimit(t *testing.T) {
	var storage messageBuffer
	storage.reset()
	encoder := json.NewEncoder(&storage)
	for _, value := range []string{"short", "private\n\t\"\\<&>", strings.Repeat("a", MaxMessageBytes-2), "short again"} {
		storage.reset()
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
		got, err := storage.frame()
		if err != nil {
			t.Fatal(err)
		}
		want, err := encode(value)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("private framing changed", err)
		}
		storage.reset()
		if !bytes.Equal(storage.data[:], make([]byte, len(storage.data))) {
			t.Fatal("private bytes retained after write")
		}
	}
	storage.reset()
	if err := encoder.Encode(strings.Repeat("a", MaxMessageBytes-1)); err == nil {
		t.Fatal("accepted oversized message")
	}
}

func TestReceiveStorageDoesNotOwnReturnedStringsOrRawMessages(t *testing.T) {
	type value struct {
		Text string          `json:"text"`
		Raw  json.RawMessage `json:"raw"`
	}
	one := value{Text: "first private value", Raw: json.RawMessage(`{"number":1}`)}
	two := value{Text: "second private value", Raw: json.RawMessage(`{"number":2}`)}
	var stream bytes.Buffer
	for _, v := range []value{one, two} {
		data, err := encode(v)
		if err != nil {
			t.Fatal(err)
		}
		stream.Write(data)
	}
	var storage [MaxMessageBytes]byte
	var first, second value
	if _, err := receiveBuffered(&stream, &first, &storage); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storage[:], make([]byte, len(storage))) {
		t.Fatal("received private bytes retained")
	}
	if _, err := receiveBuffered(&stream, &second, &storage); err != nil {
		t.Fatal(err)
	}
	for i := range storage {
		storage[i] = 0xff
	}
	if first.Text != one.Text || !bytes.Equal(first.Raw, one.Raw) || second.Text != two.Text || !bytes.Equal(second.Raw, two.Raw) {
		t.Fatal("storage reuse changed delivered values")
	}
}
