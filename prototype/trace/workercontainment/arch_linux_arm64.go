package workercontainment

import "golang.org/x/sys/unix"

const auditArchitecture = unix.AUDIT_ARCH_AARCH64

var legacyForkCalls = []uint32{}
