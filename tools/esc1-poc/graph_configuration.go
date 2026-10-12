/*
Copyright 2026 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/
package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

func selectedGraphDriver() string { return environment("GRAPH_DRIVER", "neo4j") }
func graphPasswordVariable() string {
	if selectedGraphDriver() == "pg" {
		return "POSTGRES_PASSWORD"
	}
	return "NEO4J_PASSWORD"
}

func applicationGraphConnection(application containerInfo) (*url.URL, error) {
	if containerEnvironment(application, "bhe_graph_driver") != "pg" {
		return url.Parse(containerEnvironment(application, "bhe_neo4j_connection"))
	}
	var (
		setting            = containerEnvironment(application, "bhe_database_connection")
		configuration, err = pgx.ParseConfig(setting)
	)
	if setting == "" || err != nil || configuration == nil {
		return nil, fmt.Errorf("BloodHound PostgreSQL connection is missing or invalid")
	}
	if configuration.RuntimeParams["search_path"] != "" || configuration.RuntimeParams["options"] != "" {
		return nil, fmt.Errorf("BloodHound PostgreSQL connection must use the default graph schema without session overrides")
	}
	for _, fallback := range configuration.Fallbacks {
		if fallback.Host != configuration.Host || fallback.Port != configuration.Port {
			return nil, fmt.Errorf("BloodHound PostgreSQL connection must not route to fallback hosts")
		}
	}
	if strings.Contains(setting, "service=") || strings.Contains(setting, "servicefile=") {
		return nil, fmt.Errorf("BloodHound PostgreSQL connection must provide its host directly")
	}
	if configuration.Host == "" || strings.ContainsAny(configuration.Host, ",/ ") || configuration.Database == "" || strings.Contains(configuration.Database, "/") {
		return nil, fmt.Errorf("BloodHound PostgreSQL connection must identify one host and database")
	}
	return &url.URL{Scheme: "postgresql", Host: net.JoinHostPort(configuration.Host, fmt.Sprint(configuration.Port)), Path: "/" + configuration.Database, User: url.UserPassword(configuration.User, configuration.Password)}, nil
}
