package verifier

import (
	"bytes"
	"testing"

	"github.com/cilium/ebpf/btf"
)

func abiFixture(t *testing.T, mutate func(*btf.Func, *btf.Func)) *btf.Spec {
	t.Helper()
	signed := &btf.Int{Name: "int", Size: 4, Encoding: btf.Signed}
	unsigned := &btf.Int{Name: "unsigned int", Size: 4, Encoding: btf.Unsigned}
	check := &btf.Func{Name: "bpf_check", Type: &btf.FuncProto{Return: signed}, Linkage: btf.GlobalFunc}
	finalize := &btf.Func{Name: "bpf_vlog_finalize", Linkage: btf.GlobalFunc, Type: &btf.FuncProto{
		Return: signed, Params: []btf.FuncParam{
			{Name: "log", Type: &btf.Pointer{Target: &btf.Struct{Name: "bpf_verifier_log", Size: 8}}},
			{Name: "log_size_actual", Type: &btf.Pointer{Target: &btf.Typedef{Name: "u32", Type: unsigned}}},
		}}}
	mutate(check, finalize)
	builder, err := btf.NewBuilder([]btf.Type{check, finalize}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := builder.Marshal(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := btf.LoadSpecFromReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestVerifierABIRequiresTypedFinalizerOutput(t *testing.T) {
	if err := ValidateABI(abiFixture(t, func(_, _ *btf.Func) {})); err != nil {
		t.Fatal(err)
	}
	if ValidateABI(nil) == nil {
		t.Fatal("missing BTF accepted")
	}
	for _, mutate := range []func(*btf.Func, *btf.Func){
		func(check, _ *btf.Func) { check.Name = "other_function" },
		func(_, finalize *btf.Func) {
			finalize.Type.(*btf.FuncProto).Return = &btf.Int{Name: "unsigned int", Size: 4}
		},
		func(_, finalize *btf.Func) {
			finalize.Type.(*btf.FuncProto).Params = finalize.Type.(*btf.FuncProto).Params[:1]
		},
		func(_, finalize *btf.Func) {
			finalize.Type.(*btf.FuncProto).Params[0].Type = &btf.Pointer{Target: &btf.Struct{Name: "other_log", Size: 8}}
		},
		func(_, finalize *btf.Func) {
			finalize.Type.(*btf.FuncProto).Params[1].Type = &btf.Int{Name: "unsigned int", Size: 4}
		},
		func(_, finalize *btf.Func) {
			finalize.Type.(*btf.FuncProto).Params[1].Type = &btf.Pointer{Target: &btf.Int{Name: "unsigned long", Size: 8}}
		},
		func(_, finalize *btf.Func) {
			finalize.Type.(*btf.FuncProto).Params[1].Type = &btf.Pointer{Target: &btf.Int{Name: "int", Size: 4, Encoding: btf.Signed}}
		},
	} {
		if ValidateABI(abiFixture(t, mutate)) == nil {
			t.Fatal("changed native argument ABI accepted")
		}
	}
}
