package registry

import (
	"errors"
	"testing"
)

func NewTestRegistry() (*ServiceRegistry, *MemoryBackend) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	b := NewMemoryBackend()
	r.backend = b
	return r, b
}

func TestListAssignmentKeyFormat(t *testing.T) {
	r, b := NewTestRegistry()

	b.MembersFunc = func(key string) ([]string, error) {
		if key != "dev/pools/foo" {
			t.Errorf("ListAssignments(%q) wrong key, want %s", key, "dev/pools/foo")
		}
		return []string{}, nil
	}

	r.ListAssignments("foo")
}

func TestListAssignmentsEmpty(t *testing.T) {
	r, _ := NewTestRegistry()

	assignments, err := r.ListAssignments("foo")
	if err != nil {
		t.Error(err)
	}

	if len(assignments) != 0 {
		t.Errorf("ListAssignments(%q) = %d, want %d", "foo", len(assignments), 0)
	}
}

func TestListAssignmentsNotEmpty(t *testing.T) {
	r, _ := NewTestRegistry()

	r.CreatePool("web")
	for _, k := range []string{"one", "two"} {
		assertAppCreated(t, r, k)
		if assigned, err := r.AssignApp(k); !assigned || err != nil {
			t.Fatalf("AssignApp(%q) = %t, %v, want %t, %v", k, assigned, err, true, nil)
		}
	}

	var assignments []string
	var err error
	if assignments, err = r.ListAssignments("web"); len(assignments) != 2 || err != nil {
		t.Fatalf("ListAssignments(%q) = %d, %v, want %d, %v", "web", len(assignments), err, 2, nil)
	}

	if assignments[0] != "one" {
		t.Fatalf("assignments[0] = %v, want %v", assignments[0], "one")
	}

	if assignments[1] != "two" {
		t.Fatalf("assignments[1] = %v, want %v", assignments[0], "two")
	}
}

func TestAppExistsKeyFormat(t *testing.T) {
	r, b := NewTestRegistry()

	b.KeysFunc = func(key string) ([]string, error) {
		if key != "dev/foo/*" {
			t.Errorf("AppExists(%q) wrong key, want %s", key, "dev/foo/*")
		}
		return []string{}, nil
	}

	r.AppExists("foo")
}

func TestAppNotExists(t *testing.T) {
	r, _ := NewTestRegistry()

	if exists, err := r.AppExists("foo"); exists || err != nil {
		t.Errorf("AppExists(%q) = %t, %v, want %t, %v",
			"foo", exists, err, false, nil)
	}
}

func TestAppExists(t *testing.T) {
	r, _ := NewTestRegistry()

	assertAppCreated(t, r, "foo")

	if exists, err := r.AppExists("foo"); !exists || err != nil {
		t.Errorf("AppExists(%q) = %t, %v, want %t, %v", "foo", exists, err, true, nil)
	}
}

func TestCountInstancesKeyFormat(t *testing.T) {
	r, b := NewTestRegistry()

	b.KeysFunc = func(key string) ([]string, error) {
		if key != "dev/*/hosts/*/foo" {
			t.Errorf("CountInstances(%q) wrong key, want %s", key, "dev/web/hosts/*/foo")
		}
		return []string{}, nil
	}

	r.CountInstances("foo")
}

func TestCountInstancesOne(t *testing.T) {
	r, b := NewTestRegistry()

	b.KeysFunc = func(key string) ([]string, error) {
		if key != "dev/*/hosts/*/foo" {
			t.Errorf("CountInstances(%q) wrong key, want %s", key, "dev/web/hosts/*/foo")
		}
		return []string{"dev/web/hosts/me/foo"}, nil
	}

	got := r.CountInstances("foo")
	if got != 1 {
		t.Errorf("CountInstances(%q) = %v, want %v", "foo", got, 1)
	}
}

func TestAssignAppNotExists(t *testing.T) {
	r, _ := NewTestRegistry()

	assigned, err := r.AssignApp("foo")
	if assigned {
		t.Errorf("AssignApp(%q) = %t, want %t", "foo", assigned, false)
	}

	if err != nil {
		t.Error(err)
	}
}

func TestAssignAppPoolExists(t *testing.T) {
	r, _ := NewTestRegistry()

	assertAppCreated(t, r, "app")

	if assigned, err := r.AssignApp("app"); assigned || err == nil {
		t.Errorf("AssignApp(%q) = %t, %v, want %t, %v", "app", assigned, err,
			false, errors.New("pool web does not exist"))
	}
}

