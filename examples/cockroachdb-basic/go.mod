module github.com/getpup/pupsourcing/examples/cockroachdb-basic

go 1.24.11

replace github.com/getpup/pupsourcing => ../..

require (
	github.com/getpup/pupsourcing v0.0.0-00010101000000-000000000000
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.10.9
)
