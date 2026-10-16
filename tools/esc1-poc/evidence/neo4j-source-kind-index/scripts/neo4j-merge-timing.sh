#!/bin/bash
# Time how long Neo4j takes to add N new objects with one label, the same way BloodHound's import
# does it: for each object, "MERGE" looks for an existing object with that label and ID, and
# creates one if none exists.
#
# Usage: neo4j-merge-timing.sh LABEL N [index]
#   LABEL  a label not used anywhere else, so earlier runs cannot affect the result
#   N      how many objects to add
#   index  optional; create an ID index for LABEL first (the timer starts after it is ready)
#
# Runs against the lab's Neo4j container. Every measurement includes about 1.2 seconds of
# cypher-shell start-up time (a "RETURN 1" query took 1.24 seconds).
# Clean up afterwards with:  MATCH (n:LABEL) DETACH DELETE n   and   DROP INDEX LABEL_oid

label=$1
object_count=$2
create_index=$3

run_query() {
	docker exec esc1-workspace-manual-graph-db-1 sh -c "cypher-shell --format plain -u neo4j -p \"\${NEO4J_AUTH#neo4j/}\" \"$1\""
}

if [ "$create_index" = index ]; then
	run_query "CREATE INDEX ${label}_oid FOR (n:${label}) ON (n.objectid)" >/dev/null
	run_query "CALL db.awaitIndexes(60)" >/dev/null
fi

start_time=$(date +%s.%N)
run_query "UNWIND range(1,$object_count) AS i WITH {objectid: '${label}-' + toString(i), name: 'n' + toString(i)} AS p MERGE (n:${label} {objectid: p.objectid}) SET n += p" >/dev/null
end_time=$(date +%s.%N)

printf "%-14s N=%-6s index=%-3s %8.2f s\n" "$label" "$object_count" "${create_index:-no}" "$(echo "$end_time - $start_time" | bc)"
