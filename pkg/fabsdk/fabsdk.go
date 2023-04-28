package fabsdk

import (
	contextApi "github.com/hyperledger/fabric-sdk-go/pkg/common/providers/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/core"
	"github.com/hyperledger/fabric-sdk-go/pkg/context"
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

// override
func (sdk *FabricSDK) ChannelContext(channelID string, options ...ContextOption) contextApi.ChannelProvider {
	opts := []fabsdk.ContextOption{}
	for _, opt := range options {
		opts = append(opts, opt())
	}
	return func() (contextApi.Channel, error) {
		clientCtxProvider := sdk.Context(opts...)
		return context.NewChannel(clientCtxProvider, channelID)
	}
}
