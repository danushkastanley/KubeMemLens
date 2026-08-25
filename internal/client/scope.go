package client

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// ReadScope identifies the namespace boundary applied by the server. It is
// passed by value so a reader retains the scope it was constructed with.
type ReadScope struct {
	Namespace     string
	AllNamespaces bool
}

func NamespaceScope(namespace string) (ReadScope, error) {
	namespace = strings.TrimSpace(namespace)
	scope := ReadScope{Namespace: namespace}
	if err := scope.validate(); err != nil {
		return ReadScope{}, err
	}
	return scope, nil
}

func AllNamespacesScope() ReadScope {
	return ReadScope{AllNamespaces: true}
}

func (s ReadScope) validate() error {
	if s.AllNamespaces {
		return nil
	}
	if problems := validation.IsDNS1123Label(s.Namespace); len(problems) > 0 {
		return fmt.Errorf("invalid Kubernetes namespace %q: %s", s.Namespace, strings.Join(problems, "; "))
	}
	return nil
}

func (s ReadScope) resourcePath(resource string) (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	if s.AllNamespaces {
		return "/" + resource, nil
	}
	return "/namespaces/" + s.Namespace + "/" + resource, nil
}

func (s ReadScope) allowsNamespace(namespace string) bool {
	return s.AllNamespaces || s.Namespace == namespace
}
