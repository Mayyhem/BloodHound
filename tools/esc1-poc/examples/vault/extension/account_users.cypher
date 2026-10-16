// namespace: PoC_Vault
// version: v1.0.0
// existing-edge-policy: keep-existing

// depends-on: query:vault_helpers
// depends-on: node:PoC_Vault_Container, node:PoC_Vault_Account, node:User
// depends-on: relationship:PoC_Vault_ContainsAccount

// Use this run's helper rows to choose vaults, then read the collected evidence.
// The account and directory user stay separate; the SID is the joining value.
UNWIND $vault_helpers AS helper
MATCH (vault:PoC_Vault_Container)-[:PoC_Vault_ContainsAccount]->(account:PoC_Vault_Account)
WHERE vault.objectid = helper.properties.vault_id
MATCH (directory_user:User)
WHERE directory_user.objectid = account.user_sid

// Several contained accounts can support the same single published connection.
RETURN DISTINCT vault.objectid AS source, directory_user.objectid AS target, 'PoC_Vault_HasAccountFor' AS kind
