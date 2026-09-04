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

func TestParseIDs(t *testing.T) {
	cases := []struct {
		puid, pgid string
		uid, gid   int
		wantErr    bool
	}{
		{"", "", 1000, 1000, false},   // defaults: the image's backup user
		{"99", "100", 99, 100, false}, // Unraid nobody:users
		{"abc", "", 0, 0, true},       // not a number
		{"0", "100", 0, 0, true},      // root is never a drop target
	}
	for _, tc := range cases {
		uid, gid, err := parseIDs(tc.puid, tc.pgid)
		if (err != nil) != tc.wantErr || uid != tc.uid || gid != tc.gid {
			t.Errorf("parseIDs(%q, %q) = %d, %d, %v; want %d, %d, err=%v",
				tc.puid, tc.pgid, uid, gid, err, tc.uid, tc.gid, tc.wantErr)
		}
	}
}
