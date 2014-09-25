package stack

import (
	"encoding/json"
	"fmt"
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
	for name, i := range p.Resources {
		if r, ok := i.(*asg); ok {
			r.Name = name
			return r
		}
	}
	return nil
}

func (p *Pool) ELB() *elb {
	for name, i := range p.Resources {
		if r, ok := i.(*elb); ok {
			r.Name = name
			return r
		}
	}
	return nil
}

func (p *Pool) LC() *lc {
	for name, i := range p.Resources {
		if r, ok := i.(*lc); ok {
			r.Name = name
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
	Name         string `json:"-"`
	Type         string
	Properties   asgProp
	UpdatePolicy *asgUpdatePolicy `json:",omitempty"`
}

func (a *asg) AddLoadBalancer(name string) {
	a.Properties.LoadBalancerNames = append(a.Properties.LoadBalancerNames, ref{name})
}

func (a *asg) SetLaunchConfiguration(name string) {
	a.Properties.LaunchConfigurationName = ref{name}
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
	AvailabilityZones       fnGetAZs
	Cooldown                int `json:",string"`
	DesiredCapacity         int `json:",string"`
	HealthCheckGracePeriod  int `json:",string"`
	HealthCheckType         string
	LaunchConfigurationName ref
	LoadBalancerNames       []ref `json:",omitempty"`
	MaxSize                 int   `json:",string"`
	MinSize                 int   `json:",string"`
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
	Name       string `json:"-"`
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
	CrossZone      bool
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
	Name       string `json:"-"`
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

// Ref intrinsic
type ref struct {
	Ref string
}

// getAZs intrinsic
type fnGetAZs struct {
	FnGetAZs string `json:"Fn::GetAZs"`
}

type scalingPolicy struct {
	Name       string `json:"-"`
	Type       string
	Properties spProp
}

type spProp struct {
	AdjustmentType       string
	AutoScalingGroupName *ref
	Cooldown             int `json:",string"`
	ScalingAdjustment    int `json:",string"`
}

type cloudWatchAlarm struct {
	Name       string `json:"-"`
	Type       string
	Properties cwaProp
}

type cwaProp struct {
	ActionsEnabled          bool `json:",string"`
	AlarmActions            []ref
	AlarmDescription        string `json:",omitempty"`
	AlarmName               string `json:",omitempty"`
	ComparisonOperator      string
	Dimensions              []metricDim
	EvaluationPeriods       int      `json:",string"`
	InsufficientDataActions []string `json:",omitempty"`
	MetricName              string
	Namespace               string
	OKActions               []string `json:",omitempty"`
	Period                  int      `json:",string"`
	Statistic               string
	Threshold               string
	Unit                    string `json:",omitempty"`
}

type metricDim struct {
	Name  string
	Value ref
}

// Add the appropriate Alarms and ScalingPolicies to autoscale a pool based on avg CPU usage
func (p *Pool) SetCPUAutoScaling(asgName string, adj, scaleUpCPU, scaleUpDelay, scaleDownCPU, scaleDownDelay int) {
	scaling := struct {
		ScaleUp        *scalingPolicy
		ScaleDown      *scalingPolicy
		ScaleUpAlarm   *cloudWatchAlarm
		ScaleDownAlarm *cloudWatchAlarm
	}{}

	// check for existing scaling resources
	for name, res := range p.Resources {
		switch name {
		case "ScaleUp":
			scaling.ScaleUp = res.(*scalingPolicy)
		case "ScaleUpAlarm":
			scaling.ScaleUpAlarm = res.(*cloudWatchAlarm)
		case "ScaleDown":
			scaling.ScaleDown = res.(*scalingPolicy)
		case "ScaleDownAlarm":
			scaling.ScaleDownAlarm = res.(*cloudWatchAlarm)
		}
	}

	// load the template defaults if we don't have any defined
	if scaling.ScaleUp == nil || scaling.ScaleDown == nil {
		if err := json.Unmarshal(scalingTemplate, &scaling); err != nil {
			panic("corrupt scaling template" + err.Error())
		}
	}

	scaling.ScaleUp.Properties.AutoScalingGroupName.Ref = asgName
	scaling.ScaleDown.Properties.AutoScalingGroupName.Ref = asgName

	if adj > 0 {
		scaling.ScaleUp.Properties.ScalingAdjustment = adj
		scaling.ScaleDown.Properties.ScalingAdjustment = -adj
	}

	scaling.ScaleUpAlarm.Properties.Dimensions[0].Value.Ref = asgName
	scaling.ScaleDownAlarm.Properties.Dimensions[0].Value.Ref = asgName

	if scaleUpDelay > 0 {
		scaling.ScaleUpAlarm.Properties.EvaluationPeriods = scaleUpDelay
	}
	if scaleDownDelay > 0 {
		scaling.ScaleDownAlarm.Properties.EvaluationPeriods = scaleDownDelay
	}

	if scaleUpCPU > 0 {
		scaling.ScaleUpAlarm.Properties.Threshold = fmt.Sprintf("%d.0", scaleUpCPU)
	}
	if scaleDownCPU > 0 {
		scaling.ScaleDownAlarm.Properties.Threshold = fmt.Sprintf("%d.0", scaleDownCPU)
	}

	p.Resources["ScaleUp"] = scaling.ScaleUp
	p.Resources["ScaleDown"] = scaling.ScaleDown
	p.Resources["ScaleUpAlarm"] = scaling.ScaleUpAlarm
	p.Resources["ScaleDownAlarm"] = scaling.ScaleDownAlarm
}