func TestAssignAppAddMemberFail(t *testing.T) {
	r, b := NewTestRegistry()

	assertAppCreated(t, r, "app")

	if created, err := r.CreatePool("web"); !created || err != nil {
		t.Errorf("CreatePool() = %t, %v, want %t, %v", created, err, true, nil)
	}

	b.AddMemberFunc = func(key, value string) (int, error) {
		return 0, errors.New("something failed")
	}

	if assigned, err := r.AssignApp("app"); assigned || err == nil {
		t.Errorf("AssignApp(%q) = %t, %v, want %t, %v", "app", assigned, err, false,
			errors.New("something failed"))
	}
}

func TestUnassignAppNotExists(t *testing.T) {
	r, _ := NewTestRegistry()

	if unassigned, err := r.UnassignApp("foo"); unassigned || err != nil {
		t.Errorf("UnAssignApp(%q) = %t, %v, want %t, %v", "foo", unassigned, err, false, nil)
	}
}

func TestUnassignAppRemoveMemberFail(t *testing.T) {
	r, b := NewTestRegistry()

	assertAppCreated(t, r, "app")

	if created, err := r.CreatePool("web"); !created || err != nil {
		t.Errorf("CreatePool() = %t, %v, want %t, %v", created, err, true, nil)
	}

	if assigned, err := r.AssignApp("app"); !assigned || err != nil {
		t.Errorf("AssignApp(%q) = %t, %v, want %t, %v", "app", assigned, err, true, nil)
	}

	b.RemoveMemberFunc = func(key, value string) (int, error) {
		return 0, errors.New("something failed")
	}

	if unassigned, err := r.UnassignApp("foo"); unassigned || err == nil {
		t.Errorf("UnAssignApp(%q) = %t, %v, want %t, %v", "foo", unassigned, err,
			false, errors.New("something failed"))
	}
}

func TestUnassignAppAddMemberNotifyRestart(t *testing.T) {
	r, b := NewTestRegistry()

	assertAppCreated(t, r, "app")

	if created, err := r.CreatePool("web"); !created || err != nil {
		t.Errorf("CreatePool() = %t, %v, want %t, %v", created, err, true, nil)
	}

	if assigned, err := r.AssignApp("app"); !assigned || err != nil {
		t.Errorf("AssignApp() = %t, %v, want %t, %v", assigned, err, true, nil)
	}

	b.NotifyFunc = func(key, value string) (int, error) {
		if key != "galaxy-dev" {
			t.Errorf("UnassignApp(%q) wrong notify key, want %s. got %s", "app", key, "galaxy-dev")
		}

		if value != "restart app" {
			t.Errorf("UnassignApp(%q) wrong notify value, want %s. got %s", "app", value, "restart app")
		}
		return 1, nil
	}
	if unassigned, err := r.UnassignApp("app"); !unassigned || err != nil {
		t.Errorf("UnAssignApp(%q) = %t, %v, want %t, %v", "app", unassigned, err, true, nil)
	}
}

func TestCreatePool(t *testing.T) {
	r, _ := NewTestRegistry()

	if created, err := r.CreatePool("foo"); !created || err != nil {
		t.Errorf("CreatePool(%q) = %tm %v, want %t, %v", "foo", created, err, true, nil)
	}
}

func TestDeletePool(t *testing.T) {
	r, _ := NewTestRegistry()

	if created, err := r.CreatePool("foo"); !created || err != nil {
		t.Errorf("CreatePool() = %t, %v, want %t, %v", created, err, true, nil)
	}

	r.Pool = "foo"
	if exists, err := r.PoolExists(); !exists || err != nil {
		t.Errorf("PoolExists()) = %t, %v, want %t, %v", exists, err, true, nil)
	}

	if deleted, err := r.DeletePool("foo"); !deleted || err != nil {
		t.Errorf("DeletePool(%q) = %t, %v, want %t, %v", "foo", deleted, err, true, nil)
	}
}

func TestDeletePoolHasAssignments(t *testing.T) {
	r, _ := NewTestRegistry()

	assertAppCreated(t, r, "app")

	if created, err := r.CreatePool("foo"); !created || err != nil {
		t.Errorf("CreatePool() = %t, %v, want %t, %v", created, err, true, nil)
	}

	// This is weird. AssignApp should probably take app & pool as params.
	r.Pool = "foo"
	if assigned, err := r.AssignApp("app"); !assigned || err != nil {
		t.Errorf("AssignApp() = %t, %v, want %t, %v", assigned, err, true, nil)
	}

	// Should fail.  Can't delete a pool if apps are assigned
	if deleted, err := r.DeletePool("foo"); deleted || err != nil {
		t.Errorf("DeletePool(%q) = %t, %v, want %t, %v", "foo", deleted, err, false, nil)
	}
}

