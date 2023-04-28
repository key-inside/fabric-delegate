package chpvdr

import (
	reqContext "context"

	"github.com/hyperledger/fabric-sdk-go/pkg/common/options"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"

	"github.com/hyperledger/fabric-sdk-go/pkg/fabsdk/provider/chpvdr"

	delegate "github.com/key-inside/fabric-delegate/pkg/fab/channel"
)

type ChannelProvider struct {
	*chpvdr.ChannelProvider
}

func New(config fab.EndpointConfig, opts ...options.Opt) (*ChannelProvider, error) {
	cp, _ := chpvdr.New(config, opts...) // never returns error
	return &ChannelProvider{cp}, nil
}

func (cp *ChannelProvider) ChannelService(ctx fab.ClientContext, channelID string) (fab.ChannelService, error) {
	cs, err := cp.ChannelProvider.ChannelService(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return &ChannelService{cs}, nil
}

type ChannelService struct {
	fab.ChannelService
}

func (cs *ChannelService) Transactor(reqCtx reqContext.Context) (fab.Transactor, error) {
	cfg, err := cs.ChannelConfig()
	if err != nil {
		return nil, err
	}
	return delegate.NewTransactor(reqCtx, cfg)
}
