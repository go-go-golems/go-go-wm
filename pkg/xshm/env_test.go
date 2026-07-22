package xshm

import "testing"

// TestEnvDisabled pins the switch semantics. GO_GO_WM_NO_SHM=0 previously
// disabled shared memory because the check was a bare non-empty test, and a
// measurement harness that set it to 0 to mean "enabled" produced a false
// reading that was reported as a hardware property (GGWM-012 Step 16).
func TestEnvDisabled(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"no", false},
		{"off", false},
		{" 0 ", false},
		{"1", true},
		{"true", true},
		{"yes", true},
		{"anything", true},
	}
	for _, c := range cases {
		t.Setenv("GO_GO_WM_TEST_SWITCH", c.val)
		if got := envDisabled("GO_GO_WM_TEST_SWITCH"); got != c.want {
			t.Errorf("envDisabled(%q) = %v, want %v", c.val, got, c.want)
		}
	}
}
