# Synthetic native collection fixtures

These files describe one small, invented Active Directory certificate environment. They are classic version 6 collector payloads intended for BloodHound's real file-upload API. They provide a concrete expected ESC1 result and explicit property changes for checking whether the experiment preserves a relationship while another valid template still supports it. The supplied demonstration has been run through the local BloodHound upload API and matches native ESC1 exactly for these fixtures. The [review guide](../REVIEW.md) records the results and broader checks; the [usage guide](../USAGE.md) explains the current setup.

All names, object identifiers, and certificate fingerprints are synthetic. The certificate fingerprint is an identity used by graph analysis; these payloads do not contain an actual certificate, private key, or usable directory credential. The JSON shapes were checked against the repository's `cmd/api/src/test/fixtures/fixtures/v6/all/adcs.zip`, `packages/go/ein/incoming_models.go`, and ingestion converters in `packages/go/ein/ad.go`. Payloads were authored from scratch rather than copied from that example environment.

## Baseline

Upload all eight `baseline/*.json` files in one controlled upload job. `expected.json` is an expectation manifest, not a collection file, and must not be uploaded. Each collector file declares its type, version, record count, and collection method flags in `meta`; its `data` array contains the actual objects. The method flags cover group membership, ACLs, object properties, CA registry data, and certificate services. These flags describe the fixture's intended evidence; they do not prove that a real directory collection is complete.

The scenario has these objects:

| Object | Identifier | Purpose |
| --- | --- | --- |
| Domain | `S-1-5-21-111111111-222222222-333333333` | The authentication target |
| Alice, a user | domain SID plus `-1001` | The expected ESC1 source |
| CA host, a computer | domain SID plus `-1002` | An enabled host in the CA's own domain |
| Template Enrollers, a group | domain SID plus `-1101` | Contains Alice and can enroll in the template |
| Vulnerable One, a template | `11111111-1111-4111-8111-111111111111` | Grants the group template enrollment |
| Enterprise CA | `33333333-3333-4333-8333-333333333333` | Publishes the template and grants Alice CA enrollment |
| Root CA | `44444444-4444-4444-8444-444444444444` | The domain's trusted root certificate |
| NTAuth store | `55555555-5555-4555-8555-555555555555` | The domain's trusted authentication certificate store |

The template explicitly sets all five native ESC1 eligibility fields: `requiresmanagerapproval=false`, `authenticationenabled=true`, `enrolleesuppliessubject=true`, `schemaversion=2`, and `authorizedsignatures=0`. Authentication EKUs are included as explanatory collector evidence; the pinned ESC1 predicate reads `authenticationenabled` directly.

`EnabledCertTemplates` on the CA produces `PublishedTo`. `HostingComputer` produces `HostsCAService`. The host's `enabled=true` and matching `domainsid` satisfy the current requirement for an enabled host in the CA's forest. `CARegistryData.CASecurity.Collected=true` tells ingestion to use the supplied registry security entries; its Alice `Enroll` entry becomes Alice's CA enrollment right. The template's group `Enroll` entry comes from `Aces`.

The CA and root share one invented fingerprint. Native analysis produces `EnterpriseCAFor` between those two representations of the same self-signed certificate. A one-element `certchain` intentionally has no parent, so this baseline does not produce `IssuedSignedBy`. The NTAuth store's `certthumbprints` includes the CA fingerprint, which lets native analysis produce `TrustedForNTAuth`. Explicit top-level `DomainSID` fields on the root and NTAuth records produce `RootCAFor` and `NTAuthStoreFor` respectively. These fields are separate from `Properties.domainsid`; both are supplied on purpose.

`packages/go/analysis/ad/esc1.go` requires both template enrollment and CA enrollment, and both root trust and NTAuth trust. The template enrollment set starts with the group. The CA enrollment set starts with Alice. `CalculateCrossProductNodeSets` expands the group's membership and intersects it with Alice's CA permission. The expected result is exactly Alice to the domain. The group itself has no CA enrollment, and the CA host has neither enrollment permission, so neither is an expected source. No Everyone or Authenticated Users shortcut is involved.

## Explicit change variants

Variant files replace the indicated baseline collection file, or can be uploaded as updates to the corresponding fixture objects. They are not complete standalone environments. All expected sets describe a graph containing only this fixture's objects and the indicated current properties. The same source/target pair appears once even when two templates support it.

| Variant | Setup and changed evidence | Expected ESC1 pairs |
| --- | --- | --- |
| `baseline` | Upload all baseline files | Alice to domain |
| `manager-approval` | Baseline plus this variant's `certtemplates.json`, which sets the first template's approval requirement to true | None |
| `two-templates` | Baseline plus both variant files; the CA explicitly publishes the additional template `22222222-2222-4222-8222-222222222222` | Alice to domain |
| `one-support-remaining` | Start with two templates, then upload this variant's template file; the first requires approval and the second remains eligible | Alice to domain |
| `all-supports-invalid` | Start with two templates, then upload this variant's template file; both templates require approval | None |

The invalid templates remain valid collector JSON and valid directory objects. The changed property makes them ineligible for ESC1; it does not simulate a malformed upload. No variant removes a relationship by omitting an object from a later file. Omission from a collector payload is not a reliable deletion instruction. For example, uploading the original single-template CA after the two-template case does not establish that the second `PublishedTo` relationship was removed. Use the supplied explicit template property changes for these checks, and isolate separate baseline cases in a fresh disposable environment.

## Ingestion order exercise

`baseline/users.json` and `baseline/groups.json` are deliberately separate. The user payload declares Alice but does not declare her custom group membership. The group payload's `Members` array supplies that membership. With the other six baseline files present, a native analysis completed before the group membership arrives should have no fixture ESC1 result; analysis after both files have arrived should have Alice to domain. Reverse their arrival order in a separately isolated fixture graph and compare the final results after both successful uploads and native analysis complete. Reference-only placeholder nodes may exist between uploads, so compare complete final states rather than assuming intermediate placeholder behavior.

`expected.json` contains the exact source/target pairs derived above. The local upload demonstration confirms these written expectations against native BloodHound. This fixture exercises one ordinary group expansion and one self-signed trust chain. It does not establish parity for nested groups, special groups, cross-forest chains, subordinate certificate authorities, disabled or missing hosts, or the rest of the native ESC1 input space.
