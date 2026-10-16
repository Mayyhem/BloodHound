// namespace: PoC
// version: v1.0.0
// depends-on: node:CertTemplate

// packages/go/analysis/ad/esc1.go: isCertTemplateValidForEsc1.
// All five properties must be present with the expected type. In particular,
// version 1 does not excuse a missing authorizedsignatures property.
MATCH (template:CertTemplate)
WHERE template.requiresmanagerapproval = false
  AND template.authenticationenabled = true
  AND template.enrolleesuppliessubject = true
  AND template.schemaversion IS NOT NULL
  AND template.authorizedsignatures IS NOT NULL
  AND template.schemaversion = toFloat(template.schemaversion)
  AND template.authorizedsignatures = toFloat(template.authorizedsignatures)
  AND NOT (template.schemaversion > 1 AND template.authorizedsignatures > 0)
RETURN DISTINCT template.objectid AS template
