// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

package bookpipeline

// This file contains various cloud account specific stuff; change this if
// you want to use the cloud functionality on your own site.

// Spot instance details
const (
	spotProfile = "arn:aws:iam::557852942063:instance-profile/pipeliner"
	spotImage   = "ami-0bc6ef6900f6da5d3"
	spotType    = "m5.large"
	spotSg      = "sg-0be8a3ab89e7136b9"
)

// Queue names
const (
	queuePreProc  = "rescribepreprocess"
	queueWipeOnly = "rescribewipeonly"
	queueOcr      = "rescribeocr"
	queueOcrPage  = "rescribeocrpage"
	queueAnalyse  = "rescribeanalyse"
)

// Storage bucket names
const (
	storageWip = "rescribeinprogress"
)
