package verifierfixture

import (
	"testing"
	"unsafe"
)

func TestCalibrationUAPIPrefixIncludesOnlyBoundedLogOutput(t *testing.T) {
	var attr loadAttr
	if unsafe.Sizeof(attr) != 144 || unsafe.Offsetof(attr.Instructions) != 8 || unsafe.Offsetof(attr.LogBuffer) != 32 ||
		unsafe.Offsetof(attr.Name) != 48 || unsafe.Offsetof(attr.LogTrueSize) != 140 {
		t.Fatal("load ABI changed")
	}
	if _, err := Run("unreviewed"); err == nil {
		t.Fatal("arbitrary input reached syscall")
	}
}
