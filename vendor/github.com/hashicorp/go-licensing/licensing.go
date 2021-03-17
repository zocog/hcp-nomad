package licensing

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/hashicorp/errwrap"
	"github.com/mitchellh/copystructure"
)

const (
	v2SigLength = ed25519.SignatureSize*2 + ed25519.PublicKeySize
)

var (
	builtInPubKeys = []string{
		// public key from root pair (see "Prod Licensing Vault")
		"YSB4luRtPWTXAaLYvPlAnPLlnG8NL8H5ruSpEnLEBuQ=",
	}
)

// License represents a license. A few useful items are pulled out to the top
// level; the rest is kept as product-specific flags.
type License struct {
	// The unique identifier of the license
	LicenseID string `json:"license_id" jsonapi:"primary,licenses" db:"id"`

	// The customer ID associated with the license
	CustomerID string `json:"customer_id" jsonapi:"attr,customer_id" db:"customer_id"`

	// If set, an identifier that should be used to lock the license to a
	// particular site, cluster, etc.
	InstallationID string `json:"installation_id" jsonapi:"attr,installation_id" db:"installation_id"`

	// The time at which the license was issued
	IssueTime time.Time `json:"issue_time" jsonapi:"attr,issue_time,iso8601" db:"issue_time"`

	// The time at which the license starts being valid
	StartTime time.Time `json:"start_time" jsonapi:"attr,start_time,iso8601" db:"start_time"`

	// The time after which the license expires
	ExpirationTime time.Time `json:"expiration_time" jsonapi:"attr,expiration_time,iso8601" db:"expiration_time"`

	// The time at which the license ceases to function and can
	// no longer be used in any capacity
	TerminationTime time.Time `json:"termination_time" jsonapi:"attr,termination_time,iso8601" db:"termination_time"`

	// The product the license is valid for
	Product string `json:"product" jsonapi:"attr,product" db:"product"`

	// Installation-specific information; feature bools, cluster IDs, etc.
	Flags map[string]interface{} `json:"flags" jsonapi:"attr,flags" db:"flags"`
}

// IntermediateAuthority contains a public key signed by the root private
// key and is used as a trust chain.
type IntermediateAuthority struct {
	PublicKey     ed25519.PublicKey `json:"pub_key"`
	RootSignature []byte            `json:"root_sig"`
}

// NewIntermediateAuthority is used to generate a new IntermediateAuthority.
// New IntermediateAuthorities mainly be required when rotating.
func NewIntermediateAuthority(rootPriv ed25519.PrivateKey, intPub ed25519.PublicKey) (*IntermediateAuthority, error) {
	rootSig, err := rootPriv.Sign(rand.Reader, []byte(intPub), crypto.Hash(0))
	if err != nil {
		return nil, err
	}
	return &IntermediateAuthority{
		PublicKey:     intPub,
		RootSignature: rootSig,
	}, nil
}

// v2SigEncode returns a byte slice of a v2 signature
func (ia *IntermediateAuthority) v2SigEncode(intSig []byte) []byte {
	out := append([]byte(nil), intSig...)
	out = append(out, []byte(ia.PublicKey)...)
	out = append(out, ia.RootSignature...)
	return out
}

func (ia *IntermediateAuthority) Clone() (*IntermediateAuthority, error) {
	if ia == nil {
		return nil, nil
	}

	iaCopy, err := copystructure.Copy(ia)
	if err != nil {
		return nil, errwrap.Wrapf("error copying license: {{err}}", err)
	}
	return iaCopy.(*IntermediateAuthority), nil
}

// SignedString returns the signed payload for a given license and private key
func (l *License) SignedString(signer crypto.Signer) (string, error) {
	enc, err := json.Marshal(l)
	if err != nil {
		return "", err
	}

	var signature []byte
	switch signer.(type) {
	case ed25519.PrivateKey:
		signature, err = signer.Sign(rand.Reader, enc, new(crypto.Hash))
		if err != nil {
			return "", err
		}
	default:
		return "", errors.New("unsupported key type")
	}

	sig := fmt.Sprintf("vault:v1:%s", base64.StdEncoding.EncodeToString(signature))
	return EncodeLicense(base64.StdEncoding.EncodeToString(enc), sig, 1), nil
}

