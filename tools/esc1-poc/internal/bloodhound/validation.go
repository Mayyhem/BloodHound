// Copyright 2026 Specter Ops, Inc.
//
// Licensed under the Apache License, Version 2.0
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package bloodhound

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Validate the entire envelope before choosing a format so mixed collections
// cannot depend on the order of their top-level fields.
func validateJSON(payload []byte) error {
	var (
		fields     map[string]json.RawMessage
		collection struct {
			Meta struct {
				Type    string `json:"type"`
				Version int    `json:"version"`
				Count   *int   `json:"count"`
			} `json:"meta"`
			Data []json.RawMessage `json:"data"`
		}
		supportedTypes = " sessions users groups computers gpos ous domains deleted containers localgroups aiacas rootcas enterprisecas ntauthstores certtemplates azure issuancepolicies sites siteservers sitesubnets "
	)
	if err := json.Unmarshal(payload, &fields); err != nil {
		return fmt.Errorf("invalid collection JSON: %w", err)
	}
	if graph, found := fields["graph"]; found {
		if _, found := fields["meta"]; found {
			return fmt.Errorf("collection cannot mix graph with meta or data")
		}
		if _, found := fields["data"]; found {
			return fmt.Errorf("collection cannot mix graph with meta or data")
		}
		return validateOpenGraph(graph, fields["metadata"])
	}
	if err := json.Unmarshal(payload, &collection); err != nil {
		return fmt.Errorf("invalid collection JSON: %w", err)
	}
	if collection.Meta.Type == "" || !strings.Contains(supportedTypes, " "+collection.Meta.Type+" ") {
		return fmt.Errorf("unsupported collection type %q", collection.Meta.Type)
	}
	if collection.Meta.Version <= 0 {
		return fmt.Errorf("collection meta.version must be positive")
	}
	if collection.Data == nil {
		return fmt.Errorf("collection data array is required")
	}
	if collection.Meta.Count != nil && *collection.Meta.Count != len(collection.Data) {
		return fmt.Errorf("collection meta.count does not match data length")
	}
	for _, item := range collection.Data {
		if len(bytes.TrimSpace(item)) == 0 || bytes.TrimSpace(item)[0] != '{' {
			return fmt.Errorf("collection data entries must be objects")
		}
	}
	return nil
}

func readCollection(path string) ([]byte, string, error) {
	var (
		payload, err = os.ReadFile(path)
		contentType  string
		archive      *zip.Reader
		jsonFiles    int
	)
	if err != nil {
		return nil, "", err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		contentType = "application/json"
		err = validateJSON(payload)
	case ".zip":
		contentType = "application/zip"
		archive, err = zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
		if err != nil {
			return nil, "", fmt.Errorf("invalid collection ZIP: %w", err)
		}
		for _, file := range archive.File {
			if file.FileInfo().IsDir() {
				continue
			}
			if strings.ToLower(filepath.Ext(file.Name)) != ".json" {
				return nil, "", fmt.Errorf("ZIP member %q is not collection JSON", file.Name)
			}
			if err = validateZIPMember(file); err != nil {
				return nil, "", fmt.Errorf("ZIP member %q: %w", file.Name, err)
			}
			jsonFiles++
		}
		if jsonFiles == 0 {
			err = fmt.Errorf("collection ZIP has no JSON files")
		}
	default:
		err = fmt.Errorf("collection file must have a .json or .zip extension")
	}
	if err != nil {
		return nil, "", err
	}
	return payload, contentType, nil
}

func validateZIPMember(file *zip.File) error {
	var (
		reader, err = file.Open()
		payload     []byte
	)
	if err != nil {
		return err
	}
	defer reader.Close()
	// Reading to EOF also verifies the archive member's checksum. This modest
	// limit prevents an accidentally huge compressed file from exhausting memory.
	payload, err = io.ReadAll(io.LimitReader(reader, 256<<20+1))
	if err != nil {
		return err
	}
	if len(payload) > 256<<20 {
		return fmt.Errorf("uncompressed collection exceeds 256 MiB")
	}
	return validateJSON(payload)
}

// UploadSchema registers only display node and relationship kinds. This small
// subset has no findings, environments, privilege zones, or saved queries, which
// would require a separate publication policy. Use a dedicated experiment schema:
// the server reconciles an upsert and may remove omitted existing components.
func (s *Client) UploadSchema(ctx context.Context, payload []byte) error {
	var (
		fields map[string]json.RawMessage
		schema struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			Version   string `json:"version"`
		}
		nodes []struct {
			Name string `json:"name"`
		}
		relationships []struct {
			Name string `json:"name"`
		}
	)
	if err := json.Unmarshal(payload, &fields); err != nil {
		return fmt.Errorf("invalid schema JSON: %w", err)
	}
	for name := range fields {
		switch name {
		case "schema", "node_kinds", "relationship_kinds":
		default:
			return fmt.Errorf("schema field %q is outside the supported subset", name)
		}
	}
	if err := json.Unmarshal(fields["schema"], &schema); err != nil {
		return fmt.Errorf("invalid schema metadata: %w", err)
	}
	if strings.TrimSpace(schema.Name) == "" || strings.TrimSpace(schema.Namespace) == "" || !strings.HasPrefix(schema.Version, "v") {
		return fmt.Errorf("schema needs name, namespace, and a version beginning with v")
	}
	if err := json.Unmarshal(fields["node_kinds"], &nodes); err != nil {
		return fmt.Errorf("invalid node_kinds: %w", err)
	}
	if len(nodes) == 0 {
		return fmt.Errorf("server requires at least one node kind")
	}
	if raw, ok := fields["relationship_kinds"]; ok {
		if err := json.Unmarshal(raw, &relationships); err != nil {
			return fmt.Errorf("invalid relationship_kinds: %w", err)
		}
	}
	for _, node := range nodes {
		if !strings.HasPrefix(node.Name, schema.Namespace+"_") || strings.TrimPrefix(node.Name, schema.Namespace+"_") == "" {
			return fmt.Errorf("node kind %q must have the schema namespace prefix", node.Name)
		}
	}
	for _, relationship := range relationships {
		if !strings.HasPrefix(relationship.Name, schema.Namespace+"_") || strings.TrimPrefix(relationship.Name, schema.Namespace+"_") == "" {
			return fmt.Errorf("relationship kind %q must have the schema namespace prefix", relationship.Name)
		}
	}
	return s.request(ctx, http.MethodPut, "/api/v2/extensions", "application/json", "", payload, nil)
}
