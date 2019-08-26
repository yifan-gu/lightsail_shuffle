package main

import (
	"fmt"
	"io/ioutil"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lightsail"
	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

const (
	usage = "Usage: lightsail_shuffle [--instances=]"
)

var (
	flagInstances  string
	flagAWSProfile string
)

type Instance struct {
	Region string `json:"region"`
	Name   string `json:"name"`
}

func init() {
	pflag.StringVar(&flagInstances, "instances", "", "file of the instance list")
	pflag.StringVar(&flagAWSProfile, "aws-profile", "yifan", "the aws profile")
}

// return a map from instance-name -> staticip-name
func getStaticIpMap(svc *lightsail.Lightsail) (map[string]string, error) {
	out, err := svc.GetStaticIps(&lightsail.GetStaticIpsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get static ips: %v", err)
	}

	ret := make(map[string]string)

	for _, staticIp := range out.StaticIps {
		if staticIp.AttachedTo != nil {
			ret[*staticIp.AttachedTo] = *staticIp.Name
		}
	}

	return ret, nil
}

func reattachIp(svc *lightsail.Lightsail, name string, staticIps map[string]string) error {
	glog.Infof("Shuffle IP for %v", name)

	_, err := svc.GetInstance(&lightsail.GetInstanceInput{InstanceName: aws.String(name)})
	if err != nil {
		return fmt.Errorf("failed to get instance for %v: %v", name, err)
	}

	ipName, ok := staticIps[name]
	if !ok {
		glog.Infof("No static ip found for instance %v", name)
		return nil
	}

	glog.Infof("Detach static IP %q for %v", ipName, name)

	_, err = svc.DetachStaticIp(&lightsail.DetachStaticIpInput{StaticIpName: aws.String(ipName)})
	if err != nil {
		return fmt.Errorf("failed to detach ip %q for %v: %v", ipName, name, err)
	}

	glog.Infof("Release static IP %q for %v", ipName, name)

	_, err = svc.ReleaseStaticIp(&lightsail.ReleaseStaticIpInput{StaticIpName: aws.String(ipName)})
	if err != nil {
		return fmt.Errorf("failed to release ip %q for %v: %v", ipName, name, err)
	}

	glog.Infof("Allocate static IP %q for %v", ipName, name)

	_, err = svc.AllocateStaticIp(&lightsail.AllocateStaticIpInput{StaticIpName: aws.String(ipName)})
	if err != nil {
		return fmt.Errorf("failed to allocate ip %q for %v: %v", ipName, name, err)
	}

	glog.Infof("Attach static IP %q for %v", ipName, name)

	_, err = svc.AttachStaticIp(&lightsail.AttachStaticIpInput{InstanceName: aws.String(name), StaticIpName: aws.String(ipName)})
	if err != nil {
		return fmt.Errorf("failed to allocate ip %q for %v: %v", ipName, name, err)
	}

	gout, err := svc.GetInstance(&lightsail.GetInstanceInput{InstanceName: aws.String(name)})
	if err != nil {
		return fmt.Errorf("failed to get instance for %v: %v", name, err)
	}

	glog.Infof("New IP for %v is %q", name, *gout.Instance.PublicIpAddress)

	return nil
}

func shuffleIp(profile string, instance Instance) error {
	sess := session.Must(session.NewSession(&aws.Config{
		Region:      aws.String(instance.Region),
		Credentials: credentials.NewSharedCredentials("", profile),
	}))
	svc := lightsail.New(sess)

	staticIpMap, err := getStaticIpMap(svc)
	if err != nil {
		glog.Fatal(err)
	}

	if err := reattachIp(svc, instance.Name, staticIpMap); err != nil {
		glog.Error(err)
	}

	return nil
}

func main() {
	var instances []Instance

	pflag.Parse()

	data, err := ioutil.ReadFile(flagInstances)
	if err != nil {
		glog.Fatalf("Failed to read file %v: %v", flagInstances, err)
	}

	if err := yaml.Unmarshal(data, &instances); err != nil {
		glog.Fatalf("Failed to unmarshal instances list: %v", err)
	}

	for _, instance := range instances {
		if err := shuffleIp(flagAWSProfile, instance); err != nil {
			glog.Errorf("Failed to shuffle IP for %v: %v", instance.Name, err)
		}
	}

	glog.Infof("Shuffle completed")
}
