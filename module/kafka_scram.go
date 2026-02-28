package module

import (
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"github.com/xdg-go/scram"
)

// SHA256 and SHA512 are hash generator functions used by the SCRAM client.
var (
	SHA256 scram.HashGeneratorFcn = sha256.New
	SHA512 scram.HashGeneratorFcn = sha512.New
)

// xDGSCRAMClient implements sarama.SCRAMClient using the xdg-go/scram package.
type xDGSCRAMClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (x *xDGSCRAMClient) Begin(userName, password, authzID string) error {
	client, err := x.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.Client = client
	x.ClientConversation = client.NewConversation()
	return nil
}

func (x *xDGSCRAMClient) Step(challenge string) (string, error) {
	return x.ClientConversation.Step(challenge)
}

func (x *xDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}

// ensure hash functions satisfy the interface (compile-time check)
var _ func() hash.Hash = sha256.New
var _ func() hash.Hash = sha512.New
