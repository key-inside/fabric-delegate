package channel

import (
	reqContext "context"
	"crypto/sha256"
	"fmt"
	"hash"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel/invoke"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/common/discovery/greylist"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/common/filter"
	selectopts "github.com/hyperledger/fabric-sdk-go/pkg/client/common/selection/options"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/retry"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/status"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/context"
	contextApi "github.com/hyperledger/fabric-sdk-go/pkg/common/providers/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/core"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/msp"
	contextImpl "github.com/hyperledger/fabric-sdk-go/pkg/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/fab/txn"

	delegateInvoke "github.com/key-inside/fabric-delegate/pkg/client/channel/invoke"
	delegate "github.com/key-inside/fabric-delegate/pkg/fab/channel"
)

type Client struct {
	*channel.Client

	context      context.Channel
	membership   fab.ChannelMembership
	eventService fab.EventService
	greylist     *greylist.Filter
}

// do not support channel.ClientOption
func New(channelProvider context.ChannelProvider) (*Client, error) {
	cc, err := channel.New(channelProvider)
	if err != nil {
		return nil, err
	}
	// no errors proved above
	channelContext, _ := channelProvider()
	membership, _ := channelContext.ChannelService().Membership()
	eventService, _ := channelContext.ChannelService().EventService()
	greylistProvider := greylist.New(channelContext.EndpointConfig().Timeout(fab.DiscoveryGreylistExpiry))

	return &Client{
		Client:       cc,
		context:      channelContext,
		membership:   membership,
		eventService: eventService,
		greylist:     greylistProvider,
	}, nil
}

// Sign signs the data with the private key.
func (cc *Client) Sign(data []byte) ([]byte, error) {
	signingMgr := cc.context.SigningManager()
	signature, err := signingMgr.Sign(data, cc.context.PrivateKey())
	if err != nil {
		return nil, fmt.Errorf("signing of payload failed: %w", err)
	}
	return signature, nil
}

// CreateSignedProposal create a proposal and sign it
//
//	Parameters:
//	request holds info about mandatory chaincode ID and function
//
//	Returns:
//	the signed proposal
func (cc *Client) CreateSignedProposal(request Request) (*pb.SignedProposal, error) {
	if request.ChaincodeID == "" || request.Fcn == "" {
		return nil, fmt.Errorf("ChaincodeID and Fcn are required")
	}

	// tx header
	txh, err := txn.NewHeader(cc.context, cc.context.ChannelID())
	if err != nil {
		return nil, fmt.Errorf("creating transaction header failed: %w", err)
	}

	// proposal
	proposal, err := txn.CreateChaincodeInvokeProposal(txh, fab.ChaincodeInvokeRequest{
		ChaincodeID:  request.ChaincodeID,
		Fcn:          request.Fcn,
		Args:         request.Args,
		TransientMap: request.TransientMap,
	})
	if err != nil {
		return nil, fmt.Errorf("creating transaction proposal failed: %w", err)
	}

	proposalBytes, err := proto.Marshal(proposal.Proposal)
	if err != nil {
		return nil, fmt.Errorf("marshal proposal failed: %w", err)
	}
	// sign the proposal
	signature, err := cc.Sign(proposalBytes)
	if err != nil {
		return nil, fmt.Errorf("sign proposal failed: %w", err)
	}

	return &pb.SignedProposal{
		ProposalBytes: proposalBytes,
		Signature:     signature,
	}, nil
}

type dummyCtx struct {
	contextApi.Client
	si msp.SigningIdentity
}

func (c dummyCtx) Serialize() ([]byte, error) {
	return c.si.Serialize()
}

func (dummyCtx) CryptoSuite() core.CryptoSuite {
	return cryptoSuite{}
}

type cryptoSuite struct {
	core.CryptoSuite
}

func (cryptoSuite) GetHash(opts core.HashOpts) (h hash.Hash, err error) {
	return sha256.New(), nil
}

func CreateSignedProposal(si msp.SigningIdentity, channelID string, request Request) (*pb.SignedProposal, error) {
	if request.ChaincodeID == "" || request.Fcn == "" {
		return nil, fmt.Errorf("ChaincodeID and Fcn are required")
	}

	// tx header
	txh, err := txn.NewHeader(dummyCtx{si: si}, channelID)
	if err != nil {
		return nil, fmt.Errorf("creating transaction header failed: %w", err)
	}

	// proposal
	proposal, err := txn.CreateChaincodeInvokeProposal(txh, fab.ChaincodeInvokeRequest{
		ChaincodeID:  request.ChaincodeID,
		Fcn:          request.Fcn,
		Args:         request.Args,
		TransientMap: request.TransientMap,
	})
	if err != nil {
		return nil, fmt.Errorf("creating transaction proposal failed: %w", err)
	}

	proposalBytes, err := proto.Marshal(proposal.Proposal)
	if err != nil {
		return nil, fmt.Errorf("marshal proposal failed: %w", err)
	}

	signature, err := si.Sign(proposalBytes)
	if err != nil {
		return nil, fmt.Errorf("sign proposal failed: %w", err)
	}

	return &pb.SignedProposal{
		ProposalBytes: proposalBytes,
		Signature:     signature,
	}, nil
}

