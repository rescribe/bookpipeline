package main

import (
	"flag"
	"fmt"
	"log"

	// TODO: abstract out the aws stuff into aws.go in due course
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	//"github.com/aws/aws-sdk-go/service/s3"
	//"github.com/aws/aws-sdk-go/service/sqs"
)

const usage = `Usage: lspipeline

Lists useful things related to the pipeline.

- Instances running
- Messages in each queue (ApproximateNumberOfMessages and ApproximateNumberOfMessagesNotVisible from GetQueueAttributes)
- Books not completed (from S3 without a best file)
- Books completed (from S3 with a best file)
- Last 5 lines of bookpipeline logs from each running instance (with -v)
`

func printInstances(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
	for _, r := range page.Reservations {
		for _, i := range r.Instances {
			var ip, name, spot string
			for _, t := range i.Tags {
				if *t.Key == "Name" {
					name = *t.Value
				}
			}
			if i.PublicIpAddress != nil {
				ip = *i.PublicIpAddress
			}
			if i.SpotInstanceRequestId != nil {
				spot = *i.SpotInstanceRequestId
			}
			fmt.Printf("Type: %s", *i.InstanceType)
			if name != "" {
				fmt.Printf(", Name: %s", name)
			}
			fmt.Printf(", LaunchTime: %s, State: %s", i.LaunchTime, *i.State.Name)
			if ip != "" {
				fmt.Printf(", IP: %s", ip)
			}
			if spot != "" {
				fmt.Printf(", SpotRequest: %s", spot)
			}
			fmt.Printf("\n")
		}
	}

	return !lastPage
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("eu-west-2"),
	})
	if err != nil {
		log.Fatalln("Failed to set up aws session", err)
	}
	ec2svc := ec2.New(sess)
	//s3svc := s3.New(sess)
	//sqssvc := sqs.New(sess)

	err = ec2svc.DescribeInstancesPages(&ec2.DescribeInstancesInput{}, printInstances)
	if err != nil {
		log.Fatalln("Failed to get ec2 instances", err)
	}

	// TODO: See remaining items in the usage statement
}
