#!/usr/bin/env python3
# Copyright 2026 Specter Ops, Inc.
#
# Licensed under the Apache License, Version 2.0
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
"""Make independent collector cases for a fresh, dedicated native ESC1 graph."""

import argparse
import copy
import json
from pathlib import Path
import uuid


PROJECT = Path(__file__).resolve().parents[1]
COLLECTION_TYPES = (
    "domains", "users", "computers", "groups", "certtemplates",
    "enterprisecas", "rootcas", "ntauthstores",
)

# Expectations are written from the native rules, never from query output.
CASE_DESCRIPTIONS = (
    ("baseline", "Ordinary template group and Alice CA enrollment."),
    ("approval_required", "Template requires manager approval."),
    ("missing_requiresmanagerapproval", "Fresh template omits requiresmanagerapproval."),
    ("missing_authorizedsignatures_v1", "Fresh version 1 template omits authorizedsignatures."),
    ("v1_signatures5", "Version 1 template explicitly has five authorized signatures."),
    ("v2_signatures1", "Version 2 template requires one authorized signature."),
    ("authentication_false", "Template authenticationenabled is false."),
    ("supplies_subject_false", "Template enrolleesuppliessubject is false."),
    ("disabled_host", "The CA host exists but is disabled."),
    ("no_host", "HostingComputer is null, so no host relationship is ingested."),
    ("missing_root_trust", "RootCA record is absent; the CA keeps its unique self-only chain."),
    ("missing_ntauth_thumbprint", "NTAuth store exists but its thumbprint list is empty."),
    ("nested_group_shortcut", "Alice belongs to Inner, Inner to Outer; Outer enrolls in the template and Inner in the CA."),
    ("special_ca_authenticated_users", "Authenticated Users enrolls in the CA; the ordinary group enrolls in the template."),
    ("special_template_everyone", "Everyone enrolls in the template; Alice enrolls in the CA."),
    ("both_special", "Everyone enrolls in the template and Authenticated Users in the CA."),
    ("independent_template_permission_sets", "Everyone enrolls in one template and Alice in another; the CA allows the ordinary group."),
    ("independent_ca_permission_sets", "One CA allows Alice and another allows the ordinary group; both publish the same ordinary-group template."),
)


def read_json(path):
    return json.loads(path.read_text())


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n")


def replace_strings(value, replacements):
    """Rename objects and references while keeping the baseline payload shape."""
    if isinstance(value, dict):
        return {key: replace_strings(item, replacements) for key, item in value.items()}
    if isinstance(value, list):
        return [replace_strings(item, replacements) for item in value]
    if isinstance(value, str):
        for old, new in replacements.items():
            value = value.replace(old, new)
    return value


def make_group(original_group, object_id, name, members):
    group = copy.deepcopy(original_group)
    group["ObjectIdentifier"] = object_id
    group["Properties"]["name"] = name + "@" + group["Properties"]["domain"]
    group["Properties"].pop("distinguishedname", None)
    group["Members"] = members
    return group


def set_ca_enroller(enterprise_ca, object_id, object_type):
    entry = enterprise_ca["CARegistryData"]["CASecurity"]["Data"][0]
    entry["PrincipalSID"] = object_id
    entry["PrincipalType"] = object_type


