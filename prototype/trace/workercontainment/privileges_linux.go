//go:build linux && (amd64 || arm64)

package workercontainment

import (
	"io"
	"os"
	"strconv"
	"strings"
)

// RequirePrivileges rejects missing capabilities and privilege beyond the
// selected BPF/PERFMON budget, including latent permitted/ambient/inherited caps.
func RequirePrivileges() error {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return ErrContainment
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(data) > 16384 {
		return ErrContainment
	}
	return checkCapabilities(string(data))
}

func checkCapabilities(status string) error {
	const required = uint64(1)<<38 | uint64(1)<<39
	values := make(map[string]uint64, 5)
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "CapEff:", "CapPrm:", "CapInh:", "CapAmb:", "CapBnd:":
		default:
			continue
		}
		if _, duplicate := values[fields[0]]; duplicate || len(fields) != 2 || len(fields[1]) != 16 || strings.Trim(fields[1], "0123456789abcdef") != "" {
			return ErrContainment
		}
		value, err := strconv.ParseUint(fields[1], 16, 64)
		if err != nil || value & ^required != 0 {
			return ErrContainment
		}
		values[fields[0]] = value
	}
	if len(values) != 5 || values["CapEff:"] != required || values["CapPrm:"] != required {
		return ErrContainment
	}
	return nil
}
