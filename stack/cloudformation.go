package stack

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/crowdmob/goamz/aws"

	"github.com/litl/galaxy/log"
)

/*
Most of this should probably get wrapped up in a goamz/cloudformations package,
if someone wants to write out the entire API.

TODO: this is going to need some DRY love
*/

var ErrTimeout = fmt.Errorf("timeout")

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

type serverCert struct {
	ServerCertificateName string `xml:"ServerCertificateName"`
	Path                  string `xml:"Path"`
	Arn                   string `xml:"Arn"`
	UploadDate            string `xml:"UploadDate"`
	ServerCertificateId   string `xml:"ServerCertificateId"`
	Expiration            string `xml:"Expiration"`
}

type ListServerCertsResponse struct {
	RequestId string       `xml:"ResponseMetadata>RequestId"`
	Certs     []serverCert `xml:"ListServerCertificatesResult>ServerCertificateMetadataList>member"`
}

type stackEvent struct {
	EventId              string
	LogicalResourceId    string
	PhysicalResourceId   string
	ResourceProperties   string
	ResourceStatus       string
	ResourceStatusReason string
	ResourceType         string
	StackId              string
	StackName            string
	Timestamp            time.Time
}

type DescribeStackEventsResult struct {
	Events []stackEvent `xml:"DescribeStackEventsResult>StackEvents>member"`
}

type stackSummary struct {
	CreationTime        time.Time
	DeletionTime        time.Time
	LastUpdatedTime     time.Time
	StackId             string
	StackName           string
	StackStatus         string
	StackStatusReason   string
	TemplateDescription string
}

type ListStacksResponse struct {
	Stacks []stackSummary `xml:"ListStacksResult>StackSummaries>member"`
}

// Resources from the base stack that may need to be referenced from other
// stacks
type SharedResources struct {
	Subnets        map[string]string
	SecurityGroups map[string]string
	Roles          map[string]string
	Parameters     map[string]string
	ServerCerts    map[string]string
}

// return a list of the subnet values
func (s SharedResources) ListSubnets() []string {
	subnets := []string{}
	for _, val := range s.Subnets {
		subnets = append(subnets, val)
	}
	return subnets
}

func getService(svcName string) (*aws.Service, error) {
	services := map[string]string{
		"cf":  "https://cloudformation.us-east-1.amazonaws.com/",
		"iam": "https://iam.amazonaws.com/",
	}

	endPoint := services[svcName]
	if endPoint == "" {
		return nil, fmt.Errorf("unknown service")
	}

	// only get the creds from the env for now
	auth, err := aws.GetAuth("", "", "", time.Now())
	if err != nil {
		return nil, err
	}

	serviceInfo := aws.ServiceInfo{
		Endpoint: endPoint,
		Signer:   aws.V2Signature,
	}

	svc, err := aws.NewService(auth, serviceInfo)
	if err != nil {
		return nil, err
	}
	return svc, nil
}

// Lookup and unmarshal an existing stack into a Pool
func GetPool(name string) (*Pool, error) {
	pool := &Pool{}

	poolTmpl, err := GetTemplate(name)
	if err != nil {
		return pool, err
	}

	if err := json.Unmarshal(poolTmpl, pool); err != nil {
		return nil, err
	}

	return pool, nil
}

