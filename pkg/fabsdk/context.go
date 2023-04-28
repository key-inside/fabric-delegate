package fabsdk

import (
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/msp"
	"github.com/hyperledger/fabric-sdk-go/pkg/fabsdk"
)

type ContextOption func() fabsdk.ContextOption

func WithUser(username string) ContextOption {
	return func() fabsdk.ContextOption {
		return fabsdk.WithUser(username)
	}
}

func WithIdentity(signingIdentity msp.SigningIdentity) ContextOption {
	return func() fabsdk.ContextOption {
		return fabsdk.WithIdentity(signingIdentity)
	}
}

func WithOrg(org string) ContextOption {
	return func() fabsdk.ContextOption {
		return fabsdk.WithOrg(org)
	}
}
