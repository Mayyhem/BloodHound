// namespace: PoC_AD
// version: v1.0.0
// depends-on: node:User

// Keep a summary row for each existing native directory user.
MATCH (directory_user:User)
RETURN 'PoC_AD_User_' + directory_user.objectid AS id,
       ['PoC_AD_User'] AS kinds,
       {user_sid: directory_user.objectid} AS properties
