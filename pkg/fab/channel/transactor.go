package channel

import (
	reqContext "context"
	"fmt"
	"strings"

	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	contextImpl "github.com/hyperledger/fabric-sdk-go/pkg/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/config/endpoint"
	"github.com/hyperledger/fabric-sdk-go/pkg/fab/txn"

	delegateTxn "github.com/key-inside/fabric-delegate/pkg/fab/txn"
)

type Transactor struct {
	ChannelID string

	reqCtx   reqContext.Context
	orderers []fab.Orderer
}

func NewTransactor(reqCtx reqContext.Context, cfg fab.ChannelCfg) (*Transactor, error) {
	ctx, ok := contextImpl.RequestClientContext(reqCtx)
	if !ok {
		return nil, fmt.Errorf("failed get client context from reqContext for create new transactor")
	}

	orderers, err := orderersFromChannelCfg(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("reading orderers from channel config failed: %w", err)
	}

	t := Transactor{
		ChannelID: cfg.ID(),
		reqCtx:    reqCtx,
		orderers:  orderers,
	}
	return &t, nil
}

func orderersFromChannelCfg(ctx context.Client, cfg fab.ChannelCfg) ([]fab.Orderer, error) {
	orderers, err := orderersFromChannel(ctx, cfg.ID())
	if err != nil {
		return nil, err
	}
	if len(orderers) > 0 {
		return orderers, nil
	}

	ordererDict := orderersByTarget(ctx)

	for _, target := range cfg.Orderers() {
		oCfg, ok := ordererDict[target]
		if !ok {
			matchingOrdererConfig, found, ignore := ctx.EndpointConfig().OrdererConfig(strings.ToLower(target))
			if ignore {
				continue
			}

			if found {
				oCfg = *matchingOrdererConfig
				ok = true
			}
		}

		if !ok {
			oCfg = fab.OrdererConfig{
				URL: target,
			}
		}

		o, err := ctx.InfraProvider().CreateOrdererFromConfig(&oCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create orderer from config: %w", err)
		}
		orderers = append(orderers, o)

	}
	return orderers, nil
}

func orderersFromChannel(ctx context.Client, channelID string) ([]fab.Orderer, error) {
	chNetworkConfig := ctx.EndpointConfig().ChannelConfig(channelID)
	orderers := []fab.Orderer{}
	for _, chOrderer := range chNetworkConfig.Orderers {
		ordererConfig, found, ignoreOrderer := ctx.EndpointConfig().OrdererConfig(chOrderer)
		if !found || ignoreOrderer {
			continue
		}
		orderer, err := ctx.InfraProvider().CreateOrdererFromConfig(ordererConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create orderer from config: %w", err)
		}

		orderers = append(orderers, orderer)
	}
	return orderers, nil
}

func orderersByTarget(ctx context.Client) map[string]fab.OrdererConfig {
	ordererDict := map[string]fab.OrdererConfig{}
	orderersConfig := ctx.EndpointConfig().OrderersConfig()

	for _, oc := range orderersConfig {
		address := endpoint.ToAddress(oc.URL)
		ordererDict[address] = oc
	}
	return ordererDict
}

func (t *Transactor) CreateTransactionHeader(opts ...fab.TxnHeaderOpt) (fab.TransactionHeader, error) {
	ctx, ok := contextImpl.RequestClientContext(t.reqCtx)
	if !ok {
		return nil, fmt.Errorf("failed get client context from reqContext for txn Header")
	}

	txh, err := txn.NewHeader(ctx, t.ChannelID, opts...)
	if err != nil {
		return nil, fmt.Errorf("new transaction ID failed: %w", err)
	}

	return txh, nil
}

func (t *Transactor) SendTransactionProposal(proposal *fab.TransactionProposal, targets []fab.ProposalProcessor) ([]*fab.TransactionProposalResponse, error) {
	ctx, ok := contextImpl.RequestClientContext(t.reqCtx)
	if !ok {
		return nil, fmt.Errorf("failed get client context from reqContext for SendTransactionProposal")
	}

	reqCtx, cancel := contextImpl.NewRequest(ctx, contextImpl.WithTimeoutType(fab.PeerResponse), contextImpl.WithParent(t.reqCtx))
	defer cancel()

	return txn.SendProposal(reqCtx, proposal, targets)
}

func (t *Transactor) CreateTransaction(request fab.TransactionRequest) (*fab.Transaction, error) {
	return txn.New(request)
}

func (t *Transactor) SendTransaction(tx *fab.Transaction) (*fab.TransactionResponse, error) {
	return txn.Send(t.reqCtx, tx, t.orderers)
}

func (t *Transactor) SendSignedProposal(proposal *pb.SignedProposal, targets []fab.ProposalProcessor) ([]*fab.TransactionProposalResponse, error) {
	ctx, ok := contextImpl.RequestClientContext(t.reqCtx)
	if !ok {
		return nil, fmt.Errorf("failed get client context from reqContext for SendSignedProposal")
	}

	reqCtx, cancel := contextImpl.NewRequest(ctx, contextImpl.WithTimeoutType(fab.PeerResponse), contextImpl.WithParent(t.reqCtx))
	defer cancel()

	return delegateTxn.SendSignedProposal(reqCtx, proposal, targets)
}

func (t *Transactor) BroadcastEnvelope(envelope *fab.SignedEnvelope) (*fab.TransactionResponse, error) {
	return delegateTxn.BroadcastEnvelope(t.reqCtx, envelope, t.orderers)
}
