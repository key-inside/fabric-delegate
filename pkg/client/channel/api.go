package channel

import (
	reqContext "context"
	"fmt"
	"time"

	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel"
	"github.com/hyperledger/fabric-sdk-go/pkg/client/channel/invoke"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/errors/retry"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/context"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	"github.com/hyperledger/fabric-sdk-go/pkg/fab/comm"
)

type Request channel.Request

type Response channel.Response

type DelegateRequestOption func(ctx context.Client, opts *invoke.Opts) error

// WithTargets allows overriding of the target peers for the request
func WithTargets(targets ...fab.Peer) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {

		// Validate targets
		for _, t := range targets {
			if t == nil {
				return fmt.Errorf("target is nil")
			}
		}

		o.Targets = targets
		return nil
	}
}

// WithTargetEndpoints allows overriding of the target peers for the request.
// Targets are specified by name or URL, and the SDK will create the underlying peer
// objects.
func WithTargetEndpoints(keys ...string) DelegateRequestOption {
	return func(ctx context.Client, opts *invoke.Opts) error {

		var targets []fab.Peer

		for _, url := range keys {

			peerCfg, err := comm.NetworkPeerConfig(ctx.EndpointConfig(), url)
			if err != nil {
				return err
			}

			peer, err := ctx.InfraProvider().CreatePeerFromConfig(peerCfg)
			if err != nil {
				return fmt.Errorf("creating peer from config failed: %w", err)
			}

			targets = append(targets, peer)
		}

		return WithTargets(targets...)(ctx, opts)
	}
}

// WithTargetFilter specifies a per-request target peer-filter
func WithTargetFilter(filter fab.TargetFilter) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		o.TargetFilter = filter
		return nil
	}
}

// WithTargetSorter specifies a per-request target sorter
func WithTargetSorter(sorter fab.TargetSorter) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		o.TargetSorter = sorter
		return nil
	}
}

// WithRetry option to configure retries
func WithRetry(retryOpt retry.Opts) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		o.Retry = retryOpt
		return nil
	}
}

// WithBeforeRetry specifies a function to call before a retry attempt
func WithBeforeRetry(beforeRetry retry.BeforeRetryHandler) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		o.BeforeRetry = beforeRetry
		return nil
	}
}

// WithTimeout encapsulates key value pairs of timeout type, timeout duration to Options
func WithTimeout(timeoutType fab.TimeoutType, timeout time.Duration) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		if o.Timeouts == nil {
			o.Timeouts = make(map[fab.TimeoutType]time.Duration)
		}
		o.Timeouts[timeoutType] = timeout
		return nil
	}
}

// WithParentContext encapsulates grpc parent context
func WithParentContext(parentContext reqContext.Context) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		o.ParentContext = parentContext
		return nil
	}
}

// WithChaincodeFilter adds a chaincode filter for figuring out additional endorsers
func WithChaincodeFilter(ccFilter invoke.CCFilter) DelegateRequestOption {
	return func(ctx context.Client, o *invoke.Opts) error {
		o.CCFilter = ccFilter
		return nil
	}
}