// List all resources associated with stackName
func ListStackResources(stackName string) (ListStackResourcesResponse, error) {
	listResp := ListStackResourcesResponse{}

	svc, err := getService("cf")
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
func DescribeStacks(name string) (DescribeStacksResponse, error) {
	descResp := DescribeStacksResponse{}

	svc, err := getService("cf")
	if err != nil {
		return descResp, err
	}

	params := map[string]string{
		"Action": "DescribeStacks",
	}

	if name != "" {
		params["StackName"] = name
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

// Describe a Stack's Events
func DescribeStackEvents(name string) (DescribeStackEventsResult, error) {
	descResp := DescribeStackEventsResult{}

	svc, err := getService("cf")
	if err != nil {
		return descResp, err
	}

	params := map[string]string{
		"Action": "DescribeStackEvents",
	}

	if name != "" {
		params["StackName"] = name
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

// return a list of all actives stacks
func ListActive() ([]string, error) {
	resp, err := DescribeStacks("")
	if err != nil {
		return nil, err
	}

	stacks := []string{}
	for _, stack := range resp.Stacks {
		stacks = append(stacks, stack.Name)
	}

	return stacks, nil
}

// List all stacks
// This lists all stacks including inactive and deleted.
func List() (ListStacksResponse, error) {
	listResp := ListStacksResponse{}

	svc, err := getService("cf")
	if err != nil {
		return listResp, err
	}

	params := map[string]string{
		"Action": "ListStacks",
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

func Exists(name string) (bool, error) {
	resp, err := DescribeStacks(name)
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

// Wait for a stack event to complete.
// Poll every 5s while the stack is in the CREATE_IN_PROGRESS or
// UPDATE_IN_PROGRESS state, and succeed when it enters a successful _COMPLETE
// state.
// Return and error of ErrTimeout if the timeout is reached.
func Wait(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := DescribeStacks(name)
		if err != nil {
			if err, ok := err.(*aws.Error); ok {
				// the call was successful, but AWS returned an error
				// no need to wait.
				return err
			}

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
			return ErrTimeout
		}

		time.Sleep(5 * time.Second)
	}
}

// Like the Wait function, but instead if returning as soon as there is an
// error, always wait for a final status.
// ** This assumes all _COMPLETE statuses are final, and all final statuses end
//    in _COMPLETE.
func WaitForComplete(id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := DescribeStackEvents(id)
		if err != nil {
			return err
		} else if len(resp.Events) == 0 {
			return fmt.Errorf("no events for stack %s", id)
		}

		//TODO: are these always in order?!
		latest := resp.Events[0]

		if strings.HasSuffix(latest.ResourceStatus, "_COMPLETE") {
			return nil
		}

		if time.Now().After(deadline) {
			return ErrTimeout
		}

		time.Sleep(5 * time.Second)
	}
}

// Get a list of SSL certificates from the IAM service.
// Cloudformation templates need to reference certs via their ARNs.
func ListServerCertificates() (ListServerCertsResponse, error) {
	certResp := ListServerCertsResponse{}

	svc, err := getService("iam")
	if err != nil {
		return certResp, err
	}

	params := map[string]string{
		"Action":  "ListServerCertificates",
		"Version": "2010-05-08",
	}

	resp, err := svc.Query("POST", "/", params)
	if err != nil {
		return certResp, err
	}

	if resp.StatusCode != http.StatusOK {
		err := svc.BuildError(resp)
		return certResp, err
	}
	defer resp.Body.Close()

	err = xml.NewDecoder(resp.Body).Decode(&certResp)
	if err != nil {
		return certResp, err
	}

	return certResp, nil
}

// Return the SharedResources from our base stack that are needed for pool
// stacks. We need the IDs for subnets and security groups, since they cannot
// be referenced by name in a VPC. We also lookup the IAM instance profile
// created by the base stack for use in pool's launch configs.  This could be
// cached to disk so that we don't need to lookup the base stack to build a
// pool template.
func GetSharedResources(stackName string) (SharedResources, error) {
	shared := SharedResources{}
	shared.SecurityGroups = make(map[string]string)
	shared.Subnets = make(map[string]string)
	shared.Roles = make(map[string]string)
	shared.Parameters = make(map[string]string)
	shared.ServerCerts = make(map[string]string)

	// we need to use DescribeStacks to get any parameters that were used in
	// the base stack, such as KeyName
	descResp, err := DescribeStacks(stackName)
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

	res, err := ListStackResources(stackName)
	if err != nil {
		return shared, err
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

	// now we need to find any server certs we may have
	certResp, err := ListServerCertificates()
	if err != nil {
		// we've made it this far, just log this error so we can at least get the CF data
		log.Error("error listing server certificates:", err)
	}

	for _, cert := range certResp.Certs {
		shared.ServerCerts[cert.ServerCertificateName] = cert.Arn
	}

	return shared, nil
}

func GetTemplate(name string) ([]byte, error) {
	svc, err := getService("cf")
	if err != nil {
		return nil, err
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
// Request parameters which are taken from the options:
//   StackPolicyDuringUpdateBody
func Create(name string, stackTmpl []byte, options map[string]string) error {
	svc, err := getService("cf")
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
		if key == "StackPolicyDuringUpdateBody" {
			params["StackPolicyDuringUpdateBody"] = val
			continue
		}
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
// Request parameters which are taken from the options:
//   StackPolicyDuringUpdateBody
func Update(name string, stackTmpl []byte, options map[string]string) error {
	svc, err := getService("cf")
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
		if key == "StackPolicyDuringUpdateBody" {
			params["StackPolicyDuringUpdateBody"] = val
			continue
		}
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
	svc, err := getService("cf")
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

// set a stack policy
// TODO: add delete policy
func SetPolicy(name string, policy []byte) error {
	svc, err := getService("cf")
	if err != nil {
		return err
	}

	params := map[string]string{
		"Action":          "SetStackPolicy",
		"StackName":       name,
		"StackPolicyBody": string(policy),
	}

	resp, err := svc.Query("POST", "/", params)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return svc.BuildError(resp)
	}

	return nil
}
