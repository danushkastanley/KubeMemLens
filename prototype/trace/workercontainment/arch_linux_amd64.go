package workercontainment

import "golang.org/x/sys/unix"

const auditArchitecture = unix.AUDIT_ARCH_X86_64

var legacyForkCalls = []uint32{unix.SYS_FORK, unix.SYS_VFORK}
