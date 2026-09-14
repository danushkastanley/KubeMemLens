package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionapi"
	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionkube"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"k8s.io/client-go/rest"
)

func runAdmissionAPI(ctx context.Context, args []string, errOut io.Writer) (runErr error) {
	flags := flag.NewFlagSet("admission-api", flag.ContinueOnError)
	flags.SetOutput(errOut)
	port := flags.Int("port", 8443, "aggregation TLS port")
	cert := flags.String("tls-cert", "", "aggregation certificate file")
	key := flags.String("tls-key", "", "aggregation private key file")
	controlCert := flags.String("node-client-cert", "", "private node client certificate file")
	controlKey := flags.String("node-client-key", "", "private node client key file")
	registry := flags.String("node-registry", "", "installation-owned node endpoint registry")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("admission API accepts no positional arguments")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("admission API requires in-cluster Kubernetes credentials")
	}
	client, err := admissionkube.NewClient(config)
	if err != nil {
		return err
	}
	identity, err := loadIdentity(*controlCert, *controlKey)
	if err != nil {
		return err
	}
	endpoints, err := loadEndpoints(*registry)
	if err != nil {
		return err
	}
	binder, err := nodebinding.NewClient(identity, endpoints)
	if err != nil {
		return err
	}
	defer binder.Close()
	manager, err := admission.NewManager(ctx, admission.Dependencies{Authorizer: admissionkube.NewAuthorizer(client.AuthorizationV1().SubjectAccessReviews()), Resolver: admissionkube.NewResolver(client.CoreV1()), Binder: binder, Audit: func(event admission.AuditEvent) {
		fmt.Fprintf(errOut, "trace_admission operation=%s decision=%s reason=%s principal=%s\n", event.Operation, event.Decision, event.Reason, event.Principal)
	}}, admission.DefaultPolicy())
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if manager.Close(closeCtx) != nil {
			fmt.Fprintln(errOut, "trace_admission cleanup_unconfirmed")
			runErr = errors.New("trace admission cleanup unconfirmed")
		}
	}()
	return (admissionapi.ServerOptions{Port: *port, CertificateFile: *cert, KeyFile: *key, Client: client, Manager: manager}).Run(ctx)
}
