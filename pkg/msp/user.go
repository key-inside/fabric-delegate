/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package msp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
	"math/big"

	"github.com/golang/protobuf/proto"

	pb_msp "github.com/hyperledger/fabric-protos-go/msp"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/core"
	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/msp"
	"github.com/pkg/errors"
)

func CreateSigningIdentity(mspID string, cert []byte, key *ecdsa.PrivateKey) *User {
	return &User{
		mspID:                 mspID,
		enrollmentCertificate: cert,
		privateKey:            &ecdsaPrivateKey{privKey: key, exportable: true},
	}
}

// User is a representation of a Fabric user
type User struct {
	id                    string
	mspID                 string
	enrollmentCertificate []byte
	privateKey            *ecdsaPrivateKey
}

// Identifier returns user identifier
func (u *User) Identifier() *msp.IdentityIdentifier {
	return &msp.IdentityIdentifier{MSPID: u.mspID, ID: u.id}
}

// Verify a signature over some message using this identity as reference
func (u *User) Verify(msg []byte, sig []byte) error {
	return errors.New("not implemented")
}

// Serialize converts an identity to bytes
func (u *User) Serialize() ([]byte, error) {
	serializedIdentity := &pb_msp.SerializedIdentity{
		Mspid:   u.mspID,
		IdBytes: u.enrollmentCertificate,
	}
	identity, err := proto.Marshal(serializedIdentity)
	if err != nil {
		return nil, errors.Wrap(err, "marshal serializedIdentity failed")
	}
	return identity, nil
}

// EnrollmentCertificate Returns the underlying ECert representing this user’s identity.
func (u *User) EnrollmentCertificate() []byte {
	return u.enrollmentCertificate
}

// PrivateKey returns the crypto suite representation of the private key
func (u *User) PrivateKey() core.Key {
	return u.privateKey
}

// PublicVersion returns the public parts of this identity
func (u *User) PublicVersion() msp.Identity {
	return u
}

// Sign the message
func (u *User) Sign(msg []byte) ([]byte, error) {
	h := sha256.New()
	h.Write(msg)
	digest := h.Sum(nil)

	r, s, err := ecdsa.Sign(rand.Reader, u.privateKey.privKey, digest)
	if err != nil {
		return nil, err
	}
	s, err = ToLowS(&u.privateKey.privKey.PublicKey, s)
	if err != nil {
		return nil, err
	}

	//return u.privateKey.privKey.Sign(rand.Reader, msg, nil)

	return MarshalECDSASignature(r, s)
}

var (
	// curveHalfOrders contains the precomputed curve group orders halved.
	// It is used to ensure that signature' S value is lower or equal to the
	// curve group order halved. We accept only low-S signatures.
	// They are precomputed for efficiency reasons.
	curveHalfOrders = map[elliptic.Curve]*big.Int{
		elliptic.P224(): new(big.Int).Rsh(elliptic.P224().Params().N, 1),
		elliptic.P256(): new(big.Int).Rsh(elliptic.P256().Params().N, 1),
		elliptic.P384(): new(big.Int).Rsh(elliptic.P384().Params().N, 1),
		elliptic.P521(): new(big.Int).Rsh(elliptic.P521().Params().N, 1),
	}
)

type ECDSASignature struct {
	R, S *big.Int
}

func MarshalECDSASignature(r, s *big.Int) ([]byte, error) {
	return asn1.Marshal(ECDSASignature{r, s})
}

// IsLow checks that s is a low-S
func IsLowS(k *ecdsa.PublicKey, s *big.Int) (bool, error) {
	halfOrder, ok := curveHalfOrders[k.Curve]
	if !ok {
		return false, fmt.Errorf("curve not recognized [%s]", k.Curve)
	}

	return s.Cmp(halfOrder) != 1, nil

}

func ToLowS(k *ecdsa.PublicKey, s *big.Int) (*big.Int, error) {
	lowS, err := IsLowS(k, s)
	if err != nil {
		return nil, err
	}

	if !lowS {
		// Set s to N - s that will be then in the lower part of signature space
		// less or equal to half order
		s.Sub(k.Params().N, s)

		return s, nil
	}

	return s, nil
}
