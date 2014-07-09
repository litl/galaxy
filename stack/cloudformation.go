package stack

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/bitly/go-simplejson"
	"github.com/crowdmob/goamz/aws"
)

type GetTemplateResponse struct {
	TemplateBody []byte `xml:"GetTemplateResult>TemplateBody"`
}

type CreateStackResponse struct {
	RequestId string `xml:"ResponseMetadata>RequestId"`
	StackId   string `xml:"CreateStackResult>StackId"`
}

type UpdateStackResponse struct {
	RequestId string `xml:"ResponseMetadata>RequestId"`
	StackId   string `xml:"UpdateStackResult>StackId"`
}

type DeleteStackResponse struct {
	RequestId string `xml:"ResponseMetadata>RequestId"`
}

type stackParameter struct {
	Key   string `xml:"ParameterKey"`
	Value string `xml:"ParameterValue"`
}

type stackDescription struct {
	Id           string           `xml:"StackId"`
	Name         string           `xml:"StackName"`
	Status       string           `xml:"StackStatus"`
	StatusReason string           `xml:"StackStatusReason"`
	Parameters   []stackParameter `xml:"Parameters>member"`
}

type DescribeStacksResponse struct {
	RequestId string             `xml:"ResponseMetadata>RequestId"`
	Stacks    []stackDescription `xml:"DescribeStacksResult>Stacks>member"`
}

type stackResource struct {
	Status     string `xml:"ResourceStatus"`
	LogicalId  string `xml:"LogicalResourceId"`
	PhysicalId string `xml:"PhysicalResourceId"`
	Type       string `xml:"ResourceType"`
}

type ListStackResourcesResponse struct {
	RequestId string          `xml:"ResponseMetadata>RequestId"`
	Resources []stackResource `xml:"ListStackResourcesResult>StackResourceSummaries>member"`
}

// Resources from the base stack that may need to be referenced from other
// stacks
type SharedResources struct {
	Subnets        map[string]string
	SecurityGroups map[string]string
	Roles          map[string]string
	Parameters     map[string]string
}

// Options needed to build a CloudFormation pool template.
// Each pool will have its own stack, that can quickly updated or removed.
type Pool struct {
	Name            string
	Env             string
	DesiredCapacity int
	MinSize         int
	MaxSize         int
	KeyName         string
	IAMRole         string
	InstanceType    string
	ImageID         string
	SubnetIDs       []string
	SecurityGroups  []string
	VolumeSize      int
	BaseStackName   string
	ELBs            []PoolELB
}

type PoolELB struct {
	Name           string
	Listeners      []PoolELBListener
	SecurityGroups []string
	HealthCheck    string
}

type PoolELBListener struct {
	LoadBalancerPort int `json:",string"`
	Protocol         string
	InstancePort     int `json:",string"`
	InstanceProtocol string
	PolicyNames      []string `json:",omitempty"`
	SSLCertificateId string   `json:",omitempty"`
}

// Create a CloudFormation template for a our pool stack
func CreatePoolTemplate(pool Pool) ([]byte, error) {
	// helper bits for creating pool templates
	type tag map[string]interface{}
	type ref struct{ Ref string }
	type blockDev struct {
		DeviceName string
		Ebs        map[string]int
	}

	// check for missing required fields
	switch "" {
	case pool.Name, pool.Env, pool.KeyName, pool.InstanceType, pool.ImageID:
		return nil, fmt.Errorf("incomplete pool definition")
	}

	if pool.BaseStackName == "" {
		pool.BaseStackName = "galaxy"
	}

	switch 0 {
	case len(pool.SubnetIDs), len(pool.SecurityGroups), pool.DesiredCapacity:
		return nil, fmt.Errorf("incomplete pool definition")
	}

	poolTmpl, err := simplejson.NewJson(pool_template)
	if err != nil {
		// this should always parse!
		panic("our pool_template is corrupt")
	}

	// Use the "poll_template" Resources as a template to create the correct
	// json structure for a CloudFormation stack
	tmpRes := poolTmpl.Get("Resources")
	asg := tmpRes.Get("asg_")
	elb := tmpRes.Get("elb_")
	lc := tmpRes.Get("lc_")

	poolSuffix := pool.Env + pool.Name

	// this is the Resource object we'll insert back into the template
	poolRes := simplejson.New()

	asgTags := []tag{
		tag{"Key": "Name",
			"Value":             fmt.Sprintf("%s-%s-%s", pool.BaseStackName, pool.Env, pool.Name),
			"PropagateAtLaunch": true},
		tag{"Key": "env",
			"Value":             pool.Env,
			"PropagateAtLaunch": true},
		tag{"Key": "pool",
			"Value":             pool.Name,
			"PropagateAtLaunch": true},
		tag{"Key": "source",
			"Value":             "galaxy",
			"PropagateAtLaunch": true},
	}

	asgProp := asg.Get("Properties")
	asgProp.Set("DesiredCapacity", strconv.Itoa(pool.DesiredCapacity))
	asgProp.Set("LaunchConfigurationName", ref{"lc" + poolSuffix})
	asgProp.Set("Tags", asgTags)
	asgProp.Set("VPCZoneIdentifier", pool.SubnetIDs)
	if pool.MaxSize > 0 {
		asgProp.Set("MaxSize", strconv.Itoa(pool.MaxSize))
	}
	if pool.MinSize > 0 {
		asgProp.Set("MinSize", strconv.Itoa(pool.MinSize))
	}

	lcProp := lc.Get("Properties")
	lcProp.Set("ImageId", pool.ImageID)
	lcProp.Set("InstanceType", pool.InstanceType)
	lcProp.Set("KeyName", pool.KeyName)
	lcProp.Set("SecurityGroups", pool.SecurityGroups)

	// Set the volue size for /dev/sda1 (the root volume)
	if pool.VolumeSize > 0 {
		lcProp.Set("BlockDeviceMappings", []blockDev{
			blockDev{
				DeviceName: "/dev/sda1",
				Ebs:        map[string]int{"VolumeSize": 12},
			},
		})
	}

	if pool.IAMRole != "" {
		lcProp.Set("IamInstanceProfile", pool.IAMRole)
	}

	if len(pool.ELBs) > 0 {
		for _, e := range pool.ELBs {
			if e.Listeners == nil {
				return nil, fmt.Errorf("ELB %s has no listeners", e.Name)
			}

			asgProp.Set("LoadBalancerNames", []ref{ref{"elb" + e.Name}})

			elbProp := elb.Get("Properties")
			elbProp.Get("HealthCheck").Set("Target", e.HealthCheck)
			elbProp.Set("SecurityGroups", e.SecurityGroups)
			elbProp.Set("Subnets", pool.SubnetIDs)
			elbProp.Set("Listeners", e.Listeners)

			poolRes.Set("elb"+e.Name, elb)
		}
	}

	poolRes.Set("asg"+poolSuffix, asg)
	poolRes.Set("lc"+poolSuffix, lc)

	poolTmpl.Set("Resources", poolRes)

	j, err := json.MarshalIndent(poolTmpl, "", "    ")
	if err != nil {
		return nil, err
	}
	return j, nil

}

