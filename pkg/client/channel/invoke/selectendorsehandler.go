package invoke

import (
	"fmt"

	"github.com/golang/protobuf/proto"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel/invoke"
	selectopts "github.com/hyperledger/fabric-sdk-go/pkg/client/common/selection/options"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/retry"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/logging"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/options"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"

	"github.com/key-inside/fabric-delegate/pkg/client/channel/invoke/rwsetutil"
)

var logger = logging.NewLogger("fabsdk-delegate/client")

var lsccFilter = func(ccID string) bool {
	return ccID != "lscc" && ccID != "_lifecycle"
}

func getEndorsers(requestContext *invoke.RequestContext, clientContext *invoke.ClientContext, opts ...options.Opt) ([]*fab.ChaincodeCall, []fab.Peer, error) {
	var selectionOpts []options.Opt
	selectionOpts = append(selectionOpts, opts...)
	if requestContext.SelectionFilter != nil {
		selectionOpts = append(selectionOpts, selectopts.WithPeerFilter(requestContext.SelectionFilter))
	}
	if requestContext.PeerSorter != nil {
		selectionOpts = append(selectionOpts, selectopts.WithPeerSorter(requestContext.PeerSorter))
	}

	ccCalls := newInvocationChain(requestContext)
	peers, err := clientContext.Selection.GetEndorsersForChaincode(newInvocationChain(requestContext), selectionOpts...)
	return ccCalls, peers, err
}

func getAdditionalEndorsers(requestContext *invoke.RequestContext, clientContext *invoke.ClientContext, invocationChain []*fab.ChaincodeCall) ([]fab.Peer, error) {
	invocationChainFromResponse, err := getInvocationChainFromResponse(requestContext.Response.Responses[0])
	if err != nil {
		return nil, fmt.Errorf("error getting invocation chain from proposal response: %w", err)
	}

	invocationChain, foundAdditional := mergeInvocationChains(invocationChain, invocationChainFromResponse, getCCFilter(requestContext))
	if !foundAdditional {
		return nil, nil
	}

	requestContext.Request.InvocationChain = invocationChain

	logger.Debugf("Found additional chaincodes/collections. Checking if additional endorsements are required...")

	// If using Fabric selection then disable retries. We don't want to keep retrying if the endorsement query returns an error.
	// Also, add a priority selector that gives priority to peers from which we already have endorsements. This way, we don't
	// unnecessarily get endorsements from other orgs.
	_, endorsers, err := getEndorsers(
		requestContext, clientContext,
		selectopts.WithRetryOpts(retry.Opts{}),
		selectopts.WithPrioritySelector(prioritizePeers(requestContext.Opts.Targets)))
	if err != nil {
		return nil, fmt.Errorf("error getting additional endorsers: %w", err)
	}

	var additionalEndorsers []fab.Peer
	for _, endorser := range endorsers {
		if !containsMSP(requestContext.Opts.Targets, endorser.MSPID()) {
			logger.Debugf("... will ask for additional endorsement from [%s] in order to satisfy the chaincode policy", endorser.URL())
			additionalEndorsers = append(additionalEndorsers, endorser)
		}
	}

	return additionalEndorsers, nil
}

func getCCFilter(requestContext *invoke.RequestContext) invoke.CCFilter {
	if requestContext.Opts.CCFilter != nil {
		return invoke.NewChainedCCFilter(lsccFilter, requestContext.Opts.CCFilter)
	}
	return lsccFilter
}

func containsMSP(peers []fab.Peer, mspID string) bool {
	for _, p := range peers {
		if p.MSPID() == mspID {
			return true
		}
	}
	return false
}

func getInvocationChainFromResponse(response *fab.TransactionProposalResponse) ([]*fab.ChaincodeCall, error) {
	rwSets, err := getRWSetsFromProposalResponse(response.ProposalResponse)
	if err != nil {
		return nil, err
	}

	invocationChain := make([]*fab.ChaincodeCall, len(rwSets))
	for i, rwSet := range rwSets {
		collections := make([]string, len(rwSet.CollHashedRwSets))
		for j, collRWSet := range rwSet.CollHashedRwSets {
			collections[j] = collRWSet.CollectionName
		}
		logger.Debugf("Found chaincode in RWSet [%s], Collections %v", rwSet.NameSpace, collections)
		invocationChain[i] = &fab.ChaincodeCall{ID: rwSet.NameSpace, Collections: collections}
	}

	return invocationChain, nil
}