def make_case(baseline, index, name):
    old_domain = baseline["domains"]["data"][0]
    old_sid = old_domain["ObjectIdentifier"]
    old_name = old_domain["Properties"]["domain"]
    domain_sid = f"S-1-5-21-{710000000 + index}-{720000000 + index}-{730000000 + index}"
    domain_name = f"ESC1-NATIVE-{index:02d}.TEST"
    thumbprint = f"{index:040X}"
    replacements = {
        old_sid: domain_sid,
        old_name: domain_name,
        "DC=" + old_name.replace(".", ",DC="): "DC=" + domain_name.replace(".", ",DC="),
        baseline["enterprisecas"]["data"][0]["Properties"]["certthumbprint"]: thumbprint,
    }
    for collection_type in ("certtemplates", "enterprisecas", "rootcas", "ntauthstores"):
        old_id = baseline[collection_type]["data"][0]["ObjectIdentifier"]
        replacements[old_id] = str(uuid.uuid5(uuid.NAMESPACE_URL, f"esc1-native/{name}/{collection_type}"))
    collections = replace_strings(baseline, replacements)
    template = collections["certtemplates"]["data"][0]
    properties = template["Properties"]
    enterprise_ca = collections["enterprisecas"]["data"][0]
    original_group = collections["groups"]["data"][0]
    alice_id = domain_sid + "-1001"
    group_id = domain_sid + "-1101"
    inner_id = domain_sid + "-1102"
    everyone_id = domain_name + "-S-1-1-0"
    authenticated_users_id = domain_name + "-S-1-5-11"
    expected_sources = []

    if name == "baseline":
        expected_sources = [alice_id]
    elif name == "approval_required":
        properties["requiresmanagerapproval"] = True
    elif name == "missing_requiresmanagerapproval":
        del properties["requiresmanagerapproval"]
    elif name == "missing_authorizedsignatures_v1":
        properties["schemaversion"] = 1
        del properties["authorizedsignatures"]
    elif name == "v1_signatures5":
        properties["schemaversion"] = 1
        properties["authorizedsignatures"] = 5
        expected_sources = [alice_id]
    elif name == "v2_signatures1":
        properties["authorizedsignatures"] = 1
    elif name == "authentication_false":
        properties["authenticationenabled"] = False
    elif name == "supplies_subject_false":
        properties["enrolleesuppliessubject"] = False
    elif name == "disabled_host":
        collections["computers"]["data"][0]["Properties"]["enabled"] = False
    elif name == "no_host":
        # incoming_models.EnterpriseCA.HostingComputer is a Go string. JSON null
        # leaves it empty; ad.go only emits HostsCAService for a nonempty string.
        enterprise_ca["HostingComputer"] = None
    elif name == "missing_root_trust":
        collections["rootcas"]["data"] = []
    elif name == "missing_ntauth_thumbprint":
        collections["ntauthstores"]["data"][0]["Properties"]["certthumbprints"] = []
    elif name == "nested_group_shortcut":
        original_group["Properties"]["name"] = "OUTER@" + domain_name
        original_group["Members"] = [{"ObjectIdentifier": inner_id, "ObjectType": "Group"}]
        inner_group = make_group(original_group, inner_id, "INNER", [
            {"ObjectIdentifier": alice_id, "ObjectType": "User"},
        ])
        collections["groups"]["data"].append(inner_group)
        set_ca_enroller(enterprise_ca, inner_id, "Group")
        expected_sources = [inner_id]
    elif name == "special_ca_authenticated_users":
        collections["groups"]["data"].append(make_group(
            original_group, authenticated_users_id, "AUTHENTICATED USERS", [],
        ))
        set_ca_enroller(enterprise_ca, authenticated_users_id, "Group")
        expected_sources = [group_id]
    elif name == "special_template_everyone":
        collections["groups"]["data"].append(make_group(
            original_group, everyone_id, "EVERYONE", [],
        ))
        template["Aces"][0]["PrincipalSID"] = everyone_id
        expected_sources = [alice_id]
    elif name == "both_special":
        collections["groups"]["data"].extend([
            make_group(original_group, everyone_id, "EVERYONE", []),
            make_group(original_group, authenticated_users_id, "AUTHENTICATED USERS", []),
        ])
        template["Aces"][0]["PrincipalSID"] = everyone_id
        set_ca_enroller(enterprise_ca, authenticated_users_id, "Group")
        expected_sources = [everyone_id, authenticated_users_id]
    elif name == "independent_template_permission_sets":
        collections["groups"]["data"].append(make_group(
            original_group, everyone_id, "EVERYONE", [],
        ))
        template["Aces"][0]["PrincipalSID"] = everyone_id
        set_ca_enroller(enterprise_ca, group_id, "Group")
        second_template = copy.deepcopy(template)
        second_template["ObjectIdentifier"] = str(uuid.uuid5(
            uuid.NAMESPACE_URL, f"esc1-native/{name}/certtemplates/second",
        ))
        second_template["Properties"]["name"] = "INDEPENDENT-TEMPLATE-TWO@" + domain_name
        second_template["Properties"]["displayname"] = "INDEPENDENT-TEMPLATE-TWO"
        second_template["Properties"]["distinguishedname"] = (
            "CN=INDEPENDENT-TEMPLATE-TWO," + properties["distinguishedname"].split(",", 1)[1]
        )
        second_template["Aces"][0]["PrincipalSID"] = alice_id
        second_template["Aces"][0]["PrincipalType"] = "User"
        collections["certtemplates"]["data"].append(second_template)
        enterprise_ca["EnabledCertTemplates"].append({
            "ObjectIdentifier": second_template["ObjectIdentifier"], "ObjectType": "CertTemplate",
        })
        expected_sources = [group_id, alice_id]
    elif name == "independent_ca_permission_sets":
        second_thumbprint = f"{(1 << 159) + index:040X}"
        second_ca = copy.deepcopy(enterprise_ca)
        second_ca["ObjectIdentifier"] = str(uuid.uuid5(
            uuid.NAMESPACE_URL, f"esc1-native/{name}/enterprisecas/second",
        ))
        second_ca["Properties"]["name"] = "INDEPENDENT-CA-TWO@" + domain_name
        second_ca["Properties"]["caname"] = "INDEPENDENT-CA-TWO"
        second_ca["Properties"]["distinguishedname"] = (
            "CN=INDEPENDENT-CA-TWO," + enterprise_ca["Properties"]["distinguishedname"].split(",", 1)[1]
        )
        second_ca["Properties"]["certthumbprint"] = second_thumbprint
        second_ca["Properties"]["certchain"] = [second_thumbprint]
        set_ca_enroller(second_ca, group_id, "Group")
        collections["enterprisecas"]["data"].append(second_ca)
        second_root = copy.deepcopy(collections["rootcas"]["data"][0])
        second_root["ObjectIdentifier"] = str(uuid.uuid5(
            uuid.NAMESPACE_URL, f"esc1-native/{name}/rootcas/second",
        ))
        second_root["Properties"]["name"] = "INDEPENDENT ROOT TWO@" + domain_name
        second_root["Properties"]["distinguishedname"] = (
            "CN=INDEPENDENT ROOT TWO," + second_root["Properties"]["distinguishedname"].split(",", 1)[1]
        )
        second_root["Properties"]["certthumbprint"] = second_thumbprint
        second_root["Properties"]["certchain"] = [second_thumbprint]
        collections["rootcas"]["data"].append(second_root)
        collections["ntauthstores"]["data"][0]["Properties"]["certthumbprints"].append(second_thumbprint)
        expected_sources = [group_id, alice_id]
    else:
        raise ValueError(f"Unknown case: {name}")

    expected = [{"source": source, "target": domain_sid} for source in expected_sources]
    metadata = {
        "name": name,
        "domain": domain_name,
        "domain_sid": domain_sid,
        "alice": alice_id,
        "template_group": group_id,
        "template": template["ObjectIdentifier"],
        "enterprise_ca": enterprise_ca["ObjectIdentifier"],
        "certthumbprint": thumbprint,
        "expected": expected,
    }
    return collections, metadata


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=PROJECT / "work" / "native-cases")
    args = parser.parse_args()
    baseline = {
        collection_type: read_json(PROJECT / "testdata" / "baseline" / f"{collection_type}.json")
        for collection_type in COLLECTION_TYPES
    }
    original_expected = read_json(PROJECT / "testdata" / "expected.json")["baseline"]
    original_alice = baseline["users"]["data"][0]["ObjectIdentifier"]
    original_domain = baseline["domains"]["data"][0]["ObjectIdentifier"]
    if original_expected != [{"source": original_alice, "target": original_domain}]:
        raise ValueError("The baseline expectation changed; review the native case expectations.")

    combined = {
        collection_type: {"meta": copy.deepcopy(baseline[collection_type]["meta"]), "data": []}
        for collection_type in COLLECTION_TYPES
    }
    cases = []
    expected = []
    for index, (name, description) in enumerate(CASE_DESCRIPTIONS, start=1):
        collections, metadata = make_case(baseline, index, name)
        metadata["description"] = description
        cases.append(metadata)
        expected.extend(metadata["expected"])
        for collection_type in COLLECTION_TYPES:
            combined[collection_type]["data"].extend(collections[collection_type]["data"])

    args.output.mkdir(parents=True, exist_ok=True)
    for collection_type, collection in combined.items():
        collection["meta"]["count"] = len(collection["data"])
        write_json(args.output / f"{collection_type}.json", collection)
    write_json(args.output / "expected.json", expected)
    write_json(args.output / "cases.json", cases)
    readme = [
        "# Independent native ESC1 cases", "",
        "Use these files only in a fresh dedicated graph. Upload the eight collection JSON files together.",
        "Do not upload expected.json or cases.json; those files describe the expected results.", "",
        "Each case has its own domain, object identifiers, and certificate thumbprint. The CA and root",
        "inside one case share a fingerprint and a one-element self-signed chain. No case trusts another case.",
        "All objects and fingerprints are invented. The script preserves the baseline version 6 collector shapes.", "",
        "expected.json is the combined source/target pair list. cases.json explains each case and its expected list.",
        "These expectations come from the native source rules, not from a query result or a completed native run.", "",
        "Missing template properties belong to new objects, so old graph properties cannot fill the gaps.",
        "HostingComputer is null in the no-host case; the native Go string remains empty and no host link is emitted.",
        "The missing-root case omits its RootCA record entirely. Its self-only CA chain has no trusted root.",
        "Special groups use the collector convention DOMAIN-S-1-1-0 and DOMAIN-S-1-5-11.",
        "Native shortcut matching treats these suffixes globally; their domain prefixes still keep object IDs independent.", "",
        "The nested-group result is Inner, because native analysis retains the common enrollment group as a shortcut.",
        "A special group on one permission side removes that side's restriction and retains the other original set.",
        "When both sides contain special groups, native analysis returns both original special groups.", "",
        "The final two cases keep permission sets independent for each template/CA pairing.",
        "One eligible template grants Everyone and another grants Alice, while their CA grants the ordinary group.",
        "One eligible CA grants Alice and another grants the ordinary group, while their template grants that group.",
        "Both cases retain Group and Alice. They do not mix permissions or special-group exemptions across pairs.",
        "The second CA has a separate invented self-signed certificate, matching RootCA, and NTAuth thumbprint.", "",
        "Regenerating these files does not clear an existing graph. Omission is not a deletion instruction.",
        "Do not use this dataset to patch a graph that already contains these cases or the baseline fixture.", "",
        "| Case | Expected sources |", "| --- | --- |",
    ]
    for case in cases:
        sources = ", ".join(pair["source"] for pair in case["expected"]) or "None"
        readme.append(f"| {case['name']} | {sources} |")
    (args.output / "README.md").write_text("\n".join(readme) + "\n")
    print(f"Wrote {len(cases)} cases and {len(expected)} expected pairs to {args.output}")


if __name__ == "__main__":
    main()
