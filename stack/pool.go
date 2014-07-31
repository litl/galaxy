package stack

import (
	"encoding/json"
	"fmt"
)

const (
	asgType = "AWS::AutoScaling::AutoScalingGroup"
	elbType = "AWS::ElasticLoadBalancing::LoadBalancer"
	lcType  = "AWS::AutoScaling::LaunchConfiguration"
)

// TODO: add more public functions to conveniently build out a pool

// A Pool can be marshaled directly into a Cloudformation template for our pools.
// This is Purposely constrained to our usage, with some values specifically
// using intrinsic functions, and other assumed prerequisites. This should only
// matter if the poolTmpl is modified, or we attempt to update an arbitrarily
// added pool template.
type Pool struct {
	// pre-initialized values from the template
	ASGTemplate *asg `json:"-"`
	ELBTemplate *elb `json:"-"`
	LCTemplate  *lc  `json:"-"`

	AWSTemplateFormatVersion string
	Description              string
	Resources                map[string]interface{}
}

func NewPool() *Pool {
	p := &Pool{}
	// use our pool template to initialize some defaults
	if err := json.Unmarshal(poolTmpl, p); err != nil {
		panic("corrupt pool template" + err.Error())
	}

	// move the template structs out of the final Resources
	for k, v := range p.Resources {
		switch r := v.(type) {
		case *elb:
			p.ELBTemplate = r
		case *asg:
			p.ASGTemplate = r
		case *lc:
			p.LCTemplate = r
		}
		delete(p.Resources, k)
	}

	return p
}

func (p *Pool) ASG() *asg {
	for _, i := range p.Resources {
		if r, ok := i.(*asg); ok {
			return r
		}
	}
	return nil
}

func (p *Pool) ELB() *elb {
	for _, i := range p.Resources {
		if r, ok := i.(*elb); ok {
			return r
		}
	}
	return nil
}

func (p *Pool) LC() *lc {
	for _, i := range p.Resources {
		if r, ok := i.(*lc); ok {
			return r
		}
	}
	return nil
}

func (p *Pool) UnmarshalJSON(b []byte) error {
	base := make(map[string]json.RawMessage)

	if err := json.Unmarshal(b, &base); err != nil {
		return err
	}

	if err := json.Unmarshal(base["AWSTemplateFormatVersion"], &p.AWSTemplateFormatVersion); err != nil {
		return err
	}

	if err := json.Unmarshal(base["Description"], &p.Description); err != nil {
		return err
	}

	rawResources, ok := base["Resources"]
	if !ok {
		return nil
	}

	// break out the resources by name
	tmpResources := make(map[string]json.RawMessage)
	if err := json.Unmarshal(rawResources, &tmpResources); err != nil {
		return err
	}

	p.Resources = make(map[string]interface{})

	for name, rawRes := range tmpResources {
		// now we need to get the Resource Type before we can unmarshal
		// into a struct
		tmpRes := make(map[string]json.RawMessage)
		if err := json.Unmarshal(rawRes, &tmpRes); err != nil {
			return err
		}

		resType := tmpRes["Type"]
		var t string
		if err := json.Unmarshal(resType, &t); err != nil {
			fmt.Println("ERROR GETTING TYPE")
			return err

		}
		switch t {
		case asgType:
			res := &asg{}
			p.Resources[name] = res
			if err := json.Unmarshal(rawRes, res); err != nil {
				fmt.Println("ERROR GETTING asg")
				return err
			}
		case elbType:
			res := &elb{}
			p.Resources[name] = res
			if err := json.Unmarshal(rawRes, res); err != nil {
				fmt.Println("ERROR GETTING elb")
				return err
			}
		case lcType:
			res := &lc{}
			p.Resources[name] = res
			if err := json.Unmarshal(rawRes, res); err != nil {
				fmt.Println("ERROR GETTING lc")
				return err
			}
		}
	}

	return nil
}

type asg struct {
	Type         string
	Properties   asgProp
	UpdatePolicy asgUpdatePolicy `json:",omitempty"`
}

func (a *asg) AddLoadBalancer(name string) {
	a.Properties.LoadBalancerNames = append(a.Properties.LoadBalancerNames, Intrinsic{"Ref": name})
}

type asgProp struct {
	AvailabilityZones       Intrinsic
	Cooldown                int `json:",string"`
	DesiredCapacity         int `json:",string"`
	HealthCheckGracePeriod  int `json:",string"`
	HealthCheckType         string
	LaunchConfigurationName Intrinsic
	LoadBalancerNames       []Intrinsic `json:",omitempty"`
	MaxSize                 int         `json:",string"`
	MinSize                 int         `json:",string"`
	Tags                    []Tag
	VPCZoneIdentifier       []string
}

type elb struct {
	Type       string
	Properties elbProp
}

type elbProp struct {
	Subnets        []string `json:",omitempty"`
	SecurityGroups []string `json:",omitempty"`
	HealthCheck    healthCheck
	Listeners      []*Listener
}

type healthCheck struct {
	HealthyThreshold   int `json:",string"`
	Interval           int `json:",string"`
	Target             string
	Timeout            int `json:",string"`
	UnhealthyThreshold int `json:",string"`
}

type Listener struct {
	InstancePort     int `json:",string"`
	InstanceProtocol string
	LoadBalancerPort int `json:",string"`
	Protocol         string
	PolicyNames      []string `json:",omitempty"`
	SSLCertificateId string   `json:",omitempty"`
}

type lc struct {
	Type       string
	Properties lcProp
}

type lcProp struct {
	AssociatePublicIpAddress bool
	BlockDeviceMappings      []bdMapping
	EbsOptimized             bool
	IamInstanceProfile       string `json:",omitempty"`
	ImageId                  string `json:",omitempty"`
	InstanceId               string `json:",omitempty"`
	InstanceMonitoring       *bool  `json:",omitempty"`
	InstanceType             string
	KernelId                 string   `json:",omitempty"`
	KeyName                  string   `json:",omitempty"`
	RamDiskId                string   `json:",omitempty"`
	SecurityGroups           []string `json:",omitempty"`
	SpotPrice                string   `json:",omitempty"`
	UserData                 string   `json:",omitempty"`
}

type bdMapping struct {
	DeviceName  string
	Ebs         *ebsDev `json:",omitempty"` // OR VirtualName
	VirtualName *string `json:",omitempty"` // OR Ebs
}

type ebsDev struct {
	DeleteOnTermination *bool  `json:",omitempty"`
	Iops                *int   `json:",omitempty"`
	SnapshotId          string `json:",omitempty"`
	VolumeSize          int
	VolumeType          string
}

type asgUpdatePolicy struct {
	AutoScalingRollingUpdate asgUpdate
}

type asgUpdate struct {
	MinInstancesInService string
	MaxBatchSize          string
	PauseTime             string
}

type Tag struct {
	Key               string
	Value             string
	PropagateAtLaunch *bool `json:",omitempty"`
}

// use this to indicate we're specifically using an intrinsic function over an
// actual map of values
type Intrinsic map[string]string