func getRWSetsFromProposalResponse(response *pb.ProposalResponse) ([]*rwsetutil.NsRwSet, error) {
	if response == nil {
		return nil, nil
	}

	prp := &pb.ProposalResponsePayload{}
	err := proto.Unmarshal(response.Payload, prp)
	if err != nil {
		return nil, err
	}

	chaincodeAction := &pb.ChaincodeAction{}
	err = proto.Unmarshal(prp.Extension, chaincodeAction)
	if err != nil {
		return nil, err
	}

	if len(chaincodeAction.Results) == 0 {
		return nil, nil
	}

	txRWSet := &rwsetutil.TxRwSet{}
	if err := txRWSet.FromProtoBytes(chaincodeAction.Results); err != nil {
		return nil, err
	}

	return txRWSet.NsRwSets, nil
}

func mergeInvocationChains(invocChain []*fab.ChaincodeCall, respInvocChain []*fab.ChaincodeCall, filter invoke.CCFilter) ([]*fab.ChaincodeCall, bool) {
	var mergedInvocChain []*fab.ChaincodeCall
	var changed bool
	for _, respCCCall := range respInvocChain {
		if !filter(respCCCall.ID) {
			logger.Debugf("Ignoring chaincode [%s] in the RW set since it was filtered out", respCCCall.ID)
			continue
		}
		mergedCCCall, merged := mergeCCCall(invocChain, respCCCall)
		if merged {
			changed = true
		}
		mergedInvocChain = append(mergedInvocChain, mergedCCCall)
	}
	return mergedInvocChain, changed
}

func mergeCCCall(invocChain []*fab.ChaincodeCall, respCCCall *fab.ChaincodeCall) (*fab.ChaincodeCall, bool) {
	ccCall, ok := getCCCall(invocChain, respCCCall.ID)
	if ok {
		logger.Debugf("Already have chaincode [%s]. Checking to see if any private data collections were detected in the proposal response", respCCCall.ID)
		c, merged := merge(ccCall, respCCCall)
		if merged {
			logger.Debugf("Modifying chaincode call for chaincode [%s] since additional private data collections were detected in the RW set", respCCCall.ID)
		} else {
			logger.Debugf("No additional private data collections were detected for chaincode [%s]", respCCCall.ID)
		}
		return c, merged
	}

	logger.Debugf("Detected chaincode [%s] in the RW set of the proposal response that was not part of the original invocation chain", respCCCall.ID)
	return respCCCall, true
}

func getCCCall(invocChain []*fab.ChaincodeCall, ccID string) (*fab.ChaincodeCall, bool) {
	for _, ccCall := range invocChain {
		if ccCall.ID == ccID {
			return ccCall, true
		}
	}
	return nil, false
}

func merge(c1 *fab.ChaincodeCall, c2 *fab.ChaincodeCall) (*fab.ChaincodeCall, bool) {
	c := &fab.ChaincodeCall{ID: c1.ID, Collections: c1.Collections}
	merged := false
	for _, coll := range c2.Collections {
		if !contains(c.Collections, coll) {
			c.Collections = append(c.Collections, coll)
			merged = true
		}
	}
	return c, merged
}

func contains(values []string, value string) bool {
	for _, val := range values {
		if val == value {
			return true
		}
	}
	return false
}

func prioritizePeers(peers []fab.Peer) selectopts.PrioritySelector {
	return func(peer1, peer2 fab.Peer) int {
		hasPeer1 := containsPeer(peers, peer1)
		hasPeer2 := containsPeer(peers, peer2)

		if hasPeer1 && hasPeer2 {
			return 0
		}
		if hasPeer1 {
			return 1
		}
		if hasPeer2 {
			return -1
		}
		return 0
	}
}

func containsPeer(peers []fab.Peer, peer fab.Peer) bool {
	for _, p := range peers {
		if p.URL() == peer.URL() {
			return true
		}
	}
	return false
}
