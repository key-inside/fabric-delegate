package fabsdk

import (
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/core"
	"github.com/hyperledger/fabric-sdk-go/pkg/fabsdk"

	"github.com/key-inside/fabric-delegate/pkg/fabsdk/factory/dlgsvc"
)

type FabricSDK struct {
	*fabsdk.FabricSDK
}

// New initializes the SDK with the delegate service provider factory
func New(configProvider core.ConfigProvider, opts ...fabsdk.Option) (*FabricSDK, error) {
	opts = append(opts, fabsdk.WithServicePkg(dlgsvc.NewProviderFactory()))
	sdk, err := fabsdk.New(configProvider, opts...)
	if err != nil {
		return nil, err
	}
	return &FabricSDK{sdk}, nil
}
