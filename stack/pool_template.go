package stack

/* fields that _must_ be written out to this template
Resources.asg_
         .asg_.Properties.LaunchConfigurationName {"Ref": }
         .asg_.Properties.LoadBalancerNames [{"Ref": }]
		 .asg_.Properties.MinSize
		 .asg_.Properties.MaxSize
		 .asg_.Properties.DesiredCapacity
         .asg_.Tags // must have PropagateAtLaunch
         .asg_.VPCZoneIdentifier = [SubnetIds,]

		 .elb_
         .elb_.Properties.HealthCheck.Target
		 .elb_.Properties.SecurityGroups = [webSG, defaultSG]
		 .elb_.Properties.Subnets [SubnetIds,]
		 .elb_.Propertied.Listeners

		 .lc_
         .lc_.Properties.ImageId
		 .lc_.Properties.InstanceType
		 .lc_.Properties.KeyName
		 .lc_.Properties.SecurityGroups [defaultSG, sshSG]


This JSON document lays out the basic structure of the CloudFormation template
for our pool stacks. "Resources" needs to be replaced, and populated with
correctly named asg_, elb_, and lc_ entries.
*/
var pool_template = []byte(`
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
                "DesiredCapacity": "",
                "HealthCheckGracePeriod": "300",
                "HealthCheckType": "EC2",
                "LaunchConfigurationName": "",
				"MaxSize": "",
				"MinSize": "",
				"Tags": [],
				"VPCZoneIdentifier": []
            },
			"Type": "AWS::AutoScaling::AutoScalingGroup",
            "UpdatePolicy" : {
                "AutoScalingRollingUpdate" : {
                    "MinInstancesInService" : "1",
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
							"VolumeSize": 8
						}
					}
				],
				"ImageId": {},
				"InstanceType": {},
				"KeyName": {},
				"SecurityGroups": []
			},
			"Type": "AWS::AutoScaling::LaunchConfiguration"
		}
	}
}`)
