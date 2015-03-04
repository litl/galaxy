package commander

import (
	"testing"

	"github.com/litl/galaxy/config"
)

func NewTestStore() (*config.Store, *config.MemoryBackend) {
	r := &config.Store{}
	b := config.NewMemoryBackend()
	r.Backend = b
	return r, b
}

func setup(t *testing.T, desired int, hosts []string) *config.Store {
	s, b := NewTestStore()

	created, err := s.CreateApp("app", "dev")
	if !created || err != nil {
		t.Errorf("Failed to create app: %s", err)
	}

	ac, err := s.GetApp("app", "dev")
	if !created || err != nil {
		t.Errorf("Failed to get app: %s", err)
	}

	ac.SetProcesses("web", desired)

	b.ListHostsFunc = func(env, pool string) ([]config.HostInfo, error) {
		ret := []config.HostInfo{}
		for _, h := range hosts {
			ret = append(ret, config.HostInfo{
				HostIP: h,
			})
		}
		return ret, nil

	}
	return s
}

func TestScheduleOneBadHost(t *testing.T) {

	s := setup(t, 1, []string{"127.0.0.1"})

	count, err := Balanced(s, "127.0.0.2", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 0, err)
	}

	if count != 0 {
		t.Errorf("Expected %d. Got %d", 0, count)
	}
}

func TestScheduleOneOneHost(t *testing.T) {

	s := setup(t, 1, []string{"127.0.0.1"})

	count, err := Balanced(s, "127.0.0.1", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 1, err)
	}

	if count != 1 {
		t.Errorf("Expected %d. Got %d", 1, count)
	}
}

func TestScheduleFiveOneHost(t *testing.T) {

	s := setup(t, 5, []string{"127.0.0.1"})

	count, err := Balanced(s, "127.0.0.1", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 5, err)
	}

	if count != 5 {
		t.Errorf("Expected %d. Got %d", 5, count)
	}
}

func TestScheduleTwoOneHost(t *testing.T) {

	s := setup(t, 2, []string{"127.0.0.1"})

	count, err := Balanced(s, "127.0.0.1", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 2, err)
	}

	if count != 2 {
		t.Errorf("Expected %d. Got %d", 2, count)
	}
}

func TestScheduleOneTwoHost(t *testing.T) {

	s := setup(t, 1, []string{"127.0.0.1", "127.0.0.2"})

	count, err := Balanced(s, "127.0.0.1", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 1, err)
	}

	if count != 1 {
		t.Errorf("Expected %d. Got %d", 1, count)
	}

	count, err = Balanced(s, "127.0.0.2", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 0, err)
	}

	if count != 0 {
		t.Errorf("Expected %d. Got %d", 0, count)
	}
}

func TestScheduleTwoTwoHost(t *testing.T) {

	s := setup(t, 2, []string{"127.0.0.1", "127.0.0.2"})

	count, err := Balanced(s, "127.0.0.1", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 1, err)
	}

	if count != 1 {
		t.Errorf("Expected %d. Got %d", 1, count)
	}

	count, err = Balanced(s, "127.0.0.2", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 1, err)
	}

	if count != 1 {
		t.Errorf("Expected %d. Got %d", 1, count)
	}
}

func TestScheduleFiveTwoHost(t *testing.T) {

	s := setup(t, 5, []string{"127.0.0.1", "127.0.0.2"})

	count, err := Balanced(s, "127.0.0.1", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 1, err)
	}

	if count != 3 {
		t.Errorf("Expected %d. Got %d", 3, count)
	}

	count, err = Balanced(s, "127.0.0.2", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 2, err)
	}

	if count != 2 {
		t.Errorf("Expected %d. Got %d", 2, count)
	}
}

func TestScheduleOneDefault(t *testing.T) {

	s := setup(t, -1, []string{"127.0.0.1"})

	count, err := Balanced(s, "127.0.0.1", "app", "dev", "web")
	if err != nil {
		t.Errorf("Expected %d. Got %s", 1, err)
	}

	if count != 1 {
		t.Errorf("Expected %d. Got %d", 1, count)
	}
}