func getCFService() (*aws.Service, error) {
	// only get the creds from the env for now
	auth, err := aws.GetAuth("", "", "", time.Now())
	if err != nil {
		return nil, err
	}

	serviceInfo := aws.ServiceInfo{
		Endpoint: "https://cloudformation.us-east-1.amazonaws.com/",
		Signer:   aws.V2Signature,
	}

	svc, err := aws.NewService(auth, serviceInfo)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

// List all resources associated with stackName
func ListStackResources(stackName string) (ListStackResourcesResponse, error) {
	listResp := ListStackResourcesResponse{}

	svc, err := getCFService()
	if err != nil {
		return listResp, err
	}

	params := map[string]string{
		"Action":    "ListStackResources",
		"StackName": stackName,
	}

	resp, err := svc.Query("POST", "/", params)
	if err != nil {
		return listResp, err
	}

	if resp.StatusCode != http.StatusOK {
		err := svc.BuildError(resp)
		return listResp, err
	}
	defer resp.Body.Close()

	err = xml.NewDecoder(resp.Body).Decode(&listResp)
	if err != nil {
		return listResp, err
	}
	return listResp, nil
}

// Describe all running stacks
func DescribeStacks() (DescribeStacksResponse, error) {
	descResp := DescribeStacksResponse{}

	svc, err := getCFService()
	if err != nil {
		return descResp, err
	}

	params := map[string]string{
		"Action": "DescribeStacks",
	}

	resp, err := svc.Query("POST", "/", params)
	if err != nil {
		return descResp, err
	}

	if resp.StatusCode != http.StatusOK {
		err := svc.BuildError(resp)
		return descResp, err
	}
	defer resp.Body.Close()

	err = xml.NewDecoder(resp.Body).Decode(&descResp)
	if err != nil {
		return descResp, err
	}
	return descResp, nil
}

func Exists(name string) (bool, error) {
	resp, err := DescribeStacks()
	if err != nil {
		return false, err
	}

	for _, stack := range resp.Stacks {
		if stack.Name == name {
			return true, nil
		}
	}

	return false, nil
}

// Wait for a stack creation to complete.
// Poll every 5s while the stack is in the CREATE_IN_PROGRESS state, and
// return nil when it enters CREATE_COMPLETE, or and error if it enters
// another state.
// Return and error of "timeout" if the timeout is reached.
func Wait(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := DescribeStacks()
		if err != nil {
			// I guess we should sleep and retry here, in case of intermittent
			// errors
			log.Println("DescribeStacks:", err)
			goto SLEEP
		}

		for _, stack := range resp.Stacks {
			if stack.Name == name {
				switch stack.Status {
				case "CREATE_IN_PROGRESS", "UPDATE_IN_PROGRESS":
					goto SLEEP
				case "CREATE_COMPLETE", "UPDATE_COMPLETE", "UPDATE_COMPLETE_CLEANUP_IN_PROGRESS":
					return nil
				default:
					return fmt.Errorf("%s:%s", stack.Status, stack.StatusReason)
				}
			}
		}

	SLEEP:
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout")
		}

		time.Sleep(5 * time.Second)
	}

}