// DelegateQuery send a signed proposal
//
//	Parameters:
//	proposal is a signed proposal
//	options holds optional request options
//
//	Returns:
//	the proposal responses from peer(s)
func (cc *Client) DelegateQuery(signedProposal *pb.SignedProposal, options ...DelegateRequestOption) (Response, error) {
	proposal, err := getProposalFromProposalBytes(signedProposal.ProposalBytes)
	if err != nil {
		return Response{}, err
	}
	chh, err := getChannelHeaderFromHeaderBytes(proposal.Header)
	if err != nil {
		return Response{}, err
	}
	cid, err := getChaincodeIDFromExtension(chh.Extension)
	if err != nil {
		return Response{}, err
	}

	request := channel.Request{
		ChaincodeID: cid, // for discovery endorser(peer) has the chaincode
		Fcn:         "_", // dummy
	}

	handler := delegateInvoke.NewDelegateQueryHandler(signedProposal)

	options = append(options, addDefaultTimeout(fab.Query))
	options = append(options, addDefaultTargetFilter(cc.context, filter.ChaincodeQuery))
	res, err := cc.DelegateInvokeHandler(handler, request, options...)
	if err != nil {
		return Response{}, err
	}
	return Response(res), nil
}

// DelegateEndorseAndCreateTxnPayload selects the peers along the invocation chain and sends the proposal. The txnPayload is then created
// with the responses.
//
//	Parameters:
//	proposal is a signed proposal
//	options holds optional request options
//
//	Returns:
//	the proposal responses from peer(s) with the payload
func (cc *Client) DelegateEndorseAndCreateTxnPayload(signedProposal *pb.SignedProposal, options ...DelegateRequestOption) (*common.Payload, Response, error) {
	proposal, err := getProposalFromProposalBytes(signedProposal.ProposalBytes)
	if err != nil {
		return nil, Response{}, err
	}
	chh, err := getChannelHeaderFromHeaderBytes(proposal.Header)
	if err != nil {
		return nil, Response{}, err
	}
	cid, err := getChaincodeIDFromExtension(chh.Extension)
	if err != nil {
		return nil, Response{}, err
	}

	request := channel.Request{
		ChaincodeID: cid, // for discovery endorser(peer) has the chaincode
		Fcn:         "_", // dummy
	}

	handler := delegateInvoke.NewDelegateSelectAndEndorseHandler(signedProposal)

	options = append(options, addDefaultTimeout(fab.Query))
	options = append(options, addDefaultTargetFilter(cc.context, filter.EndorsingPeer))
	res, err := cc.DelegateInvokeHandler(handler, request, options...)
	if err != nil {
		return nil, Response(res), fmt.Errorf("failed to invoke handlers: %w", err)
	}

	txnPayload, err := createTxnPayload(chh.TxId, proposal, res.Responses)
	if err != nil {
		return nil, Response(res), fmt.Errorf("failed to create txn payload with endorsement results: %w", err)
	}

	return txnPayload, Response(res), nil
}

// DelegateInvokeHandler invokes handler using request and optional request options provided
//
//	Parameters:
//	handler to be invoked
//	request holds info about mandatory chaincode ID and function
//	options holds optional request options
//
//	Returns:
//	the proposal responses from peer(s)
func (cc *Client) DelegateInvokeHandler(handler invoke.Handler, request channel.Request, options ...DelegateRequestOption) (Response, error) {
	//Read execute tx options
	txnOpts, err := cc.prepareOptsFromOptions(cc.context, options...)
	if err != nil {
		return Response{}, err
	}

	reqCtx, cancel := cc.createReqContext(&txnOpts)
	defer cancel()

	//Prepare context objects for handler
	requestContext, clientContext, err := cc.prepareHandlerContexts(reqCtx, request, txnOpts)
	if err != nil {
		return Response{}, err
	}

	invoker := retry.NewInvoker(
		requestContext.RetryHandler,
		retry.WithBeforeRetry(
			func(err error) {
				if requestContext.Opts.BeforeRetry != nil {
					requestContext.Opts.BeforeRetry(err)
				}

				cc.greylist.Greylist(err)

				// Reset context parameters
				requestContext.Opts.Targets = txnOpts.Targets
				requestContext.Error = nil
				requestContext.Response = invoke.Response{}
			},
		),
	)

	complete := make(chan bool, 1)
	go func() {
		_, _ = invoker.Invoke( // nolint: gas
			func() (interface{}, error) {
				handler.Handle(requestContext, clientContext)
				return nil, requestContext.Error
			})
		complete <- true
	}()
	select {
	case <-complete:
		return Response(requestContext.Response), requestContext.Error
	case <-reqCtx.Done():
		return Response{}, status.New(status.ClientStatus, status.Timeout.ToInt32(),
			"request timed out or been cancelled", nil)
	}
}

