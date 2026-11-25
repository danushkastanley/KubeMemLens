package main

import (
	"errors"
	"flag"
)

// The default is the authenticated aggregated API. The legacy mode serves
// reads and writes over plain HTTP to anyone who can reach the listeners, so
// it has to be chosen explicitly.
func registerIngestionMode(flags *flag.FlagSet) *string {
	return flags.String("ingestion-mode", ingestionAuthenticated, "snapshot ingestion mode: authenticated, or legacy for unauthenticated HTTP on the listen addresses")
}

func checkIngestionMode(mode, certFile, keyFile string) error {
	switch mode {
	case ingestionLegacy:
		return nil
	case ingestionAuthenticated:
		if certFile == "" || keyFile == "" {
			return errors.New("authenticated ingestion needs --extension-tls-cert-file and --extension-tls-key-file; --ingestion-mode=legacy serves unauthenticated HTTP instead")
		}
		return nil
	}
	return errors.New("ingestion mode must be legacy or authenticated")
}
