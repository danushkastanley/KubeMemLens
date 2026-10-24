module github.com/danushkastanley/kube-memlens/prototype/trace/worker

go 1.27.3

require (
	github.com/cilium/ebpf v0.22.0
	github.com/danushkastanley/kube-memlens v0.0.0
	github.com/danushkastanley/kube-memlens/prototype/trace v0.0.0
	github.com/inspektor-gadget/inspektor-gadget v0.56.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/spf13/viper v1.21.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/Azure/go-ntlmssp v0.1.1 // indirect
	github.com/Microsoft/go-winio v0.6.3-0.20251027160822-ad3df93bed29 // indirect
	github.com/Microsoft/hcsshim v0.15.0-rc.1 // indirect
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/cgroups/v3 v3.1.3 // indirect
	github.com/containerd/containerd/api v1.11.1 // indirect
	github.com/containerd/continuity v0.5.0 // indirect
	github.com/containerd/platforms v1.0.0-rc.5 // indirect
	github.com/containerd/plugin v1.1.0 // indirect
	github.com/containerd/ttrpc v1.2.8 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/cli v29.6.2+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.8-0.20250403174932-29230038a667 // indirect
	github.com/go-ldap/ldap/v3 v3.4.12 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/google/go-containerregistry v0.21.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/in-toto/attestation v1.2.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.19.0 // indirect
	github.com/moby/sys/signal v0.7.1 // indirect
	github.com/moby/sys/user v0.4.1 // indirect
	github.com/notaryproject/notation-core-go v1.3.0 // indirect
	github.com/notaryproject/notation-go v1.3.2 // indirect
	github.com/notaryproject/notation-plugin-framework-go v1.0.0 // indirect
	github.com/notaryproject/tspclient-go v1.0.0 // indirect
	github.com/opencontainers/runtime-spec v1.3.0 // indirect
	github.com/pelletier/go-toml/v2 v2.3.0 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/secure-systems-lab/go-securesystemslib v0.11.0 // indirect
	github.com/sigstore/protobuf-specs v0.5.1 // indirect
	github.com/sigstore/sigstore v1.10.8 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/veraison/go-cose v1.3.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	go.opentelemetry.io/otel v1.45.0 // indirect
	go.opentelemetry.io/otel/metric v1.45.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/exp v0.0.0-20260410095643-746e56fc9e2f // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gotest.tools/v3 v3.5.2 // indirect
	k8s.io/apimachinery v0.37.1 // indirect
	k8s.io/apiserver v0.37.1 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/utils v0.0.0-20260626114624-be93311217bd // indirect
	oras.land/oras-go/v2 v2.6.2 // indirect
)

replace github.com/danushkastanley/kube-memlens => ../../..

replace github.com/danushkastanley/kube-memlens/prototype/trace => ..

// Generated from the checksum-pinned module plus sdk-policy.patch.
replace github.com/inspektor-gadget/inspektor-gadget => ./.sdk

// Preserve the reviewed upstream replacements in this isolated worker graph.
replace github.com/notaryproject/notation-go => github.com/inspektor-gadget/notation-go v1.3.3

replace go.opentelemetry.io/ebpf-profiler => github.com/inspektor-gadget/opentelemetry-ebpf-profiler v0.0.202611-0.20260715114040-ef6cf29492c8
