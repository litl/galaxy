package registry

import (
	"errors"
	"testing"
)

type fakeBackend struct {
	MembersFunc      func(key string) ([]string, error)
	KeysFunc         func(key string) ([]string, error)
	AddMemberFunc    func(key, value string) (int, error)
	RemoveMemberFunc func(key, value string) (int, error)
	NotifyFunc       func(key, value string) (int, error)
	SetMultiFunc     func(key string, values map[string]string) (string, error)
}

func (f *fakeBackend) Connect()   {}
func (f *fakeBackend) Reconnect() {}

func (f *fakeBackend) Delete(key string) (int, error) {
	panic("not implemented")
}

func (f *fakeBackend) Expire(key string, ttl uint64) (int, error) {
	panic("not implemented")
}

func (f *fakeBackend) Ttl(key string) (int, error) {
	panic("not implemented")
}

func (f *fakeBackend) Members(key string) ([]string, error) {
	if f.MembersFunc != nil {
		return f.MembersFunc(key)
	}
	return []string{}, nil
}

func (f *fakeBackend) Keys(key string) ([]string, error) {
	if f.KeysFunc != nil {
		return f.KeysFunc(key)
	}
	return []string{}, nil
}

func (f *fakeBackend) AddMember(key, value string) (int, error) {
	if f.AddMemberFunc != nil {
		return f.AddMemberFunc(key, value)
	}
	return 0, nil
}

func (f *fakeBackend) RemoveMember(key, value string) (int, error) {
	if f.RemoveMemberFunc != nil {
		return f.RemoveMemberFunc(key, value)
	}
	return 0, nil

}

func (f *fakeBackend) Notify(key, value string) (int, error) {
	if f.NotifyFunc != nil {
		return f.NotifyFunc(key, value)
	}
	return 0, nil

}

func (f *fakeBackend) Set(key, field string, value string) (string, error) {
	panic("not implemented")
}

func (f *fakeBackend) Get(key, field string) (string, error) {
	panic("not implemented")
}

func (f *fakeBackend) GetAll(key string) (map[string]string, error) {
	panic("not implemented")
}

func (f *fakeBackend) SetMulti(key string, values map[string]string) (string, error) {
	if f.SetMultiFunc != nil {
		return f.SetMultiFunc(key, values)
	}
	return "OK", nil
}

func (f *fakeBackend) DeleteMulti(key string, fields ...string) (int, error) {
	panic("not implemented")
}

func (f *fakeBackend) Subscribe(key string) chan string {
	panic("not implemented")
}

func TestListAssignmentKeyFormat(t *testing.T) {
	r := &ServiceRegistry{
		Env: "dev",
	}
	r.backend = &fakeBackend{
		MembersFunc: func(key string) ([]string, error) {
			if key != "dev/pools/foo" {
				t.Errorf("ListAssignments(%q) wrong key, want %s", key, "dev/pools/foo")
			}
			return []string{}, nil
		},
	}

	r.ListAssignments("foo")
}

func TestListAssignmentsEmpty(t *testing.T) {
	r := &ServiceRegistry{
		Env: "dev",
	}
	r.backend = NewMemoryBackend()

	assignments, err := r.ListAssignments("foo")
	if err != nil {
		t.Error(err)
	}

	if len(assignments) != 0 {
		t.Errorf("ListAssignments(%q) = %d, want %d", "foo", len(assignments), 0)
	}
}

func TestListAssignmentsNotEmpty(t *testing.T) {
	r := &ServiceRegistry{
		Env: "dev",
	}

	r.backend = NewMemoryBackend()

	for _, k := range []string{"one", "two"} {
		if created, err := r.CreateApp(k); !created || err != nil {
			t.Fatalf("CreateApp(%q) = %t, %v, want %t, %v", k, created, err, true, nil)
		}
		r.Pool = "foo"
		if assigned, err := r.AssignApp(k); !assigned || err != nil {
			t.Fatalf("AssignApp(%q) = %t, %v, want %t, %v", k, assigned, err, true, nil)
		}
	}

	var assignments []string
	var err error
	if assignments, err = r.ListAssignments("foo"); len(assignments) != 2 || err != nil {
		t.Fatalf("ListAssignments(%q) = %d, %v, want %d, %v", "foo", len(assignments), err, 2, nil)
	}

	if assignments[0] != "one" {
		t.Fatalf("assignments[0] = %v, want %v", assignments[0], "one")
	}

	if assignments[1] != "two" {
		t.Fatalf("assignments[1] = %v, want %v", assignments[0], "two")
	}
}

func TestAppExistsKeyFormat(t *testing.T) {
	r := &ServiceRegistry{
		Env: "dev",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			if key != "dev/foo/*" {
				t.Errorf("AppExists(%q) wrong key, want %s", key, "dev/foo/*")
			}
			return []string{}, nil
		},
	}

	r.AppExists("foo")
}

func TestAppNotExists(t *testing.T) {
	r := &ServiceRegistry{
		Env: "dev",
	}
	r.backend = NewMemoryBackend()

	if exists, err := r.AppExists("foo"); exists || err != nil {
		t.Errorf("AppExists(%q) = %t, %v, want %t, %v",
			"foo", exists, err, false, nil)
	}
}

func TestAppExists(t *testing.T) {
	r := &ServiceRegistry{
		Env: "dev",
	}
	r.backend = NewMemoryBackend()

	if created, err := r.CreateApp("foo"); !created || err != nil {
		t.Errorf("CreateApp(%q) = %t, %v, want %t, %v", "foo", created, err, true, nil)
	}

	if exists, err := r.AppExists("foo"); !exists || err != nil {
		t.Errorf("AppExists(%q) = %t, %v, want %t, %v", "foo", exists, err, true, nil)
	}
}

