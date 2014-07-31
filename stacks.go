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
	"github.com/litl/galaxy/utils"
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

func sharedResources(c *cli.Context) stack.SharedResources {
	// get the resources we need from the base stack
	resources, err := stack.GetSharedResources(c.GlobalString("base"))
	if err != nil {
		log.Fatal(err)
	}

	keyPair := c.GlobalString("keyname")
	if keyPair != "" {
		resources.Parameters["KeyPair"] = keyPair
	}

	amiID := c.GlobalString("ami")
	if amiID != "" {
		resources.Parameters["PoolImageId"] = amiID
	}

	instanceType := c.GlobalString("instance-type")
	if instanceType != "" {
		resources.Parameters["PoolInstanceType"] = instanceType
	}

	return resources
}

func stackCreatePool(c *cli.Context) {
	var err error

	// there's some json "omitempty" bool fields we need to set
	True := new(bool)
	*True = true

	poolName := utils.GalaxyPool(c)
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

	pool := stack.NewPool()

	// get the resources we need from the base stack
	resources := sharedResources(c)

	desiredCap := c.Int("desired-size")
	minSize := c.Int("min-size")
	maxSize := c.Int("max-size")
	httpPort := c.Int("http-port")
	if httpPort == 0 {
		httpPort = 80
	}

	sslCert := ""
	if cert := c.String("ssl-cert"); cert != "" {
		sslCert = resources.ServerCerts[sslCert]
		if sslCert == "" {
			log.Fatalf("Could not find certificate '%s'", sslCert)
		}
	}

	// Create our Launch Config
	lc := pool.LCTemplate
	lcName := "lc" + poolEnv + poolName

	if amiID := c.GlobalString("ami"); amiID != "" {
		lc.Properties.ImageId = amiID
	} else {
		lc.Properties.ImageId = resources.Parameters["PoolImageId"]
	}

	if insType := c.GlobalString("instance-type"); insType != "" {
		lc.Properties.InstanceType = insType
	} else {
		lc.Properties.InstanceType = resources.Parameters["PoolInstanceType"]
	}

	if keyName := c.GlobalString("keypair"); keyName != "" {
		lc.Properties.KeyName = keyName
	} else {
		lc.Properties.KeyName = resources.Parameters["KeyPair"]
	}

	lc.Properties.IamInstanceProfile = resources.Roles["galaxyInstanceProfile"]

	lc.Properties.SecurityGroups = []string{
		resources.SecurityGroups["sshSG"],
		resources.SecurityGroups["defaultSG"],
	}

	// WARNING: magic constant needs a config somewhere
	lc.Properties.BlockDeviceMappings[0].Ebs.VolumeSize = 100

	pool.Resources[lcName] = lc

	// Create the Auto Scaling Group
	asg := pool.ASGTemplate
	asgName := "asg" + poolEnv + poolName

	asg.Properties.Tags = []stack.Tag{
		{Key: "Name",
			Value:             fmt.Sprintf("%s-%s-%s", baseStack, poolEnv, poolName),
			PropagateAtLaunch: True},
		{Key: "env",
			Value:             poolEnv,
			PropagateAtLaunch: True},
		{Key: "pool",
			Value:             poolName,
			PropagateAtLaunch: True},
		{Key: "source",
			Value:             "galaxy",
			PropagateAtLaunch: True},
	}

	if desiredCap > 0 {
		asg.Properties.DesiredCapacity = desiredCap
	}

	asg.Properties.LaunchConfigurationName = stack.Intrinsic{"Ref": lcName}
	asg.Properties.VPCZoneIdentifier = resources.ListSubnets()
	if maxSize > 0 {
		asg.Properties.MaxSize = maxSize
	}
	if minSize > 0 {
		asg.Properties.MinSize = minSize
	}

	pool.Resources[asgName] = asg

	// Optionally create the Elastic Load Balancer
	if strings.Contains(poolName, "web") {
		elb := pool.ELBTemplate
		elbName := "elb" + poolEnv + poolName

		// make sure to add this to the ASG
		asg.AddLoadBalancer(elbName)

		elb.Properties.Subnets = resources.ListSubnets()

		elb.Properties.SecurityGroups = []string{
			resources.SecurityGroups["webSG"],
			resources.SecurityGroups["defaultSG"],
		}
		elb.Properties.HealthCheck.Target = fmt.Sprintf("HTTP:%d/", httpPort)

		listener := &stack.Listener{
			LoadBalancerPort: 80,
			Protocol:         "HTTP",
			InstancePort:     httpPort,
			InstanceProtocol: "HTTP",
		}

		elb.Properties.Listeners = []*stack.Listener{listener}

		if sslCert != "" {
			listener := &stack.Listener{
				LoadBalancerPort: 443,
				Protocol:         "HTTPS",
				InstancePort:     httpPort,
				InstanceProtocol: "HTTP",
				SSLCertificateId: sslCert,
			}
			elb.Properties.Listeners = append(elb.Properties.Listeners, listener)
		}

		pool.Resources[elbName] = elb
	}

	poolTmpl, err := json.MarshalIndent(pool, "", "    ")
	if err != nil {
		log.Fatal(err)
	}

	if c.Bool("print") {
		fmt.Println(string(poolTmpl))
		return
	}

	if err := stack.Create(stackName, poolTmpl, nil); err != nil {
		log.Fatal(err)
	}

	// do we want to wait on this by default?
	if err := stack.Wait(stackName, 5*time.Minute); err != nil {
		log.Fatal(err)
	}
	log.Println("CreateStack complete")
}

