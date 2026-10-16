// namespace: PoC_CyberArk
// version: v1.0.0
// depends-on: node:PoC_CyberArk_Account

// This rule can run before directory users arrive.
MATCH (account:PoC_CyberArk_Account)
RETURN 'PoC_CyberArk_AccountSummary_' + account.objectid AS id,
       ['PoC_CyberArk_AccountSummary'] AS kinds,
       {account_id: account.objectid, user_sid: account.user_sid} AS properties
