// namespace: PoC
// version: v1.0.0
// depends-on: node:Group

// Native selects these exact SID suffixes, including the leading dash.
MATCH (principal:Group)
WHERE principal.objectid ENDS WITH '-S-1-1-0'
   OR principal.objectid ENDS WITH '-S-1-5-11'
RETURN principal.objectid AS principal
