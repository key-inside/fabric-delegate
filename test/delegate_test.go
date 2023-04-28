package test

import (
	"encoding/hex"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"

	"github.com/key-inside/fabric-delegate/pkg/client/channel"
	"github.com/key-inside/fabric-delegate/pkg/core/config"
	"github.com/key-inside/fabric-delegate/pkg/fabsdk"
)

const (
	cfgFile      = "./fixtures/sample/fabric.yaml"
	user         = "User1"
	org          = "org1"
	delegateUser = "User1"
	delegateOrg  = "org2"
)

var __proposal, __payload, __envelope []byte

func getDelegateClient(username, org string) (*fabsdk.FabricSDK, *channel.Client) {
	sdk, err := fabsdk.New(config.FromFile(cfgFile))
	if err != nil {
		panic(err)
	}
	ctx := sdk.ChannelContext("kiesnet-dev", fabsdk.WithUser(username), fabsdk.WithOrg(org))
	client, err := channel.New(ctx)
	if err != nil {
		panic(err)
	}
	return sdk, client
}

func Test_CreateSignedProposal(t *testing.T) {
	sdk, delegator := getDelegateClient(user, org)
	defer sdk.Close()

	proposal, err := delegator.CreateSignedProposal(
		channel.Request{
			ChaincodeID: "ping",
			Fcn:         "fruit/buy",
			Args:        [][]byte{[]byte(`{"name":"lemon","quantity":5}`)},
		},
	)
	if err != nil {
		t.Errorf("failed to create proposal: %v", err)
	}
	__proposal, err = proto.Marshal(proposal)
	if err != nil {
		t.Errorf("failed to marshal proposal: %v", err)
	}
	t.Log(hex.EncodeToString(__proposal))
}

func Test_DelegateQuery(t *testing.T) {
	sp := &peer.SignedProposal{}
	if err := proto.Unmarshal(__proposal, sp); err != nil {
		t.Errorf("failed to unmarshal proposal: %v", err)
	}

	sdk, delegator := getDelegateClient(delegateUser, delegateOrg)
	defer sdk.Close()

	res, err := delegator.DelegateQuery(sp)
	if err != nil {
		t.Errorf("failed to query: %v", err)
	}
	t.Log(string(res.Payload))
}

func Test_DelegateEndorse(t *testing.T) {
	sp := &peer.SignedProposal{}
	if err := proto.Unmarshal(__proposal, sp); err != nil {
		t.Errorf("failed to unmarshal proposal: %v", err)
	}

	sdk, delegator := getDelegateClient(delegateUser, delegateOrg)
	defer sdk.Close()

	payload, _, err := delegator.DelegateEndorseAndCreateTxnPayload(sp)
	if err != nil {
		t.Errorf("failed to endorse: %v", err)
	}
	__payload, err = proto.Marshal(payload)
	if err != nil {
		t.Errorf("failed to marshal payload: %v", err)
	}
	t.Log(hex.EncodeToString(__payload))
}

func Test_Envelop(t *testing.T) {
	payload := &common.Payload{}
	if err := proto.Unmarshal(__payload, payload); err != nil {
		t.Errorf("failed to unmarshal proposal: %v", err)
	}

	sdk, delegator := getDelegateClient(user, org)
	defer sdk.Close()

	se, err := delegator.SignTxnPayload(payload)
	if err != nil {
		t.Errorf("failed to create envelope: %v", err)
	}
	envelope := &common.Envelope{
		Payload:   se.Payload,
		Signature: se.Signature,
	}

	__envelope, err = proto.Marshal(envelope)
	if err != nil {
		t.Errorf("failed to marshal envelope: %v", err)
	}
	t.Log(hex.EncodeToString(__envelope))
}

func Test_Broadcast(t *testing.T) {
	envelope := &common.Envelope{}
	if err := proto.Unmarshal(__envelope, envelope); err != nil {
		t.Errorf("failed to unmarshal envelope: %v", err)
	}

	sdk, delegator := getDelegateClient(delegateUser, delegateOrg)
	defer sdk.Close()

	res, err := delegator.BroadcastEnvelope(envelope)
	if err != nil {
		t.Errorf("failed to broadcast: %v", err)
	}
	t.Log(string(res.TxValidationCode.String()))
}
