package sigstore

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"regexp"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
)

// Signer is who a certificate names: the workflow file of a GitHub Actions
// run, as owner/repo/.github/workflows/<file>, the subject it came from, and
// the repo the run was for, as owner/repo.
type Signer struct {
	Workflow string
	Subject  string
	Repo     string
}

var (
	workflowSubjectRe = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+/\.github/workflows/[^/@]+)@`)
	slsaBuildersRe    = regexp.MustCompile(slsaBuilders)
)

// SignerOf reads the certificate of data and returns who it names. data is a
// bundle in either of cosign's formats, a signed envelope with its
// certificate, or a certificate as cosign writes it. SignerOf checks nothing.
// oku uses it to choose what to ask of a signature, and Verify checks that.
func SignerOf(data []byte) (Signer, error) {
	cert, err := certificateOf(data)
	if err != nil {
		return Signer{}, err
	}

	summary, err := certificate.SummarizeCertificate(cert)
	if err != nil {
		return Signer{}, err
	}

	if summary.Issuer != GitHubIssuer {
		return Signer{}, errors.New("the certificate is not a GitHub Actions workflow's")
	}

	s := Signer{
		Subject: summary.SubjectAlternativeName,
		Repo:    strings.TrimPrefix(summary.SourceRepositoryURI, "https://github.com/"),
	}

	if m := workflowSubjectRe.FindStringSubmatch(s.Subject); m != nil {
		s.Workflow = m[1]
	}

	return s, nil
}

// SLSABuilder reports whether s is a builder of slsa-github-generator that oku
// trusts for provenance.
func (s Signer) SLSABuilder() bool {
	return slsaBuildersRe.MatchString(s.Subject)
}

func certificateOf(data []byte) (*x509.Certificate, error) {
	var found struct {
		Cert       string `json:"cert"`
		Signatures []struct {
			Cert string `json:"cert"`
		} `json:"signatures"`
	}

	// cosign's older bundle holds the certificate, and an envelope holds it in
	// its signature.
	if json.Unmarshal(data, &found) == nil {
		switch {
		case found.Cert != "":
			if decoded, err := base64.StdEncoding.DecodeString(found.Cert); err == nil {
				return parsePEM(decoded)
			}
		case len(found.Signatures) == 1 && found.Signatures[0].Cert != "":
			return parsePEM(pemOf([]byte(found.Signatures[0].Cert)))
		}

		var b bundle.Bundle
		if err := b.UnmarshalJSON(data); err != nil {
			return nil, err
		}

		content, err := b.VerificationContent()
		if err != nil {
			return nil, err
		}

		if cert := content.Certificate(); cert != nil {
			return cert, nil
		}

		return nil, errors.New("the bundle holds a key, not a certificate")
	}

	return parsePEM(pemOf(data))
}

func parsePEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("the certificate is not PEM")
	}

	return x509.ParseCertificate(block.Bytes)
}