// Return the SharedResources from our base stack that are needed for pool
// stacks. We need the IDs for subnets and security groups, since they cannot
// be referenced by name in a VPC. We also lookup the IAM instance profile
// created by the base stack for use in pool's launch configs.  This could be
// cached to disk so that we don't need to lookup the base stack to build a
// pool template.
func GetSharedResources(stackName string) (SharedResources, error) {
	shared := SharedResources{}
	res, err := ListStackResources(stackName)
	if err != nil {
		return shared, err
	}

	shared.SecurityGroups = make(map[string]string)
	shared.Subnets = make(map[string]string)
	shared.Roles = make(map[string]string)
	shared.Parameters = make(map[string]string)

	// we need to use DescribeStacks to get any parameters that were used in
	// the base stack, such as KeyPair
	descResp, err := DescribeStacks()
	if err != nil {
		return shared, err
	}

	// load all parameters from the base stack into the shared values
	for _, stack := range descResp.Stacks {
		if stack.Name == stackName {
			for _, param := range stack.Parameters {
				shared.Parameters[param.Key] = param.Value
			}
		}
	}

	for _, resource := range res.Resources {
		switch resource.Type {
		case "AWS::EC2::SecurityGroup":
			shared.SecurityGroups[resource.LogicalId] = resource.PhysicalId
		case "AWS::EC2::Subnet":
			shared.Subnets[resource.LogicalId] = resource.PhysicalId
		case "AWS::IAM::InstanceProfile":
			shared.Roles[resource.LogicalId] = resource.PhysicalId
		}
	}

	return shared, nil
}

func GetTemplate(name string) ([]byte, error) {
	svc, err := getCFService()
	if err != nil {
		log.Fatal(err)
	}

	params := map[string]string{
		"Action":    "GetTemplate",
		"StackName": name,
	}

	resp, err := svc.Query("POST", "/", params)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		err := svc.BuildError(resp)
		return nil, err
	}
	defer resp.Body.Close()

	tmplResp := GetTemplateResponse{}
	err = xml.NewDecoder(resp.Body).Decode(&tmplResp)

	return tmplResp.TemplateBody, err
}

// Create a CloudFormation stack
// The options map must include KeyPair, SubnetCIDRBlocks, and VPCCidrBlock
func Create(name string, stackTmpl []byte, options map[string]string) error {
	svc, err := getCFService()
	if err != nil {
		return err
	}

	params := map[string]string{
		"Action":       "CreateStack",
		"StackName":    name,
		"TemplateBody": string(stackTmpl),
	}

	optNum := 1
	for key, val := range options {
		params[fmt.Sprintf("Parameters.member.%d.ParameterKey", optNum)] = key
		params[fmt.Sprintf("Parameters.member.%d.ParameterValue", optNum)] = val
		optNum++
	}

	resp, err := svc.Query("POST", "/", params)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		err := svc.BuildError(resp)
		return err
	}
	defer resp.Body.Close()

	createResp := CreateStackResponse{}
	err = xml.NewDecoder(resp.Body).Decode(&createResp)
	if err != nil {
		return err
	}

	log.Println("CreateStack started")
	log.Println("RequestId:", createResp.RequestId)
	log.Println("StackId:", createResp.StackId)
	return nil
}

// Update an existing CloudFormation stack.
// The options map must include KeyPair
func Update(name string, stackTmpl []byte, options map[string]string) error {
	svc, err := getCFService()
	if err != nil {
		return err
	}

	params := map[string]string{
		"Action":       "UpdateStack",
		"StackName":    name,
		"TemplateBody": string(stackTmpl),
	}

	optNum := 1
	for key, val := range options {
		params[fmt.Sprintf("Parameters.member.%d.ParameterKey", optNum)] = key
		params[fmt.Sprintf("Parameters.member.%d.ParameterValue", optNum)] = val
		optNum++
	}

	resp, err := svc.Query("POST", "/", params)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		err := svc.BuildError(resp)
		return err
	}
	defer resp.Body.Close()

	updateResp := UpdateStackResponse{}
	err = xml.NewDecoder(resp.Body).Decode(&updateResp)
	if err != nil {
		return err
	}

	log.Println("UpdateStack started")
	log.Println("RequestId:", updateResp.RequestId)
	log.Println("StackId:", updateResp.StackId)
	return nil

}

// Delete and entire stack by name
func Delete(name string) error {
	svc, err := getCFService()
	if err != nil {
		return err
	}

	params := map[string]string{
		"Action":    "DeleteStack",
		"StackName": name,
	}

	resp, err := svc.Query("POST", "/", params)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		err := svc.BuildError(resp)
		return err
	}
	defer resp.Body.Close()

	deleteResp := DeleteStackResponse{}
	err = xml.NewDecoder(resp.Body).Decode(&deleteResp)
	if err != nil {
		return err
	}

	log.Println("DeleteStack started")
	log.Println("RequestId:", deleteResp.RequestId)
	return nil
}

// The default template used to create our base stack.
func GalaxyTemplate() []byte {
	return cloudformation_template
}
