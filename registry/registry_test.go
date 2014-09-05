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
	return f.MembersFunc(key)
}

func (f *fakeBackend) Keys(key string) ([]string, error) {
	return f.KeysFunc(key)
}

func (f *fakeBackend) AddMember(key, value string) (int, error) {
	return f.AddMemberFunc(key, value)
}

func (f *fakeBackend) RemoveMember(key, value string) (int, error) {
	return f.RemoveMemberFunc(key, value)
}

func (f *fakeBackend) Notify(key, value string) (int, error) {
	return f.NotifyFunc(key, value)
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
	panic("not implemented")
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
	r.backend = &fakeBackend{
		MembersFunc: func(key string) ([]string, error) {
			return []string{}, nil
		},
	}

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
	r.backend = &fakeBackend{
		MembersFunc: func(key string) ([]string, error) {
			return []string{"one", "two"}, nil
		},
	}

	assignments, err := r.ListAssignments("foo")
	if err != nil {
		t.Error(err)
	}

	if len(assignments) != 2 {
		t.Errorf("ListAssignments(%q) = %d, want %d", "foo", len(assignments), 0)
	}

	if assignments[0] != "one" {
		t.Errorf("assignments[0] = %d, want %d", assignments[0], "one")
	}

	if assignments[1] != "two" {
		t.Errorf("assignments[1] = %d, want %d", assignments[0], "two")
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
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			return []string{}, nil
		},
	}

	exists, err := r.AppExists("foo")
	if err != nil {
		t.Error(err)
	}

	if exists {
		t.Errorf("AppExists(%q) = %t, want %t", exists, false)
	}
}

func TestAppExists(t *testing.T) {
	r := &ServiceRegistry{
		Env: "dev",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			return []string{"/dev/foo/environment"}, nil
		},
	}

	exists, err := r.AppExists("foo")
	if err != nil {
		t.Error(err)
	}

	if !exists {
		t.Errorf("AppExists(%q) = %t, want %t", exists, true)
	}
}

func TestCountInstancesKeyFormat(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			if key != "dev/web/hosts/*/foo" {
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
			if key != "dev/web/hosts/*/foo" {
				t.Errorf("CountInstances(%q) wrong key, want %s", key, "dev/web/hosts/*/foo")
			}
			return []string{"dev/web/hosts/me/foo"}, nil
		},
	}

	got := r.CountInstances("foo")
	if got != 1 {
		t.Errorf("CountInstances(%q) = %t, want %t", "foo", got, 1)
	}
}

func TestAssignAppNotExists(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		KeysFunc: func(key string) ([]string, error) {
			return []string{}, nil
		},
	}

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
		t.Errorf("AssignApp(%q) = %t, want %t", "foo", err, errors.New("something failed"))
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
		t.Errorf("AssignApp(%q) = %t, want %t", "foo", err, nil)
	}
}

func TestUnassignAppNotExists(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		RemoveMemberFunc: func(key, value string) (int, error) {
			return 0, nil
		},
	}

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
		t.Errorf("UnssignApp(%q) = %t, want %t", "foo", err, errors.New("something failed"))
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
		t.Errorf("UnassignApp(%q) = %t, want %t", "foo", err, nil)
	}
}

func TestCreatePool(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		AddMemberFunc: func(key, value string) (int, error) {
			if key != "dev/pools/*" {
				t.Errorf("CreatePool(%q) wrong key, want %s. got %s", "foo", key, "dev/pools/*")
			}
			if value != "foo" {
				t.Errorf("CreatePool(%q) wrong value, want %s. got %s", "foo", key, "foo")
			}

			return 1, nil
		},
	}

	created, err := r.CreatePool("foo")
	if !created {
		t.Errorf("CreatePool(%q) = %t, want %t", "foo", created, true)
	}

	if err != nil {
		t.Errorf("CreatePool(%q) = %t, want %t", "foo", err, nil)
	}
}

func TestDeletePool(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		MembersFunc: func(key string) ([]string, error) {
			return []string{}, nil
		},

		RemoveMemberFunc: func(key, value string) (int, error) {
			if key != "dev/pools/*" {
				t.Errorf("DeletePool(%q) wrong key, want %s. got %s", "foo", key, "dev/pools/*")
			}
			if value != "foo" {
				t.Errorf("DeletePool(%q) wrong value, want %s. got %s", "foo", key, "foo")
			}

			return 1, nil
		},
	}

	deleted, err := r.DeletePool("foo")
	if !deleted {
		t.Errorf("DeletePool(%q) = %t, want %t", "foo", deleted, true)
	}

	if err != nil {
		t.Errorf("DeletePool(%q) = %t, want %t", "foo", err, nil)
	}
}

func TestDeletePoolHasAssignments(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}
	r.backend = &fakeBackend{
		MembersFunc: func(key string) ([]string, error) {
			return []string{"one", "two"}, nil
		},

		RemoveMemberFunc: func(key, value string) (int, error) {
			if key != "dev/pools/*" {
				t.Errorf("DeletePool(%q) wrong key, want %s. got %s", "foo", key, "dev/pools/*")
			}
			if value != "foo" {
				t.Errorf("DeletePool(%q) wrong value, want %s. got %s", "foo", key, "foo")
			}

			return 1, nil
		},
	}

	deleted, err := r.DeletePool("foo")
	if deleted {
		t.Errorf("DeletePool(%q) = %t, want %t", "foo", deleted, false)
	}

	if err != nil {
		t.Errorf("DeletePool(%q) = %t, want %t", "foo", err, nil)
	}
}

func TestListPools(t *testing.T) {
	r := &ServiceRegistry{
		Env:  "dev",
		Pool: "web",
	}

	// This is a table of Member func calls that are expected to be
	// invoked in the order that they are listed
	callCnt := 0
	memberFuncs := []func(key string) ([]string, error){
		func(key string) ([]string, error) {
			if key != "dev/pools/*" {
				t.Errorf("ListPools() wrong key, want %s. got %s", key, "dev/pools/*")
			}
			return []string{"one", "two"}, nil
		},
		func(key string) ([]string, error) {
			if key != "dev/pools/one" {
				t.Errorf("ListPools() wrong key, want %s. got %s", key, "dev/pools/one")
			}
			return []string{"one"}, nil

		},
		func(key string) ([]string, error) {
			if key != "dev/pools/two" {
				t.Errorf("ListPools() wrong key, want %s. got %s", key, "dev/pools/two")
			}
			return []string{"two"}, nil
		},
	}

	// Calls the func pointed to by callCnt index, then increments it
	memberFunc := func(key string) ([]string, error) {
		if callCnt > len(memberFuncs) {
			t.Errorf("ListPools() too many calls to Members. got %s", callCnt, len(memberFuncs))
		}
		defer func() {
			callCnt = callCnt + 1
		}()
		return memberFuncs[callCnt](key)
	}

	r.backend = &fakeBackend{
		MembersFunc: memberFunc,
	}

	pools, err := r.ListPools()
	if len(pools) == 0 {
		t.Errorf("ListPools() = %d, want %d", len(pools), 2)
	}

	if err != nil {
		t.Errorf("ListPools() = %t, want %t", err, nil)
	}
}
