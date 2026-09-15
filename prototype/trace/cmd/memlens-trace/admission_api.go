package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"io"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionapi"
	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionkube"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"k8s.io/client-go/rest"
)

func runAdmissionAPI(ctx context.Context, args []string, errOut io.Writer) error {
	return runAdmissionAPIConfigured(ctx, args, errOut, nil, admission.DefaultPolicy())
}
func runAdmissionAPIConfigured(ctx context.Context, args []string, errOut io.Writer, proxy *admissionapi.StreamProxy, policy admission.Policy) (runErr error) {
	flags := flag.NewFlagSet("admission-api", flag.ContinueOnError)
	flags.SetOutput(errOut)
	port := flags.Int("port", 8443, "aggregation TLS port")
	cert := flags.String("tls-cert", "", "aggregation certificate file")
	key := flags.String("tls-key", "", "aggregation private key file")
	controlCert := flags.String("node-client-cert", "", "private node client certificate file")
	controlKey := flags.String("node-client-key", "", "private node client key file")
	registry := flags.String("node-registry", "", "installation-owned node endpoint registry")
	acceptance := flags.String("acceptance-policy", "", "independently accepted worker installation policy")
	confirmedPaths := flags.Bool("allow-confirmed-paths", policy.Paths == trace.ConfirmedPaths, "permit explicitly confirmed bounded file paths")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("admission API accepts no positional arguments")
	}
	if *acceptance != "" {
		if proxy != nil {
			return errors.New("worker installation cannot replace a configured stream")
		}
		var err error
		proxy, policy, err = configureStream(*acceptance, policy)
		if err != nil {
			return err
		}
	}
	if *confirmedPaths {
		if proxy == nil {
			return errors.New("confirmed paths require an accepted stream installation")
		}
		policy.Paths = trace.ConfirmedPaths
	} else {
		policy.Paths = trace.OmitPaths
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
	resolver := admissionkube.NewResolver(client.CoreV1())
	if proxy != nil {
		proxy = proxy.WithOOMContext(resolver)
	}
	manager, err := admission.NewManager(ctx, admission.Dependencies{Authorizer: admissionkube.NewAuthorizer(client.AuthorizationV1().SubjectAccessReviews()), Resolver: resolver, Binder: binder, Audit: func(event admission.AuditEvent) {
		fmt.Fprintf(errOut, "trace_admission operation=%s decision=%s reason=%s principal=%s\n", event.Operation, event.Decision, event.Reason, event.Principal)
	}}, policy)
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
	return (admissionapi.ServerOptions{Port: *port, CertificateFile: *cert, KeyFile: *key, Client: client, Manager: manager, Stream: proxy}).Run(ctx)
}
