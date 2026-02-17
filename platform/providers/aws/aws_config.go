//go:build aws

package aws

import (
	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
)

// awsSDKConfig is an alias for the AWS SDK v2 config type used throughout
// the provider. This allows tests to construct configs with mock credentials.
type awsSDKConfig = awsv2.Config
