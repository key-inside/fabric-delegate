package txn

import (
	reqContext "context"
	"fmt"
	"sync"

	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/multi"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
)

// SendSignedProposal sends a SignedProposal to ProposalProcessor.
func SendSignedProposal(reqCtx reqContext.Context, proposal *pb.SignedProposal, targets []fab.ProposalProcessor) ([]*fab.TransactionProposalResponse, error) {
	if proposal == nil {
		return nil, fmt.Errorf("proposal is required")
	}

	if len(targets) < 1 {
		return nil, fmt.Errorf("targets is required")
	}

	for _, p := range targets {
		if p == nil {
			return nil, fmt.Errorf("target is nil")
		}
	}

	targets = getTargetsWithoutDuplicates(targets)

	request := fab.ProcessProposalRequest{SignedProposal: proposal}

	var responseMtx sync.Mutex
	var transactionProposalResponses []*fab.TransactionProposalResponse
	var wg sync.WaitGroup
	errs := multi.Errors{}

	for _, p := range targets {
		wg.Add(1)
		go func(processor fab.ProposalProcessor) {
			defer wg.Done()

			resp, err := processor.ProcessTransactionProposal(reqCtx, request)
			if err != nil {
				logger.Debugf("Received error response from txn proposal processing: %s", err)
				responseMtx.Lock()
				errs = append(errs, err)
				responseMtx.Unlock()
				return
			}

			responseMtx.Lock()
			transactionProposalResponses = append(transactionProposalResponses, resp)
			responseMtx.Unlock()
		}(p)
	}
	wg.Wait()

	return transactionProposalResponses, errs.ToError()
}

// getTargetsWithoutDuplicates returns a list of targets without duplicates
func getTargetsWithoutDuplicates(targets []fab.ProposalProcessor) []fab.ProposalProcessor {
	peerUrlsToTargets := map[string]fab.ProposalProcessor{}
	var uniqueTargets []fab.ProposalProcessor

	for i := range targets {
		peer, ok := targets[i].(fab.Peer)
		if !ok {
			// ProposalProcessor is not a fab.Peer... cannot remove duplicates
			return targets
		}
		if _, present := peerUrlsToTargets[peer.URL()]; !present {
			uniqueTargets = append(uniqueTargets, targets[i])
			peerUrlsToTargets[peer.URL()] = targets[i]
		}
	}

	if len(uniqueTargets) != len(targets) {
		logger.Warn("Duplicate target peers in configuration")
	}

	return uniqueTargets
}
