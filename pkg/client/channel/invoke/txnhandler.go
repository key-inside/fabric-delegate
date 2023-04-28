package invoke

import (
	"fmt"

	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel/invoke"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/status"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	"github.com/hyperledger/fabric-sdk-go/pkg/fab/peer"

	delegate "github.com/key-inside/fabric-delegate/pkg/fab/channel"
)

func getNext(next []invoke.Handler) invoke.Handler {
	if len(next) > 0 {
		return next[0]
	}
	return nil
}

func getDelegateTransactor(clientContext *invoke.ClientContext) (*delegate.Transactor, error) {
	t, ok := clientContext.Transactor.(*delegate.Transactor)
	if !ok {
		return nil, fmt.Errorf("no delegate transactor")
	}
	return t, nil
}

func newInvocationChain(requestContext *invoke.RequestContext) []*fab.ChaincodeCall {
	invocChain := []*fab.ChaincodeCall{{ID: requestContext.Request.ChaincodeID}}
	for _, ccCall := range requestContext.Request.InvocationChain {
		if ccCall.ID == invocChain[0].ID {
			invocChain[0].Collections = ccCall.Collections
		} else {
			invocChain = append(invocChain, ccCall)
		}
	}
	return invocChain
}

// DelegateEndorsementHandler for handling endorse a signed proposal
type DelegateEndorsementHandler struct {
	next           invoke.Handler
	signedProposal *pb.SignedProposal
}

// Handle for endorsing transactions
func (e *DelegateEndorsementHandler) Handle(requestContext *invoke.RequestContext, clientContext *invoke.ClientContext) {
	if len(requestContext.Opts.Targets) == 0 {
		requestContext.Error = status.New(status.ClientStatus, status.NoPeersFound.ToInt32(), "targets were not provided", nil)
		return
	}

	if nil == e.signedProposal {
		requestContext.Error = fmt.Errorf("a signed proposal was not provided")
		return
	}

	t, err := getDelegateTransactor(clientContext)
	if err != nil {
		requestContext.Error = err
		return
	}
	transactionProposalResponses, err := t.SendSignedProposal(
		e.signedProposal,
		peer.PeersToTxnProcessors(requestContext.Opts.Targets),
	)
	if err != nil {
		requestContext.Error = err
		return
	}

	requestContext.Response.Responses = transactionProposalResponses
	if len(transactionProposalResponses) > 0 {
		requestContext.Response.Payload = transactionProposalResponses[0].ProposalResponse.GetResponse().Payload
		requestContext.Response.ChaincodeStatus = transactionProposalResponses[0].ChaincodeStatus
	}

	//Delegate to next step if any
	if e.next != nil {
		e.next.Handle(requestContext, clientContext)
	}
}

// NewDelegateEndorsementHandler returns a handler that endorses a signed proposal
func NewDelegateEndorsementHandler(proposal *pb.SignedProposal, next ...invoke.Handler) *DelegateEndorsementHandler {
	return &DelegateEndorsementHandler{next: getNext(next), signedProposal: proposal}
}

// NewDelegateQueryHandler returns query handler with chain of ProposalProcessorHandler, NewDelegateEndorsementHandler, EndorsementValidationHandler and SignatureValidationHandler
func NewDelegateQueryHandler(proposal *pb.SignedProposal, next ...invoke.Handler) invoke.Handler {
	return invoke.NewProposalProcessorHandler(
		NewDelegateEndorsementHandler(
			proposal,
			invoke.NewEndorsementValidationHandler(
				invoke.NewSignatureValidationHandler(next...),
			),
		),
	)
}

// DelegateSelectAndEndorseHandler selects endorsers according to the policies of the chaincodes in the provided invocation chain
// and then sends the proposal to those endorsers. The read/write sets from the responses are then checked to see if additional
// chaincodes were invoked that were not in the original invocation chain. If so, a new endorser set is computed with the
// additional chaincodes and (if necessary) endorsements are requested from those additional endorsers.
type DelegateSelectAndEndorseHandler struct {
	signedProposal *pb.SignedProposal
	*invoke.EndorsementHandler
	next invoke.Handler
}

// NewDelegateSelectAndEndorseHandler returns a new DelegateSelectAndEndorseHandler
func NewDelegateSelectAndEndorseHandler(proposal *pb.SignedProposal, next ...invoke.Handler) invoke.Handler {
	return &DelegateSelectAndEndorseHandler{
		EndorsementHandler: invoke.NewEndorsementHandler(),
		next:               getNext(next),
		signedProposal:     proposal,
	}
}

// Handle selects endorsers and sends proposals to the endorsers
func (e *DelegateSelectAndEndorseHandler) Handle(requestContext *invoke.RequestContext, clientContext *invoke.ClientContext) {
	var ccCalls []*fab.ChaincodeCall
	targets := requestContext.Opts.Targets
	if len(targets) == 0 {
		var err error
		ccCalls, requestContext.Opts.Targets, err = getEndorsers(requestContext, clientContext)
		if err != nil {
			requestContext.Error = err
			return
		}
	}

	if nil == e.signedProposal {
		requestContext.Error = fmt.Errorf("a signed proposal was not provided")
		return
	}

	t, err := getDelegateTransactor(clientContext)
	if err != nil {
		requestContext.Error = err
		return
	}
	transactionProposalResponses, err := t.SendSignedProposal(
		e.signedProposal,
		peer.PeersToTxnProcessors(requestContext.Opts.Targets),
	)

	if err != nil {
		requestContext.Error = err
		return
	}

	requestContext.Response.Responses = transactionProposalResponses
	if len(transactionProposalResponses) > 0 {
		requestContext.Response.Payload = transactionProposalResponses[0].ProposalResponse.GetResponse().Payload
		requestContext.Response.ChaincodeStatus = transactionProposalResponses[0].ChaincodeStatus
	}

	if len(targets) == 0 && len(requestContext.Response.Responses) > 0 {
		additionalEndorsers, err := getAdditionalEndorsers(requestContext, clientContext, ccCalls)
		if err != nil {
			// Log a warning. No need to fail the endorsement. Use the responses collected so far,
			// which may be sufficient to satisfy the chaincode policy.
			logger.Warnf("error getting additional endorsers: %s", err)
		} else {
			if len(additionalEndorsers) > 0 {
				requestContext.Opts.Targets = additionalEndorsers
				logger.Debugf("...getting additional endorsements from %d target(s)", len(additionalEndorsers))
				additionalResponses, err := t.SendSignedProposal(e.signedProposal, peer.PeersToTxnProcessors(additionalEndorsers))
				if err != nil {
					requestContext.Error = fmt.Errorf("error sending transaction proposal: %w", err)
					return
				}

				// Add the new endorsements to the list of responses
				requestContext.Response.Responses = append(requestContext.Response.Responses, additionalResponses...)
			} else {
				logger.Debugf("...no additional endorsements are required.")
			}
		}
	}

	if e.next != nil {
		e.next.Handle(requestContext, clientContext)
	}
}

// NewDelegateInvokeHandler returns delegate endorse handler with chain of NewDelegateSelectAndEndorseHandler, EndorsementValidationHandler and SignatureValidationHandler
func NewDelegateInvokeHandler(proposal *pb.SignedProposal, next ...invoke.Handler) invoke.Handler {
	return NewDelegateSelectAndEndorseHandler(
		proposal,
		invoke.NewEndorsementValidationHandler(
			invoke.NewSignatureValidationHandler(next...),
		),
	)
}
