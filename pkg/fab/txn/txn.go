package txn

import (
	reqContext "context"
	"fmt"
	"math/rand"

	"github.com/hyperledger/fabric-sdk-go/pkg/common/logging"
	ctxprovider "github.com/hyperledger/fabric-sdk-go/pkg/common/providers/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	"github.com/hyperledger/fabric-sdk-go/pkg/context"
)

var logger = logging.NewLogger("fabsdk-delegate/fab")

func BroadcastEnvelope(reqCtx reqContext.Context, envelope *fab.SignedEnvelope, orderers []fab.Orderer) (*fab.TransactionResponse, error) {
	// Check if orderers are defined
	if len(orderers) == 0 {
		return nil, fmt.Errorf("orderers not set")
	}

	// Copy aside the ordering service endpoints
	randOrderers := []fab.Orderer{}
	randOrderers = append(randOrderers, orderers...)

	// get a context client instance to create child contexts with timeout read from the config in sendBroadcast()
	ctxClient, ok := context.RequestClientContext(reqCtx)
	if !ok {
		return nil, fmt.Errorf("failed get client context from reqContext for SendTransaction")
	}

	// Iterate them in a random order and try broadcasting 1 by 1
	var errResp error
	for _, i := range rand.Perm(len(randOrderers)) {
		resp, err := sendBroadcast(reqCtx, envelope, randOrderers[i], ctxClient)
		if err != nil {
			errResp = err
		} else {
			return resp, nil
		}
	}
	return nil, errResp
}

func sendBroadcast(reqCtx reqContext.Context, envelope *fab.SignedEnvelope, orderer fab.Orderer, client ctxprovider.Client) (*fab.TransactionResponse, error) {
	logger.Debugf("Broadcasting envelope to orderer: %s\n", orderer.URL())
	// create a childContext for this SendBroadcast orderer using the config's timeout value
	// the parent context (reqCtx) should not have a timeout value
	childCtx, cancel := context.NewRequest(client, context.WithTimeoutType(fab.OrdererResponse), context.WithParent(reqCtx))
	defer cancel()

	// Send request
	if _, err := orderer.SendBroadcast(childCtx, envelope); err != nil {
		logger.Debugf("Receive Error Response from orderer: %s\n", err)
		return nil, fmt.Errorf("calling orderer '%s' failed: %w", orderer.URL(), err)
	}

	logger.Debugf("Receive Success Response from orderer\n")
	return &fab.TransactionResponse{Orderer: orderer.URL()}, nil
}