func TestCountInstancesKeyFormat(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			if key != "dev/*/hosts/*/foo" {
				t.Errorf("CountInstances(%q) wrong key, want %s", key, "dev/web/hosts/*/foo")
			}
			return []string{}, nil
		},
	}

	r.CountInstances("foo")
}

func TestCountInstancesOne(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			if key != "dev/*/hosts/*/foo" {
				t.Errorf("CountInstances(%q) wrong key, want %s", key, "dev/web/hosts/*/foo")
			}
			return []string{"dev/web/hosts/me/foo"}, nil
		},
	}

	got := r.CountInstances("foo")
	if got != 1 {
		t.Errorf("CountInstances(%q) = %v, want %v", "foo", got, 1)
	}
}

func TestAssignAppNotExists(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = NewMemoryBackend()

	assigned, err := r.AssignApp("foo")
	if assigned {
		t.Errorf("AssignApp(%q) = %t, want %t", "foo", assigned, false)
	}

	if err != nil {
		t.Error(err)
	}
}

func TestAssignAppAddMemberFail(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			return []string{"a"}, nil
		},
		AddMemberFunc: func(key, value string) (int, error) {
			return 0, errors.New("something failed")
		},
	}

	if assigned, err := r.AssignApp("foo"); err == nil || assigned {
		t.Errorf("AssignApp(%q) = %t, %v, want %t, %v", "foo", assigned, err, false, nil)
	}
}

func TestAssignAppAddMemberNotifyRestart(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			return []string{"a"}, nil
		},
		AddMemberFunc: func(key, value string) (int, error) {
			return 1, nil
		},
		NotifyFunc: func(key, value string) (int, error) {
			if key != "galaxy-dev" {
				t.Errorf("AssignApp(%q) wrong notify key, want %s. got %s", "foo", key, "galaxy-dev")
			}

			if value != "restart foo" {
				t.Errorf("AssignApp(%q) wrong notify value, want %s. got %s", "foo", value, "restart foo")
			}
			return 1, nil
		},
	}

	if assigned, err := r.AssignApp("foo"); !assigned || err != nil {
		t.Errorf("AssignApp(%q) = %t, %v, want %t, %v", "foo", assigned, err, true, nil)
	}
}

func TestUnassignAppNotExists(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = NewMemoryBackend()

	if unassigned, err := r.UnassignApp("foo"); unassigned || err != nil {
		t.Errorf("UnAssignApp(%q) = %t, %v, want %t, %v", "foo", unassigned, err, false, nil)
	}
}

func TestUnassignAppRemoveMemberFail(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		RemoveMemberFunc: func(key, value string) (int, error) {
			return 0, errors.New("something failed")
		},
	}

	if unassigned, err := r.UnassignApp("foo"); unassigned || err == nil {
		t.Errorf("UnAssignApp(%q) = %t, %v, want %t, %v", "foo", unassigned, err, false, nil)
	}
}

func TestUnassignAppAddMemberNotifyRestart(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			return []string{"a"}, nil
		},
		RemoveMemberFunc: func(key, value string) (int, error) {
			return 1, nil
		},
		NotifyFunc: func(key, value string) (int, error) {
			if key != "galaxy-dev" {
				t.Errorf("UnassignApp(%q) wrong notify key, want %s. got %s", "foo", key, "galaxy-dev")
			}

			if value != "restart foo" {
				t.Errorf("UnassignApp(%q) wrong notify value, want %s. got %s", "foo", value, "restart foo")
			}
			return 1, nil
		},
	}

	if unassigned, err := r.UnassignApp("foo"); !unassigned || err != nil {
		t.Errorf("UnAssignApp(%q) = %t, %v, want %t, %v", "foo", unassigned, err, true, nil)
	}
}

func TestCreatePool(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = NewMemoryBackend()

	if created, err := r.CreatePool("foo"); !created || err != nil {
		t.Errorf("CreatePool(%q) = %tm %v, want %t, %v", "foo", created, err, true, nil)
	}
}

func TestDeletePool(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	r.backend = NewMemoryBackend()
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
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	r.backend = NewMemoryBackend()
	if created, err := r.CreateApp("app"); !created || err != nil {
		t.Errorf("CreateApp() = %t, %v, want %t, %v", created, err, true, nil)
	}

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
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	r.backend = NewMemoryBackend()

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
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = NewMemoryBackend()

	if created, err := r.CreateApp("foo"); !created || err != nil {
		t.Fatalf("CreateApp() = %t, %v, want %t, %v", created, err, true, nil)
	}
}

func TestCreateAppAlreadyExists(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	r.backend = NewMemoryBackend()

	if created, err := r.CreateApp("foo"); !created || err != nil {
		t.Fatalf("CreateApp() = %t, %v, want %t, %v", created, err, true, nil)
	}

	if created, err := r.CreateApp("foo"); created || err != nil {
		t.Fatalf("CreateApp() = %t, %v, want %t, %v",
			created, err,
			false, nil)
	}
}

func TestCreateAppError(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			return []string{}, errors.New("something failed")
		},
	}

	if created, err := r.CreateApp("foo"); created || err == nil {
		t.Fatalf("CreateApp() = %t, %v, want %t, %v",
			created, err,
			err, errors.New("something failed"))
	}
}
