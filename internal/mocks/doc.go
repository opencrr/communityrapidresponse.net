// Package mocks provides test doubles for external service interfaces.
//
// [MockPostgridService] implements the PostgridServiceInterface with
// configurable return values and call tracking via [ValidateAddressCall]
// and [SendPostcardCall], allowing tests to assert on invocation
// arguments without contacting real external APIs.
package mocks
