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

	exists, err := r.AppExists("foo")
	if err != nil {
		t.Error(err)
	}

	if exists {
		t.Errorf("AppExists(%q) = %t, want %t", "foo", exists, false)
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

	exists, err := r.AppExists("foo")
	if err != nil {
		t.Error(err)
	}

	if !exists {
		t.Errorf("AppExists(%q) = %t, want %t", "foo", exists, true)
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

	assigned, err := r.AssignApp("foo")
	if assigned {
		t.Errorf("AssignApp(%q) = %t, want %t", "foo", assigned, false)
	}

	if err == nil {
		t.Errorf("AssignApp(%q) = %v, want %v", "foo", err, errors.New("something failed"))
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

	assigned, err := r.AssignApp("foo")
	if !assigned {
		t.Errorf("AssignApp(%q) = %t, want %t", "foo", assigned, true)
	}

	if err != nil {
		t.Errorf("AssignApp(%q) = %v, want %v", "foo", err, nil)
	}
}

func TestUnassignAppNotExists(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = NewMemoryBackend()

	unassigned, err := r.UnassignApp("foo")
	if unassigned {
		t.Errorf("UnAssignApp(%q) = %t, want %t", "foo", unassigned, false)
	}

	if err != nil {
		t.Error(err)
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

	unassigned, err := r.UnassignApp("foo")
	if unassigned {
		t.Errorf("UnassignApp(%q) = %t, want %t", "foo", unassigned, false)
	}

	if err == nil {
		t.Errorf("UnssignApp(%q) = %v, want %v", "foo", err, errors.New("something failed"))
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

	unassigned, err := r.UnassignApp("foo")
	if !unassigned {
		t.Errorf("UnassignApp(%q) = %t, want %t", "foo", unassigned, true)
	}

	if err != nil {
		t.Errorf("UnassignApp(%q) = %v, want %v", "foo", err, nil)
	}
}

func TestCreatePool(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = NewMemoryBackend()

	created, err := r.CreatePool("foo")
	if !created {
		t.Errorf("CreatePool(%q) = %t, want %t", "foo", created, true)
	}

	if err != nil {
		t.Errorf("CreatePool(%q) = %v, want %v", "foo", err, nil)
	}
}

func TestDeletePool(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	r.backend = NewMemoryBackend()
	created, err := r.CreatePool("foo")
	if err != nil {
		t.Errorf("CreatePool() = %v, want %v", err, nil)
	}

	if !created {
		t.Errorf("CreatePool()) = %t, want %t", created, true)
	}

	r.Pool = "foo"
	if exists, err := r.PoolExists(); !exists || err != nil {
		t.Errorf("PoolExists()) = %t, %v, want %t, %v", exists, err, true, nil)
	}

	deleted, err := r.DeletePool("foo")
	if !deleted {
		t.Errorf("DeletePool(%q) = %t, want %t", "foo", deleted, true)
	}

	if err != nil {
		t.Errorf("DeletePool(%q) = %v, want %v", "foo", err, nil)
	}
}

func TestDeletePoolHasAssignments(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	r.backend = NewMemoryBackend()
	created, err := r.CreateApp("app")
	if err != nil {
		t.Errorf("CreateApp() = %v, want %v", err, nil)
	}

	if !created {
		t.Errorf("CreateApp()) = %t, want %t", created, true)
	}

	created, err = r.CreatePool("foo")
	if err != nil {
		t.Errorf("CreatePool() = %v, want %v", err, nil)
	}

	if !created {
		t.Errorf("CreatePool()) = %t, want %t", created, true)
	}

	// This is weird. AssignApp should probably take app & pool as params.
	r.Pool = "foo"
	assigned, err := r.AssignApp("app")
	if err != nil {
		t.Errorf("AssignApp() = %v, want %v", err, nil)
	}

	if !assigned {
		t.Errorf("AssignApp()) = %t, want %t", assigned, true)
	}

	deleted, err := r.DeletePool("foo")
	if deleted {
		t.Errorf("DeletePool(%q) = %t, want %t", "foo", deleted, false)
	}

	if err != nil {
		t.Errorf("DeletePool(%q) = %v, want %v", "foo", err, nil)
	}
}

func TestListPools(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	r.backend = NewMemoryBackend()

	for _, pool := range []string{"one", "two"} {
		created, err := r.CreatePool(pool)
		if err != nil {
			t.Errorf("CreatePool() = %v, want %v", err, nil)
		}

		if !created {
			t.Errorf("CreatePool() = %t, want %t", created, true)
		}
	}

	pools, err := r.ListPools()
	if len(pools) == 0 {
		t.Errorf("ListPools() = %d, want %d", len(pools), 2)
	}

	if err != nil {
		t.Errorf("ListPools() = %v, want %v", err, nil)
	}
}

func TestCreateApp(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = NewMemoryBackend()

	created, err := r.CreateApp("foo")
	if err != nil {
		t.Fatalf("CreateApp() = %v, want %v", err, nil)
	}

	if !created {
		t.Fatalf("CreateApp() = %t, want %t", created, true)
	}
}

func TestCreateAppAlreadyExists(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	r.backend = NewMemoryBackend()

	created, err := r.CreateApp("foo")
	if err != nil {
		t.Fatalf("CreateApp() = %v, want %v", err, nil)
	}

	if !created {
		t.Fatalf("CreateApp() = %t, want %t", created, true)
	}

	created, err = r.CreateApp("foo")
	if err != nil {
		t.Fatalf("CreateApp() = %v, want %v", err, nil)
	}

	if created {
		t.Fatalf("CreateApp() = %t, want %t", created, false)
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

	created, err := r.CreateApp("foo")
	if err == nil {
		t.Fatalf("CreateApp() = %v, want %v", err, errors.New("something failed"))
	}

	if created {
		t.Fatalf("CreateApp() = %t, want %t", created, false)
	}
}