// SignedString returns the signed payload for a given license and IntermediateAuthority
func (l *License) SignedStringV2(signer ed25519.PrivateKey, ia *IntermediateAuthority) (string, error) {
	b, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	intSig, err := signer.Sign(rand.Reader, b, crypto.Hash(0))
	if err != nil {
		return "", err
	}
	sigEnc := ia.v2SigEncode(intSig)
	sigStr := base64.StdEncoding.EncodeToString(sigEnc)
	return EncodeLicense(base64.StdEncoding.EncodeToString(b), sigStr, 2), nil
}

// Clone creates a copy of a license
func (l *License) Clone() (*License, error) {
	if l == nil {
		return nil, nil
	}

	licenseCopy, err := copystructure.Copy(l)
	if err != nil {
		return nil, errwrap.Wrapf("error copying license: {{err}}", err)
	}

	return licenseCopy.(*License), nil
}

// Equal safely compares two licenses
func (l *License) Equal(r *License) bool {
	return l.LicenseID == r.LicenseID &&
		l.CustomerID == r.CustomerID &&
		l.InstallationID == r.InstallationID &&
		l.IssueTime.Equal(r.IssueTime) &&
		l.StartTime.Equal(r.StartTime) &&
		l.ExpirationTime.Equal(r.ExpirationTime) &&
		l.Product == r.Product &&
		reflect.DeepEqual(l.Flags, r.Flags)
}

// ParseVersion parses the first two bytes as uint8 version number.
func ParseVersion(license string) (uint8, string, error) {
	var ver uint8
	_, err := fmt.Sscanf(license, "%02X%s", &ver, &license)
	if err != nil {
		return 0, "", errwrap.Wrapf("error decoding version: {{err}}", err)
	}
	return ver, license, nil
}

// EncodeLicense is a helper function to take the base64-encoded marshalled
// JSON that was round tripped through the signer and the string signature that
// was returned and turn them into a license string that can be validated by
// this library
func EncodeLicense(marshalledB64JSON string, signature string, version int) string {
	payload := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(fmt.Sprintf("%s.%s", marshalledB64JSON, signature)))
	return fmt.Sprintf("%02X%s", version, payload)
}

// v1Parse returns the license bytes and the signature string from a
// full license string.
func V1Parse(license string) ([]byte, string, error) {
	// B32 decode to get the dot-separated string
	dottedLicense, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(license)
	if err != nil {
		return nil, "", errwrap.Wrapf("error decoding license: {{err}}", err)
	}

	// Split the dot-separated string into component parts
	splitLicense := strings.Split(string(dottedLicense), ".")
	if len(splitLicense) != 2 {
		return nil, "", fmt.Errorf("incorrectly formatted license")
	}

	// Split the license string into the information and signature byte
	// sections, and base64-decode them
	licenseBytes, err := base64.StdEncoding.DecodeString(splitLicense[0])
	if err != nil {
		return nil, "", errwrap.Wrapf("error decoding license information: {{err}}", err)
	}
	return licenseBytes, splitLicense[1], nil
}

// v1SigParse returns the signature bytes for v1 signature strings.
func v1SigParse(sigStr string) ([]byte, error) {
	splitSig := strings.Split(sigStr, ":")
	if len(splitSig) != 3 {
		return nil, fmt.Errorf("incorrectly formatted signature")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(splitSig[2])
	if err != nil {
		return nil, errwrap.Wrapf("error decoding signature: {{err}}", err)
	}
	return sigBytes, nil
}

// v2SigParse returns the intermediate signature bytes and IntermediateAuthority
// in a v2 signature string.
func v2SigParse(sigStr string) ([]byte, *IntermediateAuthority, error) {
	sigBytes, err := base64.StdEncoding.DecodeString(sigStr)
	if err != nil {
		return nil, nil, err
	}
	if len(sigBytes) != v2SigLength {
		return nil, nil, fmt.Errorf("invalid signature length expected %d received %d", v2SigLength, len(sigBytes))
	}
	var offset int
	intSig := append([]byte(nil), sigBytes[:ed25519.SignatureSize]...)
	offset += len(intSig)
	keyBytes := append([]byte(nil), sigBytes[offset:offset+ed25519.PublicKeySize]...)
	intPub := ed25519.PublicKey(keyBytes)
	offset += len(keyBytes)
	rootSig := append([]byte(nil), sigBytes[offset:offset+ed25519.SignatureSize]...)
	return intSig,
		&IntermediateAuthority{
			RootSignature: rootSig,
			PublicKey:     intPub,
		},
		nil
}