func TestListPools(t *testing.T) {
	r, _ := NewTestRegistry()

	for _, pool := range []string{"one", "two"} {
		if created, err := r.CreatePool(pool); !created || err != nil {
			t.Errorf("CreatePool(%q) = %t, %v, want %t, %v", pool, created, err, true, nil)
		}
	}

	if pools, err := r.ListPools(); len(pools) == 0 || err != nil {
		t.Errorf("ListPools() = %d, %v, want %d, %v", len(pools), err, 2, nil)
	}
}

func TestCreateApp(t *testing.T) {
	r, _ := NewTestRegistry()

	assertAppCreated(t, r, "app")
}

func TestCreateAppAlreadyExists(t *testing.T) {
	r, _ := NewTestRegistry()

	assertAppCreated(t, r, "app")

	if created, err := r.CreateApp("app"); created || err != nil {
		t.Fatalf("CreateApp() = %t, %v, want %t, %v",
			created, err,
			false, nil)
	}
}

func TestCreateAppError(t *testing.T) {
	r, b := NewTestRegistry()

	b.KeysFunc = func(key string) ([]string, error) {
		return []string{}, errors.New("something failed")
	}

	if created, err := r.CreateApp("foo"); created || err == nil {
		t.Fatalf("CreateApp() = %t, %v, want %t, %v",
			created, err,
			false, errors.New("something failed"))
	}
}

func TestDeleteApp(t *testing.T) {
	r, _ := NewTestRegistry()

	assertAppCreated(t, r, "app")

	if exists, err := r.AppExists("app"); !exists || err != nil {
		t.Fatalf("AppExists(%q) = %t, %v, want %t, %v", "app", exists, err,
			true, nil)
	}

	if deleted, err := r.DeleteApp("app"); !deleted || err != nil {
		t.Fatalf("DeleteApp(%q) = %t, %v, want %t, %v", "app", deleted, err,
			true, nil)
	}
}

func TestDeleteAppStillAssigned(t *testing.T) {
	r, _ := NewTestRegistry()

	assertAppCreated(t, r, "app")

	if exists, err := r.AppExists("app"); !exists || err != nil {
		t.Fatalf("AppExists(%q) = %t, %v, want %t, %v", "app", exists, err,
			true, nil)
	}

	if created, err := r.CreatePool("web"); !created || err != nil {
		t.Fatalf("CreatePool(%q) = %t, %v, want %t, %v", "web", created, err,
			true, nil)
	}

	if assigned, err := r.AssignApp("app"); !assigned || err != nil {
		t.Fatalf("AssignApp(%q) = %t, %v, want %t, %v", "app", assigned, err,
			true, nil)
	}

	if deleted, err := r.DeleteApp("app"); deleted || err == nil {
		t.Fatalf("DeleteApp(%q) = %t, %v, want %t, %v", "app", deleted, err,
			false, errors.New("app is assigned to pool web"))
	}
}

func TestListApps(t *testing.T) {
	r, _ := NewTestRegistry()

	if apps, err := r.ListApps(); len(apps) > 0 || err != nil {
		t.Fatalf("ListApps() = %d, %v, want %d, %v", len(apps), err,
			0, nil)
	}

	for _, k := range []string{"one", "two"} {
		assertAppCreated(t, r, k)
	}

	if apps, err := r.ListApps(); len(apps) != 2 || err != nil {
		t.Fatalf("ListApps() = %d, %v, want %d, %v", len(apps), err,
			2, nil)
	}
}

func TestListAppsIgnoreSpecialKeys(t *testing.T) {
	r, b := NewTestRegistry()

	b.maps["dev/hosts/environment"] = make(map[string]string)

	if apps, err := r.ListApps(); len(apps) > 0 || err != nil {
		t.Fatalf("ListApps() = %d, %v, want %d, %v", len(apps), err,
			0, nil)
	}
}

func TestListEnvs(t *testing.T) {
	r, b := NewTestRegistry()

	b.maps["dev/hosts/environment"] = make(map[string]string)
	b.maps["prod/web/foo/environment"] = make(map[string]string)
	b.maps["prod/hosts/environment"] = make(map[string]string)

	if apps, err := r.ListEnvs(); len(apps) != 2 || err != nil {
		t.Fatalf("ListApps() = %d, %v, want %d, %v", len(apps), err,
			2, nil)
	}
}

func assertAppCreated(t *testing.T, r *ServiceRegistry, app string) {
	if created, err := r.CreateApp(app); !created || err != nil {
		t.Fatalf("CreateApp(%q) = %t, %v, want %t, %v", app,
			created, err,
			true, nil)
	}
}
