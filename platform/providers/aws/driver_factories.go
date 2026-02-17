//go:build aws

package aws

import (
	"github.com/GoCodeAlone/workflow/platform"
	"github.com/GoCodeAlone/workflow/platform/providers/aws/drivers"
)

// Driver factory functions bridge between the provider (aws package) and the
// drivers sub-package so that registerDrivers can create all drivers from a
// single AWS config.

func NewEKSClusterDriver(cfg awsSDKConfig) platform.ResourceDriver {
	return drivers.NewEKSClusterDriver(cfg)
}

func NewEKSNodeGroupDriver(cfg awsSDKConfig) platform.ResourceDriver {
	return drivers.NewEKSNodeGroupDriver(cfg)
}

func NewVPCDriver(cfg awsSDKConfig) platform.ResourceDriver {
	return drivers.NewVPCDriver(cfg)
}

func NewRDSDriver(cfg awsSDKConfig) platform.ResourceDriver {
	return drivers.NewRDSDriver(cfg)
}

func NewSQSDriver(cfg awsSDKConfig) platform.ResourceDriver {
	return drivers.NewSQSDriver(cfg)
}

func NewIAMDriver(cfg awsSDKConfig) platform.ResourceDriver {
	return drivers.NewIAMDriver(cfg)
}

func NewALBDriver(cfg awsSDKConfig) platform.ResourceDriver {
	return drivers.NewALBDriver(cfg)
}
