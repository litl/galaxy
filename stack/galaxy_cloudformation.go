package stack

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

func GalaxyTemplate(params *GalaxyTmplParams) ([]byte, error) {
	t := template.New("galaxy")

	funcMap := template.FuncMap{
		"SubnetRefList": params.SubnetRefList,
		"AZList":        params.AZList,
	}

	t.Funcs(funcMap)
	template.Must(t.Parse(galaxyTmpl))

	out := bytes.NewBuffer(nil)

	err := t.Execute(out, params)
	if err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

type GalaxyTmplParams struct {
	Name                   string
	VPCCIDR                string
	ControllerImageId      string
	ControllerInstanceType string
	PoolImageId            string
	PoolInstanceType       string
	KeyName                string
	Subnets                []*SubnetTmplParams
}

type SubnetTmplParams struct {
	Name   string
	Subnet string
	AZ     string
}

// Format the subnet names into a list of "Ref" intrinsics
func (p *GalaxyTmplParams) SubnetRefList() string {
	quoted := []string{}
	for _, s := range p.Subnets {
		quoted = append(quoted, fmt.Sprintf(`{"Ref": "%s"}`, s.Name))
	}
	return fmt.Sprintf("[%s]", strings.Join(quoted, ", "))
}

func (p *GalaxyTmplParams) AZList() string {
	azs := []string{}
	for _, s := range p.Subnets {
		azs = append(azs, fmt.Sprintf(`"%s"`, s.AZ))
	}
	return fmt.Sprintf("[%s]", strings.Join(azs, ", "))
}

// The base template for our Galaxy Cloudformation
var galaxyTmpl = `{{ $stackName := .Name }}{
    "AWSTemplateFormatVersion": "2010-09-09",
    "Description": "{{ $stackName }} CloudFormation",
    "Parameters": {
        "ControllerImageId": {
            "Default": "{{ .ControllerImageId }}",
            "Description": "{{ $stackName }} Controller AMI",
            "Type": "String"
        },
        "ControllerInstanceType": {
            "Default": "{{ .ControllerInstanceType }}",
            "Description": "LaunchConfig Instance Type",
            "Type": "String"
        },
        "PoolImageId": {
            "Default": "{{ .PoolImageId }}",
            "Description": "Default {{ $stackName }} pool AMI",
            "Type": "String"
        },
        "PoolInstanceType": {
            "Default": "{{ .PoolInstanceType }}",
            "Description": "Default {{ $stackName }} pool instance type",
            "Type": "String"
        },
        "KeyName": {
            "Default": "{{ .KeyName }}",
            "Description": "The name of an EC2 Key Pair to allow SSH access to the instance.",
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
                        "Value": "{{ $stackName }}-default"
                    }
                ],
                "VpcId": {
                    "Ref": "{{ $stackName }}VPC"
                }
            },
            "Type": "AWS::EC2::SecurityGroup"
        },
        "{{ $stackName }}ACLIn": {
            "Properties": {
                "CidrBlock": "0.0.0.0/0",
                "NetworkAclId": {
                    "Ref": "{{ $stackName }}BaseACL"
                },
                "Protocol": "-1",
                "RuleAction": "allow",
                "RuleNumber": "100"
            },
            "Type": "AWS::EC2::NetworkAclEntry"
        },
        "{{ $stackName }}ACLOut": {
            "Properties": {
                "CidrBlock": "0.0.0.0/0",
                "Egress": true,
                "NetworkAclId": {
                    "Ref": "{{ $stackName }}BaseACL"
                },
                "Protocol": "-1",
                "RuleAction": "allow",
                "RuleNumber": "100"
            },
            "Type": "AWS::EC2::NetworkAclEntry"
        },
{{ range .Subnets }}        "{{ .Name }}ACL": {
            "Properties": {
                "NetworkAclId": {
                    "Ref": "{{ $stackName }}BaseACL"
                },
                "SubnetId": {
                    "Ref": "{{ .Name }}"
                }
            },
            "Type": "AWS::EC2::SubnetNetworkAclAssociation"
        },
{{ end }}        "{{ $stackName }}BaseACL": {
            "Properties": {
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "{{ $stackName }}-base"
                    }
                ],
                "VpcId": {
                    "Ref": "{{ $stackName }}VPC"
                }
            },
            "Type": "AWS::EC2::NetworkAcl"
        },
        "{{ $stackName }}ControllerASG": {
            "Properties": {
                "AvailabilityZones": {{ AZList }},
                "Cooldown": "300",
                "DesiredCapacity": "1",
                "HealthCheckGracePeriod": "300",
                "HealthCheckType": "EC2",
                "LaunchConfigurationName": {
                    "Ref": "{{ $stackName }}ControllerLC"
                },
                "MaxSize": "1",
                "MinSize": "1",
                "Tags": [
                    {
                        "Key": "Name",
                        "PropagateAtLaunch": true,
                        "Value": "{{ $stackName }}-controller"
                    }
                ],
                "VPCZoneIdentifier": {{ SubnetRefList }}
            },
            "Type": "AWS::AutoScaling::AutoScalingGroup"
        },
        "{{ $stackName }}DHCP": {
            "Properties": {
                "DomainName": "ec2.internal",
                "DomainNameServers": [
                    "AmazonProvidedDNS"
                ]
            },
            "Type": "AWS::EC2::DHCPOptions"
        },
        "{{ $stackName }}DHCPAssoc": {
            "Properties": {
                "DhcpOptionsId": {
                    "Ref": "{{ $stackName }}DHCP"
                },
                "VpcId": {
                    "Ref": "{{ $stackName }}VPC"
                }
            },
            "Type": "AWS::EC2::VPCDHCPOptionsAssociation"
        },
        "{{ $stackName }}GatewayAttachment": {
            "Properties": {
                "InternetGatewayId": {
                    "Ref": "{{ $stackName }}VPCGateway"
                },
                "VpcId": {
                    "Ref": "{{ $stackName }}VPC"
                }
            },
            "Type": "AWS::EC2::VPCGatewayAttachment"
        },
        "{{ $stackName }}Ingress": {
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
        "{{ $stackName }}InstanceProfile": {
            "Properties": {
                "Path": "/",
                "Roles": [
                    {
                        "Ref": "{{ $stackName }}RootRole"
                    }
                ]
            },
            "Type": "AWS::IAM::InstanceProfile"
        },
        "{{ $stackName }}PoolLC": {
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
        "{{ $stackName }}ControllerLC": {
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
                    "Ref": "{{ $stackName }}InstanceProfile"
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
        "{{ $stackName }}RootRole": {
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
                                },
                                {
                                    "Effect": "Allow",
                                    "Action": [
                                        "iam:ListServerCertificates",
                                        "iam:ListInstanceProfiles",
                                        "iam:PassRole"
                                    ],
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
        "{{ $stackName }}Route": {
            "DependsOn": "{{ $stackName }}GatewayAttachment",
            "Properties": {
                "DestinationCidrBlock": "0.0.0.0/0",
                "GatewayId": {
                    "Ref": "{{ $stackName }}VPCGateway"
                },
                "RouteTableId": {
                    "Ref": "{{ $stackName }}RouteTable"
                }
            },
            "Type": "AWS::EC2::Route"
        },
{{ range .Subnets }}        "{{ .Name }}Route": {
            "Properties": {
                "RouteTableId": {
                    "Ref": "{{ $stackName }}RouteTable"
                },
                "SubnetId": {
                    "Ref": "{{ .Name }}"
                }
            },
            "Type": "AWS::EC2::SubnetRouteTableAssociation"
        },
{{ end }}        "{{ $stackName }}RouteTable": {
            "Properties": {
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "{{ $stackName }}-public"
                    }
                ],
                "VpcId": {
                    "Ref": "{{ $stackName }}VPC"
                }
            },
            "Type": "AWS::EC2::RouteTable"
        },
{{ range .Subnets }}        "{{ .Name }}": {
            "Properties": {
                "AvailabilityZone": "{{ .AZ }}",
                "CidrBlock": "{{ .Subnet }}",
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "{{ .Name }}-{{ .AZ }}"
                    },
                    {
                        "Key": "scope",
                        "Value": "public"
                    }
                ],
                "VpcId": {
                    "Ref": "{{ $stackName }}VPC"
                }
            },
            "Type": "AWS::EC2::Subnet"
        },
{{ end }}        "{{ $stackName }}VPC": {
            "Properties": {
                "CidrBlock": "{{ .VPCCIDR }}",
                "EnableDnsHostnames": "true",
                "EnableDnsSupport": "true",
                "InstanceTenancy": "default",
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "{{ $stackName }}"
                    }
                ]
            },
            "Type": "AWS::EC2::VPC"
        },
        "{{ $stackName }}VPCGateway": {
            "Properties": {
                "Tags": [
                    {
                        "Key": "Name",
                        "Value": "{{ $stackName }}-gateway"
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
                        "Value": "{{ $stackName }}-ssh"
                    }
                ],
                "VpcId": {
                    "Ref": "{{ $stackName }}VPC"
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
                        "Value": "{{ $stackName }}-web"
                    }
                ],
                "VpcId": {
                    "Ref": "{{ $stackName }}VPC"
                }
            },
            "Type": "AWS::EC2::SecurityGroup"
        }
    }
}`
