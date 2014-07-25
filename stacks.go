package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"github.com/crowdmob/goamz/aws"
	"github.com/litl/galaxy/log"
	"github.com/litl/galaxy/stack"
)

func promptValue(prompt, dflt string) string {
	if !tty {
		return dflt
	}

	fmt.Printf("%s [%s]: ", prompt, dflt)

	val, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		log.Println(err)
		return dflt
	}

	val = strings.TrimSpace(val)

	// return the default if the input was empty
	if len(val) == 0 {
		return dflt
	}

	return val
}

func getInitOpts(c *cli.Context) map[string]string {

	keyPair := c.GlobalString("keypair")
	if keyPair == "" {
		keyPair = promptValue("EC2 Keypair Name", "required")
		if keyPair == "required" {
			log.Fatal("keypair required")
		}
	}

	controllerAMI := promptValue("Controller AMI", "ami-018c9568")
	controllerInstance := promptValue("Controller Instance Type", "t2.medium")
	poolAMI := promptValue("Default Pool AMI", "ami-018c9568")
	poolInstance := promptValue("Default Pool Instance Type", "t2.medium")

	vpcSubnet := promptValue("VPC Subnet", "10.24.0.0/16")
	// some *very* basic input verification
	if !strings.Contains(vpcSubnet, "/") || strings.Count(vpcSubnet, ".") != 3 {
		log.Fatal("VPC Subnet must be in CIDR notation")
	}

	azSubnets := promptValue("AvailabilityZone Subnets", "10.24.1.0/24, 10.24.2.0/24, 10.24.3.0/24")
	if strings.Count(azSubnets, ",") != 2 || strings.Count(azSubnets, "/") != 3 {
		log.Fatal("There must be 3 comma separated AZ Subnets")
	}

	opts := map[string]string{
		"KeyPair":                keyPair,
		"ControllerImageId":      controllerAMI,
		"ControllerInstanceType": controllerInstance,
		"PoolImageId":            poolAMI,
		"PoolInstanceType":       poolInstance,
		"VPCCidrBlock":           vpcSubnet,
		"SubnetCidrBlocks":       azSubnets,
	}

	return opts
}

