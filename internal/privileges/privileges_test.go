package privileges

import "testing"

func TestShouldDrop(t *testing.T) {
	cases := []struct {
		uid     int
		dropped string
		want    bool
	}{
		{0, "", true},     // root, not yet dropped
		{0, "1", false},   // already re-executed
		{1000, "", false}, // already unprivileged (e.g. user: in compose)
	}
	for _, tc := range cases {
		if got := shouldDrop(tc.uid, tc.dropped); got != tc.want {
			t.Errorf("shouldDrop(%d, %q) = %v, want %v", tc.uid, tc.dropped, got, tc.want)
		}
	}
}
