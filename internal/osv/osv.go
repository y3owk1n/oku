// Package osv asks the OSV database whether a package version is malicious.
package osv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// API is the address of the OSV database.
const API = "https://api.osv.dev"

// Client asks the OSV database at Base with HTTP.
type Client struct {
	Base string
	HTTP *http.Client
}

// Package is a version of a package of an ecosystem, by OSV's names, such as
// "npm" or "PyPI".
type Package struct {
	Ecosystem, Name, Version string
}

// Finding is a package that OSV lists as malicious, and the ids of the
// advisories that say so.
type Finding struct {
	Package
	IDs []string
}

// batchSize is the most queries OSV takes in one request.
const batchSize = 1000

// Malicious returns the ids of the advisories that list version of the
// package name in ecosystem as malicious.
func (c Client) Malicious(ctx context.Context, ecosystem, name, version string) ([]string, error) {
	found, err := c.MaliciousOf(ctx, []Package{{ecosystem, name, version}})
	if err != nil || len(found) == 0 {
		return nil, err
	}

	return found[0].IDs, nil
}

// MaliciousOf returns the packages of pkgs that OSV lists as malicious, as the
// OpenSSF malicious-packages project reports them with ids that start "MAL-".
// An advisory that was withdrawn does not count.
func (c Client) MaliciousOf(ctx context.Context, pkgs []Package) ([]Finding, error) {
	type query struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		Version string `json:"version"`
	}

	var findings []Finding

	for start := 0; start < len(pkgs); start += batchSize {
		batch := pkgs[start:min(start+batchSize, len(pkgs))]
		queries := make([]query, len(batch))

		for i, p := range batch {
			queries[i].Package.Name, queries[i].Package.Ecosystem, queries[i].Version = p.Name, p.Ecosystem, p.Version
		}

		var answer struct {
			Results []struct {
				Vulns []struct {
					ID string `json:"id"`
				} `json:"vulns"`
			} `json:"results"`
		}

		if err := c.post(ctx, "/v1/querybatch", map[string]any{"queries": queries}, &answer); err != nil {
			return nil, err
		}

		if len(answer.Results) != len(batch) {
			return nil, fmt.Errorf("%s answered %d of %d queries", c.Base, len(answer.Results), len(batch))
		}

		for i, result := range answer.Results {
			var ids []string

			for _, v := range result.Vulns {
				if !strings.HasPrefix(v.ID, "MAL-") {
					continue
				}

				withdrawn, err := c.withdrawn(ctx, v.ID)
				if err != nil {
					return nil, err
				}

				if !withdrawn {
					ids = append(ids, v.ID)
				}
			}

			if len(ids) > 0 {
				findings = append(findings, Finding{batch[i], ids})
			}
		}
	}

	return findings, nil
}

// withdrawn reports whether the advisory id was withdrawn. A batch answer
// gives ids alone.
func (c Client) withdrawn(ctx context.Context, id string) (bool, error) {
	var advisory struct {
		Withdrawn string `json:"withdrawn"`
	}

	if err := c.get(ctx, "/v1/vulns/"+id, &advisory); err != nil {
		return false, err
	}

	return advisory.Withdrawn != "", nil
}

func (c Client) post(ctx context.Context, path string, body, into any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	return c.do(req, into)
}

func (c Client) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}

	return c.do(req, into)
}

func (c Client) do(req *http.Request, into any) error {
	req.Header.Set("User-Agent", "oku")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", c.Base, resp.Status)
	}

	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("read the answer of %s: %w", c.Base, err)
	}

	return nil
}
