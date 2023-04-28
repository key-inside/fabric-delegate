# Fabric Delegate SDK

* CreateSignedProposal
* DelegateEndorseAndCreateTxnPayload
* SignTxnPayload
* BroadcastEnvelope
* DelegateQuery
* DelegateInvokeHandler

## Usage

```go
import (
    "github.com/hyperledger/fabric-protos-go/peer"
    "github.com/key-inside/fabric-delegate/pkg/client/channel"
    "github.com/key-inside/fabric-delegate/pkg/core/config"
    "github.com/key-inside/fabric-delegate/pkg/fabsdk"
)

func delegateQuery(cfgPath string, sp *peer.SignedProposal, opts ...channel.DelegateRequestOption) {
    sdk, _ := fabsdk.New(config.FromFile(cfgPath))
    defer sdk.Close()

    ctx := sdk.ChannelContext("sample-channel", fabsdk.WithUser("delegator"))
    client := channel.New(ctx)
    client.DelegateQuery(sp, opts...)
}
```