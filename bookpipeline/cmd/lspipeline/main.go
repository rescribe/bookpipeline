package main

import (
	"errors"
	"flag"
	"fmt"
	"log"

	// TODO: abstract out the aws stuff into aws.go in due course
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	//"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sqs"
)

const usage = `Usage: lspipeline

Lists useful things related to the pipeline.

- Instances running
- Messages in each queue
- Books not completed (from S3 without a best file)
- Books completed (from S3 with a best file)
- Last 5 lines of bookpipeline logs from each running instance (with -v)
`

type instanceDetails struct {
	id, name, ip, spot, iType, state, launchTime string
}

type queueDetails struct {
	name, numAvailable, numInProgress string
}

func ec2getInstances(svc *ec2.EC2, instances chan instanceDetails) {
	err := svc.DescribeInstancesPages(&ec2.DescribeInstancesInput{}, parseInstances(instances))
	if err != nil {
		close(instances)
		log.Println("Error with ec2 DescribeInstancePages call:", err)
	}
}

func parseInstances(details chan instanceDetails) (func(*ec2.DescribeInstancesOutput, bool) bool) {
	return func(page *ec2.DescribeInstancesOutput, lastPage bool) bool {
		for _, r := range page.Reservations {
			for _, i := range r.Instances {
				var d instanceDetails

				for _, t := range i.Tags {
					if *t.Key == "Name" {
						d.name = *t.Value
					}
				}
				if i.PublicIpAddress != nil {
					d.ip = *i.PublicIpAddress
				}
				if i.SpotInstanceRequestId != nil {
					d.spot = *i.SpotInstanceRequestId
				}
				d.iType = *i.InstanceType
				d.id = *i.InstanceId
				d.launchTime = i.LaunchTime.String()
				d.state = *i.State.Name

				details <- d
			}
		}
		if lastPage {
			close(details)
		}
		return !lastPage
	}
}

func ec2GetQueueDetails(svc *sqs.SQS, qdetails chan queueDetails, qnames []string) {
	for _, q := range qnames {
		avail, inprog, err := getQueueDetails(q, svc)
		if err != nil {
			log.Println("Error getting queue details:", err)
		}
		var qd queueDetails
		qd.name = q
		qd.numAvailable = avail
		qd.numInProgress = inprog
		qdetails <- qd
	}
	close(qdetails)
}

func getQueueDetails(qname string, svc *sqs.SQS) (string, string, error) {
	result, err := svc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: aws.String(qname),
	})
	if err != nil {
		return "", "", err
	}
	prequrl := *result.QueueUrl

	numAvailable := "ApproximateNumberOfMessages"
	numInProgress := "ApproximateNumberOfMessagesNotVisible"
	attrs, err := svc.GetQueueAttributes(&sqs.GetQueueAttributesInput{
		AttributeNames: []*string{&numAvailable, &numInProgress},
		QueueUrl: &prequrl,
	})
	if err != nil {
		return "", "", errors.New(fmt.Sprintf("Failed to get queue attributes: %s", err))
	}
	return *attrs.Attributes[numAvailable], *attrs.Attributes[numInProgress], nil
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
	sqssvc := sqs.New(sess)

	instances := make(chan instanceDetails, 100)
	queues := make(chan queueDetails)

	go ec2getInstances(ec2svc, instances)
	go ec2GetQueueDetails(sqssvc, queues, []string{"rescribepreprocess", "rescribeocr", "rescribeanalyse"})

	fmt.Println("# Instances")
	for i := range instances {
		fmt.Printf("ID: %s, Type: %s, LaunchTime: %s, State: %s", i.id, i.iType, i.launchTime, i.state)
		if i.name != "" {
			fmt.Printf(", Name: %s", i.name)
		}
		if i.ip != "" {
			fmt.Printf(", IP: %s", i.ip)
		}
		if i.spot != "" {
			fmt.Printf(", SpotRequest: %s", i.spot)
		}
		fmt.Printf("\n")
	}

	fmt.Println("\n# Queues")
	for i := range queues {
		fmt.Printf("%s: %s available, %s in progress\n", i.name, i.numAvailable, i.numInProgress)
	}

	// TODO: See remaining items in the usage statement
}
