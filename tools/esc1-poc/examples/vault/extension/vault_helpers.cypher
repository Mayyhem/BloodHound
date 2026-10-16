// namespace: PoC_Vault
// version: v1.0.0
// depends-on: node:PoC_Vault_Container, node:PoC_Vault_Account
// depends-on: relationship:PoC_Vault_ContainsAccount

// Keep one helper for each vault, even when it contains no collected accounts.
// A changing count makes helper rollback observable after a later failure.
MATCH (vault:PoC_Vault_Container)
OPTIONAL MATCH (vault)-[:PoC_Vault_ContainsAccount]->(account:PoC_Vault_Account)
WITH vault, count(DISTINCT account) AS account_count
RETURN 'PoC_Vault_Helper_' + vault.objectid AS id,
       ['PoC_Vault_Helper'] AS kinds,
       {name: vault.name + ' processing helper',
        vault_id: vault.objectid,
        account_count: account_count} AS properties
