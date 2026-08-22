package actions

import "testing"

func TestScopedWhere_AllReadPaths(t *testing.T) {
	cases := []struct{ name, base, want string }{
		{"dashboard status", "", "WHERE instance_id = ?"},
		{"report location", "WHERE discovery_city IS NOT NULL", "WHERE discovery_city IS NOT NULL AND instance_id = ?"},
		{"report type", "WHERE animal_type IS NOT NULL", "WHERE animal_type IS NOT NULL AND instance_id = ?"},
		{"report species", "WHERE species IS NOT NULL", "WHERE species IS NOT NULL AND instance_id = ?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, args := ScopedWhere(ResolveReportScope("center-north"), tc.base)
			if got != tc.want {
				t.Fatalf("where = %q, want %q", got, tc.want)
			}
			if len(args) != 1 || args[0] != "center-north" {
				t.Fatalf("args = %#v", args)
			}
		})
	}
}

func TestResolveReportScope_GlobalAndInstance(t *testing.T) {
	if !ResolveReportScope("").IsGlobal() {
		t.Fatal("empty scope must be global")
	}
	if ResolveReportScope("center-north").IsGlobal() {
		t.Fatal("instance scope marked global")
	}
}
