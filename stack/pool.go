package stack

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
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
	// The *Template attributes hold the pre-initialed types from the pool
	// template.  These are not serialized to json, and should be used to
	// create the proper Resources.
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
			return err

		}
		switch t {
		case asgType:
			res := &asg{}
			p.Resources[name] = res
			if err := json.Unmarshal(rawRes, res); err != nil {
				return err
			}
		case elbType:
			res := &elb{}
			p.Resources[name] = res
			if err := json.Unmarshal(rawRes, res); err != nil {
				return err
			}
		case lcType:
			res := &lc{}
			p.Resources[name] = res
			if err := json.Unmarshal(rawRes, res); err != nil {
				return err
			}
		}
	}

	return nil
}

type asg struct {
	Type         string
	Properties   asgProp
	UpdatePolicy *asgUpdatePolicy `json:",omitempty"`
}

func (a *asg) AddLoadBalancer(name string) {
	a.Properties.LoadBalancerNames = append(a.Properties.LoadBalancerNames, intrinsic{"Ref": name})
}

func (a *asg) SetLaunchConfiguration(name string) {
	a.Properties.LaunchConfigurationName = intrinsic{"Ref": name}
}

func (a *asg) AddTag(key, value string, propagateAtLauch bool) {
	t := tag{
		Key:               key,
		Value:             value,
		PropagateAtLaunch: &propagateAtLauch,
	}

	a.Properties.Tags = append(a.Properties.Tags, t)
}

type asgProp struct {
	AvailabilityZones       intrinsic
	Cooldown                int `json:",string"`
	DesiredCapacity         int `json:",string"`
	HealthCheckGracePeriod  int `json:",string"`
	HealthCheckType         string
	LaunchConfigurationName intrinsic
	LoadBalancerNames       []intrinsic `json:",omitempty"`
	MaxSize                 int         `json:",string"`
	MinSize                 int         `json:",string"`
	Tags                    []tag
	VPCZoneIdentifier       []string
}

type asgUpdatePolicy struct {
	AutoScalingRollingUpdate asgUpdate
}

type asgUpdate struct {
	MinInstancesInService string
	MaxBatchSize          string
	PauseTime             string
}

// Generate an ASGUpdatePolicy
func (a *asg) SetASGUpdatePolicy(min, batch int, pause time.Duration) {
	a.UpdatePolicy = &asgUpdatePolicy{
		AutoScalingRollingUpdate: asgUpdate{
			MinInstancesInService: strconv.Itoa(min),
			MaxBatchSize:          strconv.Itoa(batch),
			// XML duration -- close enough for now.
			PauseTime: "PT" + strings.ToUpper(pause.String()),
		},
	}
}

type elb struct {
	Type       string
	Properties elbProp
}

// Add a listener, replacing an existing listener with the same port
func (e *elb) AddListener(port int, proto string, instancePort int, instanceProto string, sslCert string, policyNames []string) {
	// take our current listeners
	current := []listener{}

	for _, l := range e.Properties.Listeners {
		// skip this one if the port matches what we're setting
		if l.LoadBalancerPort != port {
			current = append(current, l)
		}
	}

	proto = strings.ToUpper(proto)
	instanceProto = strings.ToUpper(instanceProto)

	l := listener{
		InstancePort:     instancePort,
		InstanceProtocol: instanceProto,
		LoadBalancerPort: port,
		Protocol:         proto,
		PolicyNames:      policyNames,
		SSLCertificateId: sslCert,
	}

	e.Properties.Listeners = append(current, l)
}

type elbProp struct {
	Subnets        []string `json:",omitempty"`
	SecurityGroups []string `json:",omitempty"`
	HealthCheck    healthCheck
	Listeners      []listener
}

type healthCheck struct {
	HealthyThreshold   int `json:",string"`
	Interval           int `json:",string"`
	Target             string
	Timeout            int `json:",string"`
	UnhealthyThreshold int `json:",string"`
}

type listener struct {
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

func (c *lc) SetVolumeSize(size int) {
	if len(c.Properties.BlockDeviceMappings) == 0 {
		return
	}
	if c.Properties.BlockDeviceMappings[0].Ebs == nil {
		return
	}
	c.Properties.BlockDeviceMappings[0].Ebs.VolumeSize = size
}

type lcProp struct {
	AssociatePublicIpAddress bool
	BlockDeviceMappings      []bdMapping `json:",omitempty"`
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

type tag struct {
	Key               string
	Value             string
	PropagateAtLaunch *bool `json:",omitempty"`
}

// use this to indicate we're specifically using an intrinsic function over an
// actual map of values
type intrinsic map[string]string