func getProposalFromProposalBytes(proposalBytes []byte) (*pb.Proposal, error) {
	proposal := &pb.Proposal{}
	if err := proto.Unmarshal(proposalBytes, proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

func getChannelHeaderFromHeaderBytes(headerBytes []byte) (*common.ChannelHeader, error) {
	h := common.Header{}
	if err := proto.Unmarshal(headerBytes, &h); err != nil {
		return nil, err
	}
	chh := &common.ChannelHeader{}
	if err := proto.Unmarshal(h.ChannelHeader, chh); err != nil {
		return nil, err
	}
	if chh.ChannelId == "" {
		return nil, fmt.Errorf("missing channel id")
	}
	return chh, nil
}

func getChaincodeIDFromExtension(extension []byte) (string, error) {
	ext := pb.ChaincodeHeaderExtension{}
	if err := proto.Unmarshal(extension, &ext); err != nil {
		return "", err
	}
	return ext.ChaincodeId.Name, nil
}

func createTxnPayload(txnID string, proposal *pb.Proposal, responses []*fab.TransactionProposalResponse) (*common.Payload, error) {
	txnProposal := fab.TransactionProposal{
		TxnID:    fab.TransactionID(txnID),
		Proposal: proposal,
	}
	txnRequest := fab.TransactionRequest{
		Proposal:          &txnProposal,
		ProposalResponses: responses,
	}

	tx, err := txn.New(txnRequest)
	if err != nil {
		return nil, err
	}

	hdr := &common.Header{}
	err = proto.Unmarshal(proposal.Header, hdr)
	if err != nil {
		return nil, err
	}

	txBytes, err := proto.Marshal(tx.Transaction)
	if err != nil {
		return nil, err
	}

	return &common.Payload{
		Header: hdr,
		Data:   txBytes,
	}, nil
}

// SignTxnPayload signs the transaction payload creating an signed envelope
//
//	Parameters:
//	payload transaction payload
//
//	Returns:
//	the signed envelope
func (cc *Client) SignTxnPayload(payload *common.Payload) (*fab.SignedEnvelope, error) {
	payloadBytes, err := proto.Marshal(payload)
	if err != nil {
		return nil, err
	}

	signature, err := cc.Sign(payloadBytes)
	if err != nil {
		return nil, err
	}

	return &fab.SignedEnvelope{
		Payload:   payloadBytes,
		Signature: signature,
	}, nil
}

func getTxnIDFromPayloadBytes(payloadBytes []byte) (string, error) {
	txnPayload := &common.Payload{}
	err := proto.Unmarshal(payloadBytes, txnPayload)
	if err != nil {
		return "", err
	}

	channelHeader := &common.ChannelHeader{}
	err = proto.Unmarshal(txnPayload.Header.ChannelHeader, channelHeader)
	if err != nil {
		return "", err
	}

	if channelHeader.ChannelId == "" {
		return "", fmt.Errorf("missing channel id")
	}
	return channelHeader.TxId, nil
}

// BroadcastEnvelope sends the signed envelope to the orderers
//
//	 Parameters:
//		envelope holds the transaction payload and the signature
//	 options holds optional request options
//
//	 Returns:
//	 the transaction result
func (cc *Client) BroadcastEnvelope(envelope *common.Envelope, options ...DelegateRequestOption) (Response, error) {
	txnID, err := getTxnIDFromPayloadBytes(envelope.Payload)
	if err != nil {
		return Response{}, fmt.Errorf("failed to get txnID from envelope: %w", err)
	}

	options = append(options, addDefaultTimeout(fab.Execute))
	options = append(options, addDefaultTargetFilter(cc.context, filter.EndorsingPeer))

	// reads execute tx options
	txnOpts, err := cc.prepareOptsFromOptions(cc.context, options...)
	if err != nil {
		return Response{}, err
	}

	// prepares request context
	reqCtx, cancel := cc.createReqContext(&txnOpts)
	defer cancel()

	// prepares delegate transactor
	t, err := cc.context.ChannelService().Transactor(reqCtx)
	if err != nil {
		return Response{}, fmt.Errorf("preparation of channel config failed: %w", err)
	}
	delegator, ok := t.(*delegate.Transactor)
	if !ok {
		return Response{}, fmt.Errorf("not delegate transactor")
	}

	// registers tx event
	reg, statusNotifier, err := cc.eventService.RegisterTxStatusEvent(string(txnID))
	if err != nil {
		return Response{}, fmt.Errorf("failed to register tx event: %w", err)
	}
	defer cc.eventService.Unregister(reg)

	// broadcast
	_, err = delegator.BroadcastEnvelope(&fab.SignedEnvelope{
		Payload:   envelope.Payload,
		Signature: envelope.Signature,
	})
	if err != nil {
		return Response{}, fmt.Errorf("failed to broadcast signed envelope: %w", err)
	}

	var res = channel.Response{}
	select {
	case txStatus := <-statusNotifier:
		res.TxValidationCode = txStatus.TxValidationCode

		if txStatus.TxValidationCode != pb.TxValidationCode_VALID {
			return Response(res), status.New(status.EventServerStatus, int32(txStatus.TxValidationCode),
				"received invalid transaction", nil)
		}
	case <-reqCtx.Done():
		return Response(res), status.New(status.ClientStatus, status.Timeout.ToInt32(),
			"Execute didn't receive block event", nil)
	}

	return Response(res), nil
}

func addDefaultTimeout(tt fab.TimeoutType) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		if o.Timeouts[tt] == 0 {
			return WithTimeout(tt, ctx.EndpointConfig().Timeout(tt))(ctx, o)
		}
		return nil
	}
}

