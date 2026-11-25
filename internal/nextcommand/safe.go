// Package nextcommand decides whether a name read from a response may be
// placed in a command that is shown to an operator to copy and run.
//
// Owner kinds and names come from tenant objects, and the Kubernetes API
// server does not restrict the characters in an owner reference. A command is
// offered only when every value in it is a plain Kubernetes name, which has no
// character a shell treats specially.
package nextcommand

import "k8s.io/apimachinery/pkg/util/validation"

const maxKindLength = 63

// SafeNames reports whether every value is a DNS-1123 subdomain, the form of
// namespace, Pod, workload and Node names.
func SafeNames(values ...string) bool {
	for _, value := range values {
		if len(validation.IsDNS1123Subdomain(value)) != 0 {
			return false
		}
	}
	return true
}

// SafeKind reports whether kind looks like a Kubernetes kind: ASCII letters
// and digits, starting with a letter.
func SafeKind(kind string) bool {
	if kind == "" || len(kind) > maxKindLength || !isLetter(kind[0]) {
		return false
	}
	for index := 1; index < len(kind); index++ {
		if !isLetter(kind[index]) && (kind[index] < '0' || kind[index] > '9') {
			return false
		}
	}
	return true
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
