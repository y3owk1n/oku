package sigstore

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
)

// VerifyWithKey checks data, a bundle or a cosign signature in base64, of the
// file whose sha256 is digest, against key, a public key as a manifest names
// it: the base64 between the lines of its PEM. The log must hold the
// signature, as cosign verify-blob --key checks by default, so for a signature
// without a bundle oku finds the log's entry in Rekor.
func (v *Verifier) VerifyWithKey(ctx context.Context, data, digest []byte, key string) error {
	if err := v.trust(); err != nil {
		return err
	}

	der, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("%w: read the signing key: %w", ErrVerify, err)
	}

	public, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return fmt.Errorf("%w: read the signing key: %w", ErrVerify, err)
	}

	keyVerifier, err := signature.LoadVerifier(public, crypto.SHA256)
	if err != nil {
		return fmt.Errorf("%w: read the signing key: %w", ErrVerify, err)
	}

	var b *bundle.Bundle

	if data = bytes.TrimSpace(data); bytes.HasPrefix(data, []byte("{")) {
		if b, err = readBundle(data, digest); err != nil {
			return fmt.Errorf("%w: read the bundle: %w", ErrVerify, err)
		}
	} else if b, err = v.keySignature(ctx, string(data), der, digest); err != nil {
		return err
	}

	material := root.TrustedMaterialCollection{
		root.NewTrustedPublicKeyMaterial(func(string) (root.TimeConstrainedVerifier, error) {
			return root.NewExpiringKey(keyVerifier, time.Time{}, time.Time{}), nil
		}),
		v.material,
	}

	verifier, err := verify.NewVerifier(material, v.keyOptions...)
	if err != nil {
		return err
	}

	if _, err := verifier.Verify(b, verify.NewPolicy(
		verify.WithArtifactDigest("sha256", digest),
		verify.WithKey(),
	)); err != nil {
		return fmt.Errorf("%w: %w", ErrVerify, err)
	}

	return nil
}

// keySignature puts sig, a signature in base64 by the key whose DER is der, of
// the file whose sha256 is digest into a bundle with the log's entry for it.
func (v *Verifier) keySignature(ctx context.Context, sig string, der, digest []byte) (*bundle.Bundle, error) {
	entry, err := v.logged(ctx, digest, sig, func(key []byte) bool {
		block, _ := pem.Decode(key)

		return block != nil && bytes.Equal(block.Bytes, der)
	})
	if err != nil {
		return nil, err
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	if err != nil {
		return nil, fmt.Errorf("%w: read the signature: %w", ErrVerify, err)
	}

	b, err := bundle.NewBundle(&protobundle.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle+json;version=0.1",
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content:     &protobundle.VerificationMaterial_PublicKey{PublicKey: &protocommon.PublicKeyIdentifier{}},
			TlogEntries: []*protorekor.TransparencyLogEntry{entry},
		},
		Content: &protobundle.Bundle_MessageSignature{
			MessageSignature: &protocommon.MessageSignature{
				MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: digest},
				Signature:     raw,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVerify, err)
	}

	return b, nil
}
