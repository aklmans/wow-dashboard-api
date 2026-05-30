package rbac

import (
	"reflect"
	"testing"
)

// TestCatalogExcludesWildcard locks in the security-critical invariant that the
// assignable catalog never contains the admin-only wildcard: if "*" ever leaked
// into Catalog, IsAssignable would let a custom role be granted every permission.
func TestCatalogExcludesWildcard(t *testing.T) {
	for _, p := range Catalog {
		if p == PermissionAll {
			t.Fatal("Catalog must not contain the PermissionAll wildcard")
		}
	}
}

// TestCatalogHasNoDuplicates guards List/IsAssignable correctness — a duplicate
// entry would make List emit the same permission twice.
func TestCatalogHasNoDuplicates(t *testing.T) {
	seen := make(map[Permission]struct{}, len(Catalog))
	for _, p := range Catalog {
		if _, dup := seen[p]; dup {
			t.Fatalf("Catalog contains duplicate permission %q", p)
		}
		seen[p] = struct{}{}
	}
}

func TestIsAssignable(t *testing.T) {
	tests := []struct {
		name string
		perm Permission
		want bool
	}{
		{"catalog permission", PermissionUsersRead, true},
		{"another catalog permission", PermissionProjectsCreate, true},
		{"wildcard is not assignable", PermissionAll, false},
		{"unknown string is not assignable", Permission("billing:export"), false},
		{"empty string is not assignable", Permission(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAssignable(tt.perm); got != tt.want {
				t.Fatalf("IsAssignable(%q) = %v, want %v", tt.perm, got, tt.want)
			}
		})
	}

	// Every catalog entry must report as assignable.
	for _, p := range Catalog {
		if !IsAssignable(p) {
			t.Fatalf("catalog permission %q reported as not assignable", p)
		}
	}
}

func TestSetHas(t *testing.T) {
	t.Run("direct grant", func(t *testing.T) {
		s := NewSet([]string{string(PermissionUsersRead), string(PermissionRolesRead)})
		if !s.Has(PermissionUsersRead) {
			t.Error("Has(users:read) = false, want true for a directly granted permission")
		}
		if s.Has(PermissionUsersManage) {
			t.Error("Has(users:manage) = true, want false for an ungranted permission")
		}
	})

	t.Run("wildcard grants everything", func(t *testing.T) {
		s := NewSet([]string{string(PermissionAll)})
		for _, p := range Catalog {
			if !s.Has(p) {
				t.Errorf("wildcard set: Has(%q) = false, want true", p)
			}
		}
	})

	t.Run("empty set grants nothing", func(t *testing.T) {
		s := NewSet(nil)
		if s.Has(PermissionUsersRead) {
			t.Error("empty set granted a permission")
		}
	})
}

func TestSetList(t *testing.T) {
	t.Run("returns granted permissions in catalog order", func(t *testing.T) {
		// Provide them out of catalog order to prove List re-orders.
		s := NewSet([]string{string(PermissionProjectsCreate), string(PermissionUsersRead)})
		want := []string{string(PermissionUsersRead), string(PermissionProjectsCreate)}
		if got := s.List(); !reflect.DeepEqual(got, want) {
			t.Fatalf("List() = %v, want %v", got, want)
		}
	})

	t.Run("wildcard is listed first and not expanded to the full catalog", func(t *testing.T) {
		s := NewSet([]string{string(PermissionUsersRead), string(PermissionAll)})
		want := []string{string(PermissionAll), string(PermissionUsersRead)}
		if got := s.List(); !reflect.DeepEqual(got, want) {
			t.Fatalf("List() = %v, want %v", got, want)
		}
	})

	t.Run("drops raw strings that are not in the catalog", func(t *testing.T) {
		// A stray value from role_permissions must not leak to clients via List.
		s := NewSet([]string{"billing:export", string(PermissionRolesRead)})
		want := []string{string(PermissionRolesRead)}
		if got := s.List(); !reflect.DeepEqual(got, want) {
			t.Fatalf("List() = %v, want %v", got, want)
		}
	})

	t.Run("empty set lists nothing", func(t *testing.T) {
		if got := NewSet(nil).List(); len(got) != 0 {
			t.Fatalf("List() = %v, want empty", got)
		}
	})
}
