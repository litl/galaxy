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
	"github.com/ryanuber/columnize"
)

// return --base, or try to find a base clodformation stack
func getBase(c *cli.Context) string {
	errNoBase := fmt.Errorf("could not identify a unique base stack")

	base := c.String("base")
	if base != "" {
		return base
	}

	stacks, err := stack.ListActive()
	if err != nil {
		log.Fatal(err)
	}

	for _, name := range stacks {
		parts := strings.Split(name, "-")

		// the best we can do for now is look for a stack with a single word
		if len(parts) == 1 {
			if base != "" {
				err = errNoBase
			}
			base = name
		}

		// or check for "-base" in the name
		for _, p := range parts {
			if p == "base" {
				if base != "" {
					err = errNoBase
				}
				base = name
			}
		}
	}

	if err != nil {
		log.Fatalf("%s: %s", err, "use --base")
	}

	return base
}

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

	keyName := c.String("keyname")
	if keyName == "" {
		keyName = promptValue("EC2 Keypair Name", "required")
		if keyName == "required" {
			log.Fatal("keyname required")
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
		"KeyName":                keyName,
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
	if p := c.String("parameters"); p != "" {
		paramJSON, err := jsonFromArg(p)
		if err != nil {
			log.Fatal("Error decoding parameters:", err)
		}

		err = json.Unmarshal(paramJSON, &params)
		if err != nil {
			log.Fatal(err)
		}
	}

	template := c.String("template")
	if template != "" {
		stackTmpl, err = jsonFromArg(template)
		if err != nil {
			log.Fatal(err)
		}
	}

	if policy := c.String("policy"); policy != "" {
		policyJSON, err := jsonFromArg(policy)
		if err != nil {
			log.Fatal("policy error:", err)
		}

		params["StackPolicyDuringUpdateBody"] = string(policyJSON)
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
	resources, err := stack.GetSharedResources(getBase(c))
	if err != nil {
		log.Fatal(err)
	}

	keyName := c.String("keyname")
	if keyName != "" {
		resources.Parameters["KeyName"] = keyName
	}

	amiID := c.String("ami")
	if amiID != "" {
		resources.Parameters["PoolImageId"] = amiID
	}

	instanceType := c.String("instance-type")
	if instanceType != "" {
		resources.Parameters["PoolInstanceType"] = instanceType
	}

	return resources
}

func stackCreatePool(c *cli.Context) {
	var err error

	poolName := utils.GalaxyPool(c)
	if poolName == "" {
		log.Fatal("pool name required")
	}

	baseStack := getBase(c)

	poolEnv := utils.GalaxyEnv(c)
	if poolEnv == "" {
		log.Fatal("env required")
	}

	stackName := fmt.Sprintf("%s-%s-%s", baseStack, poolEnv, poolName)

	pool := stack.NewPool()

	// get the resources we need from the base stack
	// TODO: this may search for the base stack a second time
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
		sslCert = resources.ServerCerts[cert]
		if sslCert == "" {
			log.Fatalf("Could not find certificate '%s'", cert)
		}
	}

	// Create our Launch Config
	lc := pool.LCTemplate
	lcName := "lc" + poolEnv + poolName

	if amiID := c.String("ami"); amiID != "" {
		lc.Properties.ImageId = amiID
	} else {
		lc.Properties.ImageId = resources.Parameters["PoolImageId"]
	}

	if insType := c.String("instance-type"); insType != "" {
		lc.Properties.InstanceType = insType
	} else {
		lc.Properties.InstanceType = resources.Parameters["PoolInstanceType"]
	}

	if keyName := c.String("keyname"); keyName != "" {
		lc.Properties.KeyName = keyName
	} else {
		lc.Properties.KeyName = resources.Parameters["KeyName"]
	}

	lc.Properties.IamInstanceProfile = resources.Roles["galaxyInstanceProfile"]

	lc.Properties.SecurityGroups = []string{
		resources.SecurityGroups["sshSG"],
		resources.SecurityGroups["defaultSG"],
	}

	// WARNING: magic constant needs a config somewhere
	lc.SetVolumeSize(100)

	pool.Resources[lcName] = lc

	// Create the Auto Scaling Group
	asg := pool.ASGTemplate
	asgName := "asg" + poolEnv + poolName

	asg.AddTag("Name", fmt.Sprintf("%s-%s-%s", baseStack, poolEnv, poolName), true)
	asg.AddTag("env", poolEnv, true)
	asg.AddTag("pool", poolName, true)
	asg.AddTag("source", "galaxy", true)

	if desiredCap > 0 {
		asg.Properties.DesiredCapacity = desiredCap
	}

	asg.SetLaunchConfiguration(lcName)
	asg.Properties.VPCZoneIdentifier = resources.ListSubnets()
	if maxSize > 0 {
		asg.Properties.MaxSize = maxSize
	}
	if minSize > 0 {
		asg.Properties.MinSize = minSize
	}

	if c.Bool("auto-update") {
		// TODO: configure this somehow
		asg.SetASGUpdatePolicy(1, 1, 5*time.Minute)
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

		elb.AddListener(80, "HTTP", httpPort, "HTTP", "", nil)

		if sslCert != "" {
			elb.AddListener(443, "HTTPS", httpPort, "HTTP", sslCert, nil)
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
		log.Error(err)
		log.Error("CreateStack Failed, attempting to delete")

		waitAndDelete(stackName)
		return
	}

	log.Println("CreateStack complete")
}

// wait until a stack is in a final state, then delete it
func waitAndDelete(name string) {
	// we need to get the StackID in order to lookup DELETE events
	desc, err := stack.DescribeStacks(name)
	if err != nil {
		log.Fatal(err)
	} else if len(desc.Stacks) == 0 {
		log.Fatal("could not describe stack:", name)
	}

	stackId := desc.Stacks[0].Id

	err = stack.WaitForComplete(stackId, 5*time.Minute)
	if err != nil {
		log.Fatal(err)
	}

	err = stack.Delete(name)
	if err != nil {
		log.Fatal(err)
	}

	// wait
	err = stack.WaitForComplete(stackId, 5*time.Minute)
	if err != nil {
		log.Fatal(err)
	}
}

// Update an existing Pool Stack
func stackUpdatePool(c *cli.Context) {
	poolName := utils.GalaxyPool(c)
	if poolName == "" {
		log.Fatal("pool name required")
	}

	baseStack := getBase(c)

	poolEnv := utils.GalaxyEnv(c)
	if poolEnv == "" {
		log.Fatal("env required")
	}

	stackName := fmt.Sprintf("%s-%s-%s", baseStack, poolEnv, poolName)

	pool, err := stack.GetPool(stackName)
	if err != nil {
		log.Fatal(err)
	}

	options := make(map[string]string)
	if policy := c.String("policy"); policy != "" {
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

	if c.Bool("auto-update") {
		// TODO: configure this somehow
		// note that the max pause is only PT5M30S
		asg.SetASGUpdatePolicy(1, 1, 5*time.Minute)
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

	if elb != nil {
		for _, l := range elb.Properties.Listeners {
			if sslCert != "" && l.Protocol == "HTTPS" {
				l.SSLCertificateId = sslCert
			}

			if httpPort > 0 {
				l.InstancePort = httpPort
			}
		}
	}

	lc := pool.LC()
	if amiID := c.String("ami"); amiID != "" {
		lc.Properties.ImageId = amiID
	} else {
		lc.Properties.ImageId = resources.Parameters["PoolImageId"]
	}

	if insType := c.String("instance-type"); insType != "" {
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

func stackDeletePool(c *cli.Context) {
	poolName := utils.GalaxyPool(c)
	if poolName == "" {
		log.Fatal("pool name required")
	}

	baseStack := getBase(c)

	poolEnv := utils.GalaxyEnv(c)
	if poolEnv == "" {
		log.Fatal("env required")
	}

	stackName := fmt.Sprintf("%s-%s-%s", baseStack, poolEnv, poolName)

	err := stack.Delete(stackName)
	if err != nil {
		log.Fatal(err)
	}
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

func stackList(c *cli.Context) {
	descResp, err := stack.DescribeStacks("")
	if err != nil {
		log.Fatal(err)
	}

	stacks := []string{"stack | status | "}

	for _, stack := range descResp.Stacks {
		s := fmt.Sprintf("%s | %s | %s", stack.Name, stack.Status, stack.StatusReason)
		stacks = append(stacks, s)
	}

	output, _ := columnize.SimpleFormat(stacks)
	log.Println(output)
}
