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

// Malicious returns the ids of the advisories that list version of the
// package name in ecosystem as malicious, as the OpenSSF malicious-packages
// project reports them to OSV with ids that start "MAL-". Ecosystem is OSV's
// name for it, such as "npm" or "PyPI".
func (c Client) Malicious(ctx context.Context, ecosystem, name, version string) ([]string, error) {
	query, err := json.Marshal(map[string]any{
		"package": map[string]string{"name": name, "ecosystem": ecosystem},
		"version": version,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/v1/query", bytes.NewReader(query))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "oku")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", c.Base, resp.Status)
	}

	var found struct {
		Vulns []struct {
			ID        string `json:"id"`
			Withdrawn string `json:"withdrawn"`
		} `json:"vulns"`
	}

	if err := json.Unmarshal(data, &found); err != nil {
		return nil, fmt.Errorf("read the answer of %s: %w", c.Base, err)
	}

	var ids []string

	for _, v := range found.Vulns {
		if strings.HasPrefix(v.ID, "MAL-") && v.Withdrawn == "" {
			ids = append(ids, v.ID)
		}
	}

	return ids, nil
}
