package stack

// This JSON document lays out the basic structure of the CloudFormation
// template for our pool stacks. The Resources here will be transfered to the
// Pool.*Template attributes.
// Make certain that the appropriate Pool structures are modified when changing
// the structure of this template.
var poolTmpl = []byte(`
{
    "AWSTemplateFormatVersion": "2010-09-09",
    "Description": "Galaxy Pool Template",
    "Resources": {
        "asg_": {
            "Properties": {
                "AvailabilityZones": {
                    "Fn::GetAZs": ""
                },
                "Cooldown": "300",
				"DesiredCapacity": "1",
                "HealthCheckGracePeriod": "300",
                "HealthCheckType": "EC2",
                "LaunchConfigurationName": {},
				"MinSize": "1",
				"MaxSize": "1",
				"Tags": [],
				"VPCZoneIdentifier": []
            },
			"Type": "AWS::AutoScaling::AutoScalingGroup",
            "UpdatePolicy" : {
                "AutoScalingRollingUpdate" : {
                    "MinInstancesInService" : "0",
                    "MaxBatchSize" : "1",
                    "PauseTime" : "PT5M"
                }
            }
        },
		"elb_": {
			"Properties": {
				"HealthCheck": {
					"HealthyThreshold": "2",
					"Interval": "30",
					"Target": "HTTP:80/health",
					"Timeout": "5",
					"UnhealthyThreshold": "2"
				},
				"Listeners": [
					{
						"InstancePort": "80",
						"InstanceProtocol": "HTTP",
						"LoadBalancerPort": "80",
						"Protocol": "HTTP"
					}
				],
				"SecurityGroups": [],
				"Subnets": []
			},
			"Type": "AWS::ElasticLoadBalancing::LoadBalancer"
		},
		"lc_": {
			"Properties": {
				"AssociatePublicIpAddress": true,
				"BlockDeviceMappings": [
					{
						"DeviceName": "/dev/sda1",
						"Ebs": {
							"VolumeType": "gp2",
							"VolumeSize": 8
						}
					}
				],
				"InstanceType": "",
				"KeyName": "",
				"SecurityGroups": []
			},
			"Type": "AWS::AutoScaling::LaunchConfiguration"
		}
	}
}`)
