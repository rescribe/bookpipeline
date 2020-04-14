// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package bookpipeline

// This file contains various cloud account specific stuff; change this if
// you want to use the cloud functionality on your own site.

// Spot instance details.
// This is only needed if you want to start spot instances with the
// spotme command, to start up preconfigured virtual servers running
// bookpipeline.
// The profile needs to allow permissions to the below S3 buckets and
// SQS queues, the Sg (security group) doesn't need any permissions,
// beyond SSH if you like, and the image should have bookpipeline
// installed and ideally auto-updating.
// TODO: release ansible repository which creates AMI.
// TODO: create profile and security group with mkpipeline
const (
	spotProfile = "arn:aws:iam::557852942063:instance-profile/pipeliner"
	spotImage   = "ami-0bc6ef6900f6da5d3"
	spotType    = "m5.large"
	spotSg      = "sg-0be8a3ab89e7136b9"
)

// Queue names. Can be anything unique in SQS.
const (
	queuePreProc  = "rescribepreprocess"
	queueWipeOnly = "rescribewipeonly"
	queueOcrPage  = "rescribeocrpage"
	queueAnalyse  = "rescribeanalyse"
)

// Storage bucket names. Can be anything unique in S3.
const (
	storageWip = "rescribeinprogress"
)
