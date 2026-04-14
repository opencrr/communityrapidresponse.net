// Package main is a CLI tool that seeds school and school district data
// from the National Center for Education Statistics (NCES) API.
//
// It fetches districts and schools in bulk, maps them to the local
// schema, and upserts them into MariaDB. Intended to be run once during
// initial setup or periodically to refresh NCES data.
package main