// Return json supplied in the argument, or look for a file by the name given.
// Is the name is "STDIN", read the json from stdin
func jsonFromArg(arg string) ([]byte, error) {
	var jsonArg []byte
	var err error

	arg = strings.TrimSpace(arg)

	// assume that an opening brack mean the json is given directly
	if strings.HasPrefix(arg, "{") {
		jsonArg = []byte(arg)
	} else if arg == "STDIN" {
		jsonArg, err = ioutil.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
	} else {
		// all else fails, look for a file
		jsonArg, err = ioutil.ReadFile(arg)
		if err != nil {
			return nil, err
		}
	}

	// verify the json by compacting it
	buf := bytes.NewBuffer(nil)
	err = json.Compact(buf, jsonArg)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// create our base stack
func stackInit(c *cli.Context) {
	stackName := c.Args().First()
	if stackName == "" {
		log.Fatal("stack name required")
	}

	exists, err := stack.Exists(stackName)
	if exists {
		log.Fatalf("stack %s already exists", stackName)
	} else if err != nil {
		log.Fatal(err)
	}

	stackTmpl := stack.GalaxyTemplate()

	opts := getInitOpts(c)

	err = stack.Create(stackName, stackTmpl, opts)
	if err != nil {
		log.Fatal(err)
	}
}

// update the base stack
func stackUpdate(c *cli.Context) {
	var stackTmpl []byte
	var err error

	stackName := c.Args().First()
	if stackName == "" {
		log.Fatal("stack name required")
	}

	params := make(map[string]string)
	if c.String("parameters") != "" {
		err := json.Unmarshal([]byte(c.String("parameters")), &params)
		if err != nil {
			log.Fatal("Error decoding parameters:", err)
		}
	}

	template := c.String("template")
	if template != "" {
		stackTmpl, err = jsonFromArg(template)
		if err != nil {
			log.Fatal(err)
		}
	}

	if len(stackTmpl) == 0 {
		// get the current running template
		stackTmpl, err = stack.GetTemplate(stackName)
		if err != nil {
			log.Fatal(err)
		}
	}

	// this reads the Parameters supplied for our current stack for us
	shared, err := stack.GetSharedResources(stackName)
	if err != nil {
		log.Fatal(err)
	}

	// add any missing parameters to our
	for key, val := range shared.Parameters {
		if params[key] == "" {
			params[key] = val
		}
	}

	p, _ := json.MarshalIndent(params, "", "  ")
	ok := promptValue(fmt.Sprintf("\nUpdate the [%s] stack with:\n%s\nAccept?", stackName, string(p)), "n")
	switch strings.ToLower(ok) {
	case "y", "yes":
		err = stack.Update(stackName, stackTmpl, params)
		if err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("aborted")
	}
}

// Print a Cloudformation template to stdout.
func stackTemplate(c *cli.Context) {
	stackName := c.Args().First()

	if stackName == "" {
		if _, err := os.Stdout.Write(stack.GalaxyTemplate()); err != nil {
			log.Fatal(err)
		}
		return
	}

	stackTmpl, err := stack.GetTemplate(stackName)
	if err != nil {
		if err, ok := err.(*aws.Error); ok {
			if err.Code == "ValidationError" && strings.Contains(err.Message, "does not exist") {
				log.Fatalf("Stack '%s' does not exist", stackName)
			}
		}
		log.Fatal(err)
	}

	if _, err := os.Stdout.Write(stackTmpl); err != nil {
		log.Fatal(err)
	}
}

func stackCreatePool(c *cli.Context) {
	stackPool(c, false)
}

func stackUpdatePool(c *cli.Context) {
	stackPool(c, true)
}

// TODO: this function has gotten very long
// manually create a pool stack
func stackPool(c *cli.Context, update bool) {
	var err error
	options := make(map[string]string)

	poolName := c.GlobalString("pool")
	if poolName == "" {
		log.Fatal("pool name required")
	}

	baseStack := c.GlobalString("base")
	if baseStack == "" {
		log.Fatal("base stack required")
	}

	poolEnv := c.GlobalString("env")
	if poolEnv == "" {
		log.Fatal("env required")
	}

	stackName := fmt.Sprintf("%s-%s-%s", baseStack, poolEnv, poolName)

	if policy := c.String("update-policy"); policy != "" && update {
		policyJSON, err := jsonFromArg(policy)
		if err != nil {
			log.Fatal("policy error:", err)
		}

		options["StackPolicyDuringUpdateBody"] = string(policyJSON)
	}

	// get the resources we need from the base stack
	resources, err := stack.GetSharedResources(baseStack)
	if err != nil {
		log.Fatal(err)
	}

	var oldPool stack.Pool
	if update {
		oldPool, err = stack.DescribePoolStack(stackName)
		if err != nil {
			log.Fatal(err)
		}
	}

	keyPair := c.GlobalString("keyname")
	if keyPair == "" {
		keyPair = oldPool.KeyName
	}
	if keyPair == "" {
		keyPair = resources.Parameters["KeyPair"]
	}
	if keyPair == "" {
		log.Fatal("KeyPair required")
	}

	amiID := c.GlobalString("ami")
	if amiID == "" {
		amiID = oldPool.ImageID
	}
	if amiID == "" {
		amiID = resources.Parameters["PoolImageId"]
	}

	instanceType := c.GlobalString("instance-type")
	if instanceType == "" {
		instanceType = oldPool.InstanceType
	}
	if instanceType == "" {
		instanceType = resources.Parameters["PoolInstanceType"]
	}

	// add all subnets
	subnets := []string{}
	for _, val := range resources.Subnets {
		subnets = append(subnets, val)
	}

	desiredCap := c.Int("desired-size")
	if desiredCap == 0 {
		desiredCap = oldPool.DesiredCapacity
	}

	minSize := c.Int("min-size")
	if minSize == 0 {
		minSize = oldPool.MinSize
	}

	maxSize := c.Int("max-size")
	if maxSize == 0 {
		maxSize = oldPool.MaxSize
	}

	sslCert := c.String("ssl-cert")

	httpPort := c.Int("http-port")
	if httpPort == 0 {
		httpPort = 80
	}

	volumeSize := 100
	if oldPool.VolumeSize > 0 {
		volumeSize = oldPool.VolumeSize
	}

	// TODO: add more options
	pool := stack.Pool{
		Name:            poolName,
		Env:             poolEnv,
		DesiredCapacity: desiredCap,
		MinSize:         minSize,
		MaxSize:         maxSize,
		KeyName:         keyPair,
		InstanceType:    instanceType,
		ImageID:         amiID,
		IAMRole:         resources.Roles["galaxyInstanceProfile"],
		SubnetIDs:       subnets,
		SecurityGroups: []string{
			resources.SecurityGroups["sshSG"],
			resources.SecurityGroups["defaultSG"],
		},
		VolumeSize:    volumeSize,
		BaseStackName: baseStack,
	}

	if strings.Contains(poolName, "web") {
		elb := stack.PoolELB{
			Name: poolEnv + poolName,

			SecurityGroups: []string{
				resources.SecurityGroups["webSG"],
				resources.SecurityGroups["defaultSG"],
			},
			HealthCheck: fmt.Sprintf("HTTP:%d/", httpPort),
		}

		listener := stack.PoolELBListener{
			LoadBalancerPort: 80,
			Protocol:         "HTTP",
			InstancePort:     httpPort,
			InstanceProtocol: "HTTP",
		}

		elb.Listeners = append(elb.Listeners, listener)

		if sslCert != "" {
			certID := resources.ServerCerts[sslCert]
			if certID == "" {
				log.Fatalf("Could not find certificate '%s'", sslCert)
			}

			listener := stack.PoolELBListener{
				LoadBalancerPort: 443,
				Protocol:         "HTTPS",
				InstancePort:     httpPort,
				InstanceProtocol: "HTTP",
				SSLCertificateId: certID,
			}
			elb.Listeners = append(elb.Listeners, listener)
		}

		pool.ELBs = append(pool.ELBs, elb)
	}

	poolTmpl, err := stack.CreatePoolTemplate(pool)
	if err != nil {
		log.Fatal(err)
	}

	switch update {
	case true:
		updatePool(poolTmpl, stackName, options)
	case false:
		createPool(poolTmpl, stackName, options)
	}
}

func createPool(poolTmpl []byte, stackName string, opts map[string]string) {
	if err := stack.Create(stackName, poolTmpl, opts); err != nil {
		log.Fatal(err)
	}

	// do we want to wait on this by default?
	if err := stack.Wait(stackName, 5*time.Minute); err != nil {
		log.Fatal(err)
	}
	log.Println("CreateStack complete")
}

func updatePool(poolTmpl []byte, stackName string, opts map[string]string) {
	if err := stack.Update(stackName, poolTmpl, opts); err != nil {
		log.Fatal(err)
	}

	// do we want to wait on this by default?
	if err := stack.Wait(stackName, 5*time.Minute); err != nil {
		log.Fatal(err)
	}

	log.Println("UpdateStack complete")
}

func stackDelete(c *cli.Context) {
	stackName := c.Args().First()
	if stackName == "" {
		log.Fatal("stack name required")
	}

	switch strings.ToLower(promptValue(fmt.Sprintf("\nDelete Stack '%s'?", stackName), "n")) {
	case "y", "yes":
		err := stack.Delete(stackName)
		if err != nil {
			log.Fatal(err)
		}
	default:
		log.Println("aborted")
	}
}
