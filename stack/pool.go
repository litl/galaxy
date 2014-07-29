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

type NewPool struct {
	AWSTemplateFormatVersion string
	Description              string
	Resources                map[string]interface{}
}

func (p *NewPool) UnmarshalJSON(b []byte) error {
	p.AWSTemplateFormatVersion = "2010-09-09"
	p.Description = "Galaxy Pool Template"

	base := make(map[string]json.RawMessage)
	if err := json.Unmarshal(b, &base); err != nil {
		return err
	}

	rawResources, ok := base["Resources"]
	if !ok {
		return nil
	}

	// beak out the resources by name
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
	name         string
	Type         string
	Properties   asgProp
	UpdatePolicy asgUpdatePolicy
}

type asgProp struct {
	AvailabilityZones intrinsic
	Cooldown          int `json:",string"`
	DesiredCapacity   int `json:",string"`

	HealthCheckGracePeriod  int `json:",string"`
	HealthCheckType         string
	LaunchConfigurationName intrinsic
	LoadBalancerNames       []intrinsic
	MaxSize                 int `json:",string"`
	MinSize                 int `json:",string"`
	Tags                    []tag
	VPCZoneIdentifier       []string
}

type elb struct {
	name       string
	Type       string
	Properties elbProp
}

type elbProp struct {
	Subnets        []string
	SecurityGroups []string
	HealthCheck    healthCheck
	Listeners      []listener
}

type healthCheck struct {
	HealthyThreshold int `json:",string"`

	Interval int `json:",string"`

	Target             string
	Timeout            int `json:",string"`
	UnhealthyThreshold int `json:",string"`
}

type listener struct {
	InstancePort int `json:",string"`

	InstanceProtocol string
	LoadBalancerPort int `json:",string"`
	Protocol         string
	SSLCertificateId *string `json:",omitempty"`
}

type lc struct {
	name       string
	Type       string
	Properties lcProp
}

type lcProp struct {
	AssociatePublicIpAddress bool
	BlockDeviceMappings      []bdMapping
	EbsOptimized             bool
	IamInstanceProfile       *string `json:",omitempty"`
	ImageId                  *string `json:",omitempty"`
	InstanceId               *string `json:",omitempty"`
	InstanceMonitoring       *bool   `json:",omitempty"`
	InstanceType             string
	KernelId                 *string  `json:",omitempty"`
	KeyName                  *string  `json:",omitempty"`
	RamDiskId                *string  `json:",omitempty"`
	SecurityGroups           []string `json:",omitempty"`
	SpotPrice                *string  `json:",omitempty"`
	UserData                 *string  `json:",omitempty"`
}

type bdMapping struct {
	DeviceName  string
	Ebs         *ebsDev `json:",omitempty"` // OR VirtualName
	VirtualName *string `json:",omitempty"` // OR Ebs
}

type ebsDev struct {
	DeleteOnTermination *bool   `json:",omitempty"`
	Iops                *int    `json:",omitempty"`
	SnapshotId          *string `json:",omitempty"`
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

type tag struct {
	Key               string
	Value             string
	PropagateAtLaunch *bool `json:",omitempty"`
}

// use this to indicate we're specifically using an intrinsic function over an
// actual map of values
type intrinsic map[string]string
