package stack

//TODO: replace "galaxy" strings with actual stack name for easier cross-reference
// The base template for our Galaxy Cloudformation
var cloudformation_template = []byte(`{
    "AWSTemplateFormatVersion": "2010-09-09",
    "Description": "Galaxy CloudFormation",
    "Parameters": {
        "ControllerImageId": {
            "Description": "Galaxy Controller AMI",
            "Type": "String"
        },
        "ControllerInstanceType": {
            "Description": "LaunchConfig Instance Type",
            "Type": "String"
        },
        "PoolImageId": {
            "Description": "Default galaxy pool AMI",
            "Type": "String"
        },
        "PoolInstanceType": {
            "Description": "Default galaxy pool instance type",
            "Type": "String"
        },
        "KeyName": {
            "Description": "The name of an EC2 Key Pair to allow SSH access to the instance.",
            "Type": "String"
        },
        "SubnetCidrBlocks": {
            "Description": "Comma delimited list of Cidr blocks for subnets (3)",
            "Type": "CommaDelimitedList"
        },
        "VPCCidrBlock": {
            "Description": "Cidr Block for the VPC",
            "Type": "String"
        }
    },
    "Resources": {
        "defaultSG": {
            "Properties": {
                "GroupDescription": "default VPC security group",
                "SecurityGroupEgress": [
                    {
                        "CidrIp": "0.0.0.0/0",
                        "IpProtocol": "-1"
                    }
                ],
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy-default"
                    }
                ],
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::SecurityGroup"
        },
        "galaxyACLIn": {
            "Properties": {
                "CidrBlock": "0.0.0.0/0",
                "NetworkAclId": {
                    "Ref": "galaxyBaseACL"
                },
                "Protocol": "-1",
                "RuleAction": "allow",
                "RuleNumber": "100"
            },
            "Type": "AWS::EC2::NetworkAclEntry"
        },
        "galaxyACLOut": {
            "Properties": {
                "CidrBlock": "0.0.0.0/0",
                "Egress": true,
                "NetworkAclId": {
                    "Ref": "galaxyBaseACL"
                },
                "Protocol": "-1",
                "RuleAction": "allow",
                "RuleNumber": "100"
            },
            "Type": "AWS::EC2::NetworkAclEntry"
        },
        "galaxyACLSubnet1": {
            "Properties": {
                "NetworkAclId": {
                    "Ref": "galaxyBaseACL"
                },
                "SubnetId": {
                    "Ref": "galaxySubnet1"
                }
            },
            "Type": "AWS::EC2::SubnetNetworkAclAssociation"
        },
        "galaxyACLSubnet2": {
            "Properties": {
                "NetworkAclId": {
                    "Ref": "galaxyBaseACL"
                },
                "SubnetId": {
                    "Ref": "galaxySubnet2"
                }
            },
            "Type": "AWS::EC2::SubnetNetworkAclAssociation"
        },
        "galaxyACLSubnet3": {
            "Properties": {
                "NetworkAclId": {
                    "Ref": "galaxyBaseACL"
                },
                "SubnetId": {
                    "Ref": "galaxySubnet3"
                }
            },
            "Type": "AWS::EC2::SubnetNetworkAclAssociation"
        },
        "galaxyBaseACL": {
            "Properties": {
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy-base"
                    }
                ],
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::NetworkAcl"
        },
        "galaxyControllerASG": {
            "Properties": {
                "AvailabilityZones": {
                    "Fn::GetAZs": ""
                },
                "Cooldown": "300",
                "DesiredCapacity": "1",
                "HealthCheckGracePeriod": "300",
                "HealthCheckType": "EC2",
                "LaunchConfigurationName": {
                    "Ref": "galaxyControllerLC"
                },
                "MaxSize": "1",
                "MinSize": "1",
                "Tags": [
                    {
                        "Key": "Name",
                        "PropagateAtLaunch": true,
                        "Value": "galaxy-controller"
                    }
                ],
                "VPCZoneIdentifier": [
                    {
                        "Ref": "galaxySubnet1"
                    },
                    {
                        "Ref": "galaxySubnet2"
                    },
                    {
                        "Ref": "galaxySubnet3"
                    }
                ]
            },
            "Type": "AWS::AutoScaling::AutoScalingGroup"
        },
        "galaxyDHCP": {
            "Properties": {
                "DomainName": "ec2.internal",
                "DomainNameServers": [
                    "AmazonProvidedDNS"
                ]
            },
            "Type": "AWS::EC2::DHCPOptions"
        },
        "galaxyDHCPAssoc": {
            "Properties": {
                "DhcpOptionsId": {
                    "Ref": "galaxyDHCP"
                },
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::VPCDHCPOptionsAssociation"
        },
        "galaxyGatewayAttachment": {
            "Properties": {
                "InternetGatewayId": {
                    "Ref": "galaxyVPCGateway"
                },
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::VPCGatewayAttachment"
        },
        "galaxyIngress": {
            "Properties": {
                "GroupId": {
                    "Ref": "defaultSG"
                },
                "IpProtocol": "-1",
                "SourceSecurityGroupId": {
                    "Ref": "defaultSG"
                }
            },
            "Type": "AWS::EC2::SecurityGroupIngress"
        },
        "galaxyInstanceProfile": {
            "Properties": {
                "Path": "/",
                "Roles": [
                    {
                        "Ref": "galaxyRootRole"
                    }
                ]
            },
            "Type": "AWS::IAM::InstanceProfile"
        },
        "galaxyPoolLC": {
            "Properties": {
                "ImageId": {
                    "Ref": "ControllerImageId"
                },
                "InstanceType": {
                    "Ref": "ControllerInstanceType"
                },
                "KeyName": {
                    "Ref": "KeyName"
                }
            },
            "Type": "AWS::AutoScaling::LaunchConfiguration"
        },
        "galaxyControllerLC": {
            "Properties": {
                "AssociatePublicIpAddress": true,
                "BlockDeviceMappings": [
                    {
                        "DeviceName": "/dev/sda1",
                        "Ebs": {
                            "VolumeSize": 100,
							"VolumeType": "gp2"
                        }
                    }
                ],
                "IamInstanceProfile": {
                    "Ref": "galaxyInstanceProfile"
                },
                "ImageId": {
                    "Ref": "ControllerImageId"
                },
                "InstanceType": {
                    "Ref": "ControllerInstanceType"
                },
                "KeyName": {
                    "Ref": "KeyName"
                },
                "SecurityGroups": [
                    {
                        "Ref": "sshSG"
                    },
                    {
                        "Ref": "defaultSG"
                    }
                ]
            },
            "Type": "AWS::AutoScaling::LaunchConfiguration"
        },
        "galaxyRootRole": {
            "Properties": {
                "AssumeRolePolicyDocument": {
                    "Statement": [
                        {
                            "Action": [
                                "sts:AssumeRole"
                            ],
                            "Effect": "Allow",
                            "Principal": {
                                "Service": [
                                    "ec2.amazonaws.com"
                                ]
                            }
                        }
                    ],
                    "Version": "2012-10-17"
                },
                "Path": "/",
                "Policies": [
                    {
                        "PolicyDocument": {
                            "Statement": [
                                {
                                    "Effect": "Allow",
                                    "NotAction": "iam:*",
                                    "Resource": "*"
                                }
                            ],
                            "Version": "2012-10-17"
                        },
                        "PolicyName": "root"
                    }
                ]
            },
            "Type": "AWS::IAM::Role"
        },
        "galaxyRoute": {
            "DependsOn": "galaxyGatewayAttachment",
            "Properties": {
                "DestinationCidrBlock": "0.0.0.0/0",
                "GatewayId": {
                    "Ref": "galaxyVPCGateway"
                },
                "RouteTableId": {
                    "Ref": "galaxyRouteTable"
                }
            },
            "Type": "AWS::EC2::Route"
        },
        "galaxyRouteSubnet1": {
            "Properties": {
                "RouteTableId": {
                    "Ref": "galaxyRouteTable"
                },
                "SubnetId": {
                    "Ref": "galaxySubnet1"
                }
            },
            "Type": "AWS::EC2::SubnetRouteTableAssociation"
        },
        "galaxyRouteSubnet2": {
            "Properties": {
                "RouteTableId": {
                    "Ref": "galaxyRouteTable"
                },
                "SubnetId": {
                    "Ref": "galaxySubnet3"
                }
            },
            "Type": "AWS::EC2::SubnetRouteTableAssociation"
        },
        "galaxyRouteSubnet3": {
            "Properties": {
                "RouteTableId": {
                    "Ref": "galaxyRouteTable"
                },
                "SubnetId": {
                    "Ref": "galaxySubnet2"
                }
            },
            "Type": "AWS::EC2::SubnetRouteTableAssociation"
        },
        "galaxyRouteTable": {
            "Properties": {
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy-public"
                    }
                ],
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::RouteTable"
        },
        "galaxySubnet1": {
            "Properties": {
                "AvailabilityZone": "us-east-1d",
                "CidrBlock": {
                    "Fn::Select": [
                        "0",
                        {
                            "Ref": "SubnetCidrBlocks"
                        }
                    ]
                },
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy-us-east-1d"
                    },
                    {
                        "Key": "scope",
                        "Value": "public"
                    }
                ],
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::Subnet"
        },
        "galaxySubnet2": {
            "Properties": {
                "AvailabilityZone": "us-east-1c",
                "CidrBlock": {
                    "Fn::Select": [
                        "1",
                        {
                            "Ref": "SubnetCidrBlocks"
                        }
                    ]
                },
                "Tags": [
                    {
                        "Key": "scope",
                        "Value": "public"
                    },
                    {
                        "Key": "Name",
                        "Value": "galaxy-us-east-1c"
                    }
                ],
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::Subnet"
        },
        "galaxySubnet3": {
            "Properties": {
                "AvailabilityZone": "us-east-1b",
                "CidrBlock": {
                    "Fn::Select": [
                        "2",
                        {
                            "Ref": "SubnetCidrBlocks"
                        }
                    ]
                },
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy-us-east-1b"
                    },
                    {
                        "Key": "scope",
                        "Value": "public"
                    }
                ],
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::Subnet"
        },
        "galaxyVPC": {
            "Properties": {
                "CidrBlock": {
                    "Ref": "VPCCidrBlock"
                },
                "EnableDnsHostnames": "true",
                "EnableDnsSupport": "true",
                "InstanceTenancy": "default",
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy"
                    }
                ]
            },
            "Type": "AWS::EC2::VPC"
        },
        "galaxyVPCGateway": {
            "Properties": {
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy-gateway"
                    }
                ]
            },
            "Type": "AWS::EC2::InternetGateway"
        },
        "sshSG": {
            "Properties": {
                "GroupDescription": "public ssh",
                "SecurityGroupEgress": [
                    {
                        "CidrIp": "0.0.0.0/0",
                        "IpProtocol": "-1"
                    }
                ],
                "SecurityGroupIngress": [
                    {
                        "CidrIp": "0.0.0.0/0",
                        "FromPort": "22",
                        "IpProtocol": "tcp",
                        "ToPort": "22"
                    },
                    {
                        "CidrIp": "0.0.0.0/0",
                        "FromPort": "60000",
                        "IpProtocol": "udp",
                        "ToPort": "61000"
                    }
                ],
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy-ssh"
                    }
                ],
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::SecurityGroup"
        },
        "webSG": {
            "Properties": {
                "GroupDescription": "web",
                "SecurityGroupEgress": [
                    {
                        "CidrIp": "0.0.0.0/0",
                        "IpProtocol": "-1"
                    }
                ],
                "SecurityGroupIngress": [
                    {
                        "CidrIp": "0.0.0.0/0",
                        "FromPort": "80",
                        "IpProtocol": "tcp",
                        "ToPort": "80"
                    },
                    {
                        "CidrIp": "0.0.0.0/0",
                        "FromPort": "443",
                        "IpProtocol": "tcp",
                        "ToPort": "443"
                    }
                ],
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "galaxy-web"
                    }
                ],
                "VpcId": {
                    "Ref": "galaxyVPC"
                }
            },
            "Type": "AWS::EC2::SecurityGroup"
        }
    }
}`)