func stackUpdatePool(c *cli.Context) {
	poolName := utils.GalaxyPool(c)
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

	pool, err := stack.GetPool(stackName)
	if err != nil {
		log.Fatal(err)
	}

	options := make(map[string]string)
	if policy := c.String("update-policy"); policy != "" {
		policyJSON, err := jsonFromArg(policy)
		if err != nil {
			log.Fatal("policy error:", err)
		}

		options["StackPolicyDuringUpdateBody"] = string(policyJSON)
	}

	resources := sharedResources(c)

	asg := pool.ASG()
	if asg == nil {
		log.Fatal("missing ASG")
	}

	if c.Int("desired-size") > 0 {
		asg.Properties.DesiredCapacity = c.Int("desired-size")
	}

	if c.Int("min-size") > 0 {
		asg.Properties.MinSize = c.Int("min-size")
	}

	if c.Int("max-size") > 0 {
		asg.Properties.MaxSize = c.Int("max-size")
	}

	elb := pool.ELB()

	sslCert := ""
	if cert := c.String("ssl-cert"); cert != "" {
		sslCert = resources.ServerCerts[sslCert]
		if sslCert == "" {
			log.Fatalf("Could not find certificate '%s'", sslCert)
		}
	}

	httpPort := c.Int("http-port")

	if (sslCert != "" || httpPort > 0) && elb == nil {
		log.Fatal("Pool does not have an ELB")
	}

	for _, l := range elb.Properties.Listeners {
		if sslCert != "" && l.Protocol == "HTTPS" {
			l.SSLCertificateId = sslCert
		}

		if httpPort > 0 {
			l.InstancePort = httpPort
		}
	}

	lc := pool.LC()
	if amiID := c.GlobalString("ami"); amiID != "" {
		lc.Properties.ImageId = amiID
	} else {
		lc.Properties.ImageId = resources.Parameters["PoolImageId"]
	}

	if insType := c.GlobalString("instance-type"); insType != "" {
		lc.Properties.InstanceType = insType
	} else {
		lc.Properties.InstanceType = resources.Parameters["PoolInstanceType"]
	}

	poolTmpl, err := json.MarshalIndent(pool, "", "    ")
	if err != nil {
		log.Fatal(err)
	}

	if c.Bool("print") {
		fmt.Println(string(poolTmpl))
		return
	}

	if err := stack.Update(stackName, poolTmpl, options); err != nil {
		log.Fatal(err)
	}

	// do we want to wait on this by default?
	if err := stack.Wait(stackName, 5*time.Minute); err != nil {
		log.Fatal(err)
	}

	log.Println("UpdateStack complete")
}

// delete a pool
func stackDelete(c *cli.Context) {
	stackName := c.Args().First()
	if stackName == "" {
		log.Fatal("stack name required")
	}

	ok := c.Bool("y")
	if !ok {
		switch strings.ToLower(promptValue(fmt.Sprintf("\nDelete Stack '%s'?", stackName), "n")) {
		case "y", "yes":
			ok = true
		}
	}

	if !ok {
		log.Fatal("aborted")
	}

	err := stack.Delete(stackName)
	if err != nil {
		log.Fatal(err)
	}
}
