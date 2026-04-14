// Package database provides the data-access layer for MariaDB.
//
// [DB] wraps *sql.DB with transaction helpers and Sentry tracing. Each
// domain area has a dedicated repository type (e.g. [UserRepository],
// [RegionRepository], [SignalGroupRepository]) that accepts a [Querier]
// interface, allowing the same queries to run inside or outside a
// transaction.
//
// Spatial queries use MariaDB's ST_Contains for geographic containment
// checks on the GEOMETRY column of geographic_regions. [DatabaseQueue]
// implements an email notification queue backed by the same database.
package database
