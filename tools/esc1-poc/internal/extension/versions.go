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

package extension

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var versionCondition = regexp.MustCompile(`^(==|!=|>=|<=|~=|>|<)\s*(v[^\s]+)$`)

// Concrete analysis versions use the documented v-prefixed SemVer spelling.
// In particular, v1 and v1.2 are not versions of an installed analysis.
func parseVersion(value string) (*semver.Version, error) {
	if !strings.HasPrefix(value, "v") {
		return nil, fmt.Errorf("use a v-prefixed semantic version, such as v1.0.0")
	}
	version, err := semver.StrictNewVersion(strings.TrimPrefix(value, "v"))
	if err != nil {
		return nil, fmt.Errorf("use a complete semantic version, such as v1.0.0")
	}
	return version, nil
}

type versionComparison struct {
	operator string
	version  *semver.Version
	upper    *semver.Version
}

// Parse the small requirements.txt-style language ourselves. This deliberately
// excludes semver library shortcuts such as ^, ~, OR, and unprefixed versions.
func parseVersionConditions(text string) ([]versionComparison, error) {
	var comparisons []versionComparison
	if strings.TrimSpace(text) == "" {
		return comparisons, nil
	}
	for _, condition := range strings.Split(text, ",") {
		parts := versionCondition.FindStringSubmatch(strings.TrimSpace(condition))
		if parts == nil {
			return nil, fmt.Errorf("use comma-separated version conditions, such as >=v1.0.0,<v2.0.0")
		}
		operator, value := parts[1], parts[2]
		wildcard := strings.HasSuffix(value, ".*")
		if wildcard || operator == "~=" {
			if wildcard && operator != "==" && operator != "!=" {
				return nil, fmt.Errorf("a trailing .* is allowed only with == or !=")
			}
			segments := strings.Split(strings.TrimSuffix(strings.TrimPrefix(value, "v"), ".*"), ".")
			if wildcard && len(segments) != 1 && len(segments) != 2 || !wildcard && len(segments) != 2 && len(segments) != 3 {
				return nil, fmt.Errorf("use ==v1.*, ==v1.2.*, ~=v1.2, or ~=v1.2.3")
			}
			// Compatible release conditions allow a prerelease or build suffix
			// on the final component; ordinary strict version parsing checks it.
			base := strings.Join(segments, ".")
			for count := len(segments); count < 3; count++ {
				base += ".0"
			}
			version, err := parseVersion("v" + base)
			if err != nil || wildcard && (version.Prerelease() != "" || version.Metadata() != "") {
				return nil, fmt.Errorf("invalid wildcard or compatible-release version")
			}
			major, minor := version.Major(), version.Minor()
			upper := ""
			if len(segments) == 1 || operator == "~=" && len(segments) == 2 {
				if major == ^uint64(0) {
					return nil, fmt.Errorf("version bound is too large")
				}
				upper = strconv.FormatUint(major+1, 10) + ".0.0"
			} else {
				if minor == ^uint64(0) {
					return nil, fmt.Errorf("version bound is too large")
				}
				upper = strconv.FormatUint(major, 10) + "." + strconv.FormatUint(minor+1, 10) + ".0"
			}
			upperVersion, err := semver.StrictNewVersion(upper)
			if err != nil {
				return nil, fmt.Errorf("invalid version bound")
			}
			comparisons = append(comparisons, versionComparison{operator: operator, version: version, upper: upperVersion})
			continue
		}
		version, err := parseVersion(value)
		if err != nil {
			return nil, err
		}
		comparisons = append(comparisons, versionComparison{operator: operator, version: version})
	}
	return comparisons, nil
}

// VersionMatches checks the one activated version. It never searches older
// files. An omitted condition accepts that version, including reviewed releases
// marked as prereleases. With conditions, prereleases must be explicitly named
// in at least one comparison. Build metadata does not change SemVer ordering.
func VersionMatches(versionText, conditions string) (bool, error) {
	var allowsPrerelease = strings.TrimSpace(conditions) == ""
	version, err := parseVersion(versionText)
	if err != nil {
		return false, err
	}
	comparisons, err := parseVersionConditions(conditions)
	if err != nil {
		return false, err
	}
	for _, comparison := range comparisons {
		allowsPrerelease = allowsPrerelease || comparison.version.Prerelease() != ""
		order := version.Compare(comparison.version)
		matches := false
		if comparison.upper != nil {
			matches = order >= 0 && version.LessThan(comparison.upper)
			if comparison.operator == "!=" {
				matches = !matches
			}
		} else {
			switch comparison.operator {
			case "==":
				matches = order == 0
			case "!=":
				matches = order != 0
			case ">":
				matches = order > 0
			case ">=":
				matches = order >= 0
			case "<":
				matches = order < 0
			case "<=":
				matches = order <= 0
			}
		}
		if !matches {
			return false, nil
		}
	}
	return version.Prerelease() == "" || allowsPrerelease, nil
}

// Existing BloodHound schema uploads also accept 1.0.0 without the v prefix.
// Normalize only for comparison; reports keep the actual installed spelling.
// Analysis files and dependency comments still use their documented v spelling.
func SchemaVersionMatches(versionText, conditions string) (bool, error) {
	version, err := semver.NewVersion(versionText)
	if err != nil {
		return false, fmt.Errorf("invalid installed schema version")
	}
	return VersionMatches("v"+version.String(), conditions)
}
