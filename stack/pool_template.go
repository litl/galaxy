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
                "AvailabilityZones": [],
                "Cooldown": "300",
				"DesiredCapacity": "1",
                "HealthCheckGracePeriod": "300",
                "HealthCheckType": "EC2",
                "LaunchConfigurationName": {},
				"MinSize": "1",
				"MaxSize": "2",
				"Tags": [],
				"VPCZoneIdentifier": []
            },
			"Type": "AWS::AutoScaling::AutoScalingGroup"
        },
		"elb_": {
			"Properties": {
				"CrossZone": true,
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

var scalingTemplate = []byte(`
{
    "ScaleDown": {
        "Properties": {
            "AdjustmentType": "ChangeInCapacity",
            "AutoScalingGroupName": {
                "Ref": "ASG"
            },
            "Cooldown": "300",
            "ScalingAdjustment": "-1"
        },
        "Type": "AWS::AutoScaling::ScalingPolicy"
    },
    "ScaleDownAlarm": {
        "Properties": {
            "ActionsEnabled": "true",
            "AlarmActions": [
                {
                    "Ref": "ScaleDown"
                }
            ],
            "ComparisonOperator": "LessThanThreshold",
            "Dimensions": [
                {
                    "Name": "AutoScalingGroupName",
                    "Value": {
                        "Ref": "ASG"
                    }
                }
            ],
            "EvaluationPeriods": "5",
            "MetricName": "CPUUtilization",
            "Namespace": "AWS/EC2",
            "Period": "60",
            "Statistic": "Average",
            "Threshold": "30.0"
        },
        "Type": "AWS::CloudWatch::Alarm"
    },
    "ScaleUp": {
        "Properties": {
            "AdjustmentType": "ChangeInCapacity",
            "AutoScalingGroupName": {
                "Ref": "ASG"
            },
            "Cooldown": "300",
            "ScalingAdjustment": "1"
        },
        "Type": "AWS::AutoScaling::ScalingPolicy"
    },
    "ScaleUpAlarm": {
        "Properties": {
            "ActionsEnabled": "true",
            "AlarmActions": [
                {
                    "Ref": "ScaleUp"
                }
            ],
            "ComparisonOperator": "GreaterThanThreshold",
            "Dimensions": [
                {
                    "Name": "AutoScalingGroupName",
                    "Value": {
                        "Ref": "ASG"
                    }
                }
            ],
            "EvaluationPeriods": "5",
            "MetricName": "CPUUtilization",
            "Namespace": "AWS/EC2",
            "Period": "60",
            "Statistic": "Average",
            "Threshold": "80.0"
        },
        "Type": "AWS::CloudWatch::Alarm"
    }
}`)