func addDefaultTargetFilter(chCtx context.Channel, ft filter.EndpointType) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		if len(o.Targets) == 0 && o.TargetFilter == nil {
			return WithTargetFilter(filter.NewEndpointFilter(chCtx, ft))(ctx, o)
		}
		return nil
	}
}

func (cc *Client) createReqContext(txnOpts *invoke.Opts) (reqContext.Context, reqContext.CancelFunc) {
	if txnOpts.Timeouts == nil {
		txnOpts.Timeouts = make(map[fab.TimeoutType]time.Duration)
	}

	//setting default timeouts when not provided
	if txnOpts.Timeouts[fab.Execute] == 0 {
		txnOpts.Timeouts[fab.Execute] = cc.context.EndpointConfig().Timeout(fab.Execute)
	}

	reqCtx, cancel := contextImpl.NewRequest(cc.context, contextImpl.WithTimeout(txnOpts.Timeouts[fab.Execute]),
		contextImpl.WithParent(txnOpts.ParentContext))
	//Add timeout overrides here as a value so that it can be used by immediate child contexts (in handlers/transactors)
	reqCtx = reqContext.WithValue(reqCtx, contextImpl.ReqContextTimeoutOverrides, txnOpts.Timeouts)

	return reqCtx, cancel
}

func (cc *Client) prepareHandlerContexts(reqCtx reqContext.Context, request channel.Request, o invoke.Opts) (*invoke.RequestContext, *invoke.ClientContext, error) {
	if request.ChaincodeID == "" || request.Fcn == "" {
		return nil, nil, fmt.Errorf("ChaincodeID and Fcn are required")
	}

	transactor, err := cc.context.ChannelService().Transactor(reqCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create transactor: %w", err)
	}

	selection, err := cc.context.ChannelService().Selection()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create selection service: %w", err)
	}

	discovery, err := cc.context.ChannelService().Discovery()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create discovery service: %w", err)
	}

	peerFilter := func(peer fab.Peer) bool {
		if !cc.greylist.Accept(peer) {
			return false
		}
		if o.TargetFilter != nil && !o.TargetFilter.Accept(peer) {
			return false
		}
		return true
	}

	var peerSorter selectopts.PeerSorter
	if o.TargetSorter != nil {
		peerSorter = func(peers []fab.Peer) []fab.Peer {
			return o.TargetSorter.Sort(peers)
		}
	}

	clientContext := &invoke.ClientContext{
		Selection:    selection,
		Discovery:    discovery,
		Membership:   cc.membership,
		Transactor:   transactor,
		EventService: cc.eventService,
	}

	requestContext := &invoke.RequestContext{
		Request:         invoke.Request(request),
		Opts:            invoke.Opts(o),
		Response:        invoke.Response{},
		RetryHandler:    retry.New(o.Retry),
		Ctx:             reqCtx,
		SelectionFilter: peerFilter,
		PeerSorter:      peerSorter,
	}

	return requestContext, clientContext, nil
}

func (cc *Client) prepareOptsFromOptions(ctx context.Client, options ...DelegateRequestOption) (invoke.Opts, error) {
	txnOpts := invoke.Opts{}
	for _, option := range options {
		err := option(ctx, &txnOpts)
		if err != nil {
			return txnOpts, fmt.Errorf("failed to read opts: %w", err)
		}
	}
	return txnOpts, nil
}
