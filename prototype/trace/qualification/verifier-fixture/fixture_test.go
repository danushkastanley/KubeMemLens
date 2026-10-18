package verifierfixture

import "testing"

func TestOnlyThreeFixedTwoInstructionCasesAreAvailable(t *testing.T) {
	for _, which := range []Case{NoLog, WithLog, Rejected} {
		d, err := describe(which)
		if err != nil || len(d.code) != 16 || d.code[0] != 0xb7 || d.code[8] != 0x95 {
			t.Fatal("fixed instruction sequence changed")
		}
		if (which == NoLog) != (d.level == 0) || d.level > 1 {
			t.Fatal("unbounded log level")
		}
		for index, b := range d.code {
			if index != 0 && index != 1 && index != 8 && b != 0 {
				t.Fatal("instruction acquired unreviewed operands")
			}
		}
		if (which == Rejected) != (d.code[1] == 1) {
			t.Fatal("rejection fixture no longer leaves r0 unreadable")
		}
	}
	for _, which := range []Case{"", "arbitrary", "accepted-with-log\n"} {
		if _, err := describe(which); err == nil {
			t.Fatal("unreviewed fixture accepted")
		}
	}
}
