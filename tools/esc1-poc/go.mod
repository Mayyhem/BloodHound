module github.com/specterops/bloodhound/tools/esc1-poc

go 1.27.1

require (
	github.com/RoaringBitmap/roaring/v2 v2.19.0
	github.com/antlr4-go/antlr/v4 v4.13.1
	github.com/jackc/pgx/v5 v5.10.0
	github.com/neo4j/neo4j-go-driver/v5 v5.28.4
	github.com/specterops/bloodhound v0.0.0
	github.com/specterops/dawgs v0.9.1
)

require (
	github.com/axiomhq/hyperloglog v0.2.6 // indirect
	github.com/bits-and-blooms/bitset v1.24.6 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-metro v0.0.0-20250106013310-edb8663e5e33 // indirect
	github.com/gammazero/deque v1.2.1 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgtype v1.14.4 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kamstrup/intmap v0.5.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	golang.org/x/exp v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

replace github.com/specterops/dawgs => ../DAWGS

// Read native kinds from this BloodHound checkout instead of keeping a second list.
replace github.com/specterops/bloodhound => ../..
