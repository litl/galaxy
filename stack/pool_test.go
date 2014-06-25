package stack

import "testing"

// WARNING: these only test code cover for now.
// Run with `go test -v` and manually inspect the output
func TestPoolTemplate(t *testing.T) {
	p := Pool{
		Name:              "super",
		Env:               "test",
		DesiredCapacity:   17,
		MinSize:           1,
		MaxSize:           1435677,
		KeyName:           "admin-key",
		InstanceType:      "t1.testy",
		ImageID:           "ami-awesome",
		SubnetIDs:         []string{"sn-1", "sn-2", "sn-3"},
		SecurityGroups:    []string{"sg-1", "sg-2"},
		ELB:               true,
		ELBHealthCheck:    "HTTP:765/health",
		ELBSecurityGroups: []string{"sg-2", "sg-3"},
	}

	tmpl, err := CreatePoolTemplate(p)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(string(tmpl))
}
