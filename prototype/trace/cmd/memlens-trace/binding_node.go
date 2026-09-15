package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
	"golang.org/x/net/netutil"
)

func runBindingNode(ctx context.Context, args []string, errOut io.Writer) error {
	return runBindingNodeRuntime(ctx, args, errOut, nil)
}
func runBindingNodeRuntime(ctx context.Context, args []string, errOut io.Writer, runtime nodebinding.Runtime) (runErr error) {
	flags := flag.NewFlagSet("binding-node", flag.ContinueOnError)
	flags.SetOutput(errOut)
	address := flags.String("listen", ":9443", "private TLS listener")
	cert := flags.String("tls-cert", "", "node certificate file")
	key := flags.String("tls-key", "", "node private key file")
	ca := flags.String("control-ca", "", "control certificate CA file")
	pin := flags.String("control-certificate-sha256", "", "exact control leaf certificate digest")
	uid := flags.String("node-uid", "", "installation-bound Node UID")
	name := flags.String("node-name", "", "installation-bound Node name")
	root := flags.String("kubelet-cgroup-root", "", "explicit kubelet cgroup root")
	bundle := flags.String("bundle", "/opt/memlens-trace/reference", "frozen preflight reference directory")
	acceptance := flags.String("acceptance-policy", "", "independently accepted worker installation policy")
	executable := flags.String("worker-executable", "/opt/memlens-trace/memlens-filecache-worker", "accepted worker executable")
	programmes := flags.String("programme-bundle", "/opt/memlens-trace/programmes", "accepted programme bundle directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *uid == "" || *name == "" || *root == "" {
		return errors.New("binding node requires exact installation identity and cgroup root")
	}
	if *acceptance != "" {
		if runtime != nil {
			return errors.New("worker installation cannot replace a configured runtime")
		}
		installed, err := configureWorker(ctx, *acceptance, *executable, *programmes)
		if err != nil {
			return err
		}
		runtime = installed
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if installed.Close(closeCtx) != nil {
				runErr = errors.New("worker runtime cleanup unconfirmed")
			}
		}()
	}
	identity, err := loadIdentity(*cert, *key)
	if err != nil {
		return err
	}
	control, err := loadPeer(*ca, *pin)
	if err != nil {
		return err
	}
	tlsConfig, err := nodebinding.ServerTLS(identity, control)
	if err != nil {
		return err
	}
	// Execute the existing supervised, non-attaching baseline in this very
	// runtime before opening the listener. No externally supplied report is used.
	report := supervise(ctx, *bundle, 10*time.Second)
	if report.State != tracepreflight.Supported {
		if err := writeJSON(errOut, report); err != nil {
			return errors.New("binding node preflight report unavailable")
		}
		return errors.New("binding node preflight is incomplete or unsupported")
	}
	service, err := nodebinding.NewService(ctx, *uid, *name, control, func(ctx context.Context, w admission.Workload) (targetfs.Handle, error) {
		return targetfs.Resolve(ctx, targetfs.Config{MountPoint: "/sys/fs/cgroup", KubeletRoot: *root}, w)
	}, func(ctx context.Context) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return tracepreflight.Baseline().Digest(), nil
	}, func(reason string) { fmt.Fprintln(errOut, "binding_node", reason) }, runtime)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if service.Close(closeCtx) != nil {
			fmt.Fprintln(errOut, "binding_node cleanup_unconfirmed")
			runErr = errors.New("binding node cleanup unconfirmed")
		}
	}()
	server, err := nodebinding.NewHTTPServer(*address, tlsConfig, service)
	if err != nil {
		return err
	}
	// TLS handshake diagnostics are reduced to a fixed category; they must not
	// retain peer addresses or supplied certificate names.
	server.ErrorLog = log.New(fixedDiagnostic{errOut}, "", 0)
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return errors.New("binding node listener unavailable")
	}
	listener = netutil.LimitListener(listener, 16)
	results := make(chan error, 1)
	go func() { results <- server.ServeTLS(listener, "", "") }()
	select {
	case err = <-results:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.New("binding node server failed")
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = server.Shutdown(shutdown)
		cancel()
		if err != nil {
			_ = server.Close()
			return errors.New("binding node shutdown incomplete")
		}
		if err = <-results; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return errors.New("binding node server failed")
		}
	}
	return nil
}

type fixedDiagnostic struct{ out io.Writer }

func (w fixedDiagnostic) Write(data []byte) (int, error) {
	_, err := io.WriteString(w.out, "binding_node transport_failure\n")
	return len(data), err
}
