// Package models defines the core domain types shared across the
// application: Transaction and TransactionSet (the parsed CSV bank data),
// dashboard KPIs, healthcare and major-expense definitions, what-if
// retirement settings and the WhatIfAnalysis result, persons/identities,
// and the insights pattern types. These types are intentionally
// dependency-light — they have no behavior beyond plain accessors so they
// can be safely shared between services, handlers, and templates.
package models
