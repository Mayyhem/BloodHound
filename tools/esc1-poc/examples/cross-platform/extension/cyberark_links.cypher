// namespace: PoC_CyberArk
// version: v1.0.0
// existing-edge-policy: keep-existing

// depends-on: query:PoC_AD/ad_users
// depends-on: node:PoC_CyberArk_Account, node:User

// Consume this run's directory summary rows across the schema boundary.
// The target remains the native User, rather than the summary helper.
UNWIND $ad_users AS directory_user
MATCH (account:PoC_CyberArk_Account)
WHERE account.user_sid = directory_user.properties.user_sid
MATCH (native_user:User)
WHERE native_user.objectid = directory_user.properties.user_sid
RETURN DISTINCT account.objectid AS source, native_user.objectid AS target, 'PoC_CyberArk_AccountFor' AS kind
